package aws

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/amplify"
	amplifytypes "github.com/aws/aws-sdk-go-v2/service/amplify/types"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apigwv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/aws/aws-sdk-go-v2/service/appstream"
	appstreamtypes "github.com/aws/aws-sdk-go-v2/service/appstream/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticbeanstalk"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/elasticbeanstalk/types"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	ebridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/lightsail"
	lightsailtypes "github.com/aws/aws-sdk-go-v2/service/lightsail/types"
	"github.com/aws/aws-sdk-go-v2/service/waf"
	waftypes "github.com/aws/aws-sdk-go-v2/service/waf/types"
	"github.com/aws/aws-sdk-go-v2/service/wafregional"
	wafregionaltypes "github.com/aws/aws-sdk-go-v2/service/wafregional/types"
	"github.com/aws/aws-sdk-go-v2/service/wafv2"
	wafv2types "github.com/aws/aws-sdk-go-v2/service/wafv2/types"

	"github.com/unicrons/tagctl/internal/types"
)

type scriptedPage struct {
	items int
	next  string
}

// pageScript serves pages in order and rejects a request that does not carry
// the token of the page before it.
type pageScript struct {
	pages []scriptedPage
	calls int
}

func (s *pageScript) serve(token *string) ([]string, *string, error) {
	if s.calls == len(s.pages) {
		return nil, nil, fmt.Errorf("request %d after the last page", s.calls+1)
	}
	want := ""
	if s.calls > 0 {
		want = s.pages[s.calls-1].next
	}
	if got := aws.ToString(token); got != want {
		return nil, nil, fmt.Errorf("request %d sent token %q, want %q", s.calls+1, got, want)
	}
	page := s.pages[s.calls]
	s.calls++
	names := make([]string, page.items)
	for i := range names {
		names[i] = fmt.Sprintf("page%d-item%d", s.calls, i)
	}
	var next *string
	if page.next != "" {
		next = aws.String(page.next)
	}
	return names, next, nil
}

type pagedHTTPAPIs struct{ *pageScript }

func (f pagedHTTPAPIs) GetApis(_ context.Context, in *apigatewayv2.GetApisInput, _ ...func(*apigatewayv2.Options)) (*apigatewayv2.GetApisOutput, error) {
	names, next, err := f.serve(in.NextToken)
	out := &apigatewayv2.GetApisOutput{NextToken: next}
	for _, n := range names {
		out.Items = append(out.Items, apigwv2types.Api{ApiId: aws.String(n)})
	}
	return out, err
}

type pagedEventRules struct{ *pageScript }

func (f pagedEventRules) ListRules(_ context.Context, in *eventbridge.ListRulesInput, _ ...func(*eventbridge.Options)) (*eventbridge.ListRulesOutput, error) {
	names, next, err := f.serve(in.NextToken)
	out := &eventbridge.ListRulesOutput{NextToken: next}
	for _, n := range names {
		out.Rules = append(out.Rules, ebridgetypes.Rule{Name: aws.String(n), Arn: aws.String("arn:events:" + n)})
	}
	return out, err
}

type pagedBeanstalk struct{ *pageScript }

func (f pagedBeanstalk) DescribeEnvironments(_ context.Context, in *elasticbeanstalk.DescribeEnvironmentsInput, _ ...func(*elasticbeanstalk.Options)) (*elasticbeanstalk.DescribeEnvironmentsOutput, error) {
	names, next, err := f.serve(in.NextToken)
	out := &elasticbeanstalk.DescribeEnvironmentsOutput{NextToken: next}
	for _, n := range names {
		out.Environments = append(out.Environments, ebtypes.EnvironmentDescription{EnvironmentId: aws.String(n), EnvironmentArn: aws.String("arn:eb:" + n)})
	}
	return out, err
}

type pagedAppStream struct{ fleets, stacks *pageScript }

func (f pagedAppStream) DescribeFleets(_ context.Context, in *appstream.DescribeFleetsInput, _ ...func(*appstream.Options)) (*appstream.DescribeFleetsOutput, error) {
	names, next, err := f.fleets.serve(in.NextToken)
	out := &appstream.DescribeFleetsOutput{NextToken: next}
	for _, n := range names {
		out.Fleets = append(out.Fleets, appstreamtypes.Fleet{Name: aws.String(n), Arn: aws.String("arn:as:fleet/" + n)})
	}
	return out, err
}

func (f pagedAppStream) DescribeStacks(_ context.Context, in *appstream.DescribeStacksInput, _ ...func(*appstream.Options)) (*appstream.DescribeStacksOutput, error) {
	names, next, err := f.stacks.serve(in.NextToken)
	out := &appstream.DescribeStacksOutput{NextToken: next}
	for _, n := range names {
		out.Stacks = append(out.Stacks, appstreamtypes.Stack{Name: aws.String(n), Arn: aws.String("arn:as:stack/" + n)})
	}
	return out, err
}

type pagedLightsail struct {
	lightsailAPI
	*pageScript
}

func (f pagedLightsail) GetInstances(_ context.Context, in *lightsail.GetInstancesInput, _ ...func(*lightsail.Options)) (*lightsail.GetInstancesOutput, error) {
	names, next, err := f.serve(in.PageToken)
	out := &lightsail.GetInstancesOutput{NextPageToken: next}
	for _, n := range names {
		out.Instances = append(out.Instances, lightsailtypes.Instance{Name: aws.String(n)})
	}
	return out, err
}

type pagedAmplify struct{ *pageScript }

func (f pagedAmplify) ListApps(_ context.Context, in *amplify.ListAppsInput, _ ...func(*amplify.Options)) (*amplify.ListAppsOutput, error) {
	names, next, err := f.serve(in.NextToken)
	out := &amplify.ListAppsOutput{NextToken: next}
	for _, n := range names {
		out.Apps = append(out.Apps, amplifytypes.App{AppId: aws.String(n)})
	}
	return out, err
}

type pagedWAFRegional struct{ *pageScript }

func (f pagedWAFRegional) ListWebACLs(_ context.Context, in *wafregional.ListWebACLsInput, _ ...func(*wafregional.Options)) (*wafregional.ListWebACLsOutput, error) {
	names, next, err := f.serve(in.NextMarker)
	out := &wafregional.ListWebACLsOutput{NextMarker: next}
	for _, n := range names {
		out.WebACLs = append(out.WebACLs, wafregionaltypes.WebACLSummary{WebACLId: aws.String(n)})
	}
	return out, err
}

type pagedWAFv2 struct{ *pageScript }

func (f pagedWAFv2) ListWebACLs(_ context.Context, in *wafv2.ListWebACLsInput, _ ...func(*wafv2.Options)) (*wafv2.ListWebACLsOutput, error) {
	names, next, err := f.serve(in.NextMarker)
	out := &wafv2.ListWebACLsOutput{NextMarker: next}
	for _, n := range names {
		out.WebACLs = append(out.WebACLs, wafv2types.WebACLSummary{Id: aws.String(n), ARN: aws.String("arn:wafv2:" + n)})
	}
	return out, err
}

type pagedWAFGlobal struct{ *pageScript }

func (f pagedWAFGlobal) ListWebACLs(_ context.Context, in *waf.ListWebACLsInput, _ ...func(*waf.Options)) (*waf.ListWebACLsOutput, error) {
	names, next, err := f.serve(in.NextMarker)
	out := &waf.ListWebACLsOutput{NextMarker: next}
	for _, n := range names {
		out.WebACLs = append(out.WebACLs, waftypes.WebACLSummary{WebACLId: aws.String(n)})
	}
	return out, err
}

func TestPaginatedListers(t *testing.T) {
	ctx := context.Background()
	p := bulkProvider(nil)
	lastPage := func() *pageScript { return &pageScript{pages: []scriptedPage{{}}} }
	cases := []struct {
		name string
		list func(*pageScript) ([]types.Resource, error)
	}{
		{"API Gateway v2", func(s *pageScript) ([]types.Resource, error) {
			return p.listHTTPAPIsFrom(ctx, pagedHTTPAPIs{s}, defaultRegion)
		}},
		{"EventBridge", func(s *pageScript) ([]types.Resource, error) {
			return p.listEventRulesFrom(ctx, pagedEventRules{s}, defaultRegion)
		}},
		{"Elastic Beanstalk", func(s *pageScript) ([]types.Resource, error) {
			return p.listBeanstalkEnvironmentsFrom(ctx, pagedBeanstalk{s}, defaultRegion)
		}},
		{"AppStream fleets", func(s *pageScript) ([]types.Resource, error) {
			return p.listAppStreamResourcesFrom(ctx, pagedAppStream{fleets: s, stacks: lastPage()}, defaultRegion)
		}},
		{"AppStream stacks", func(s *pageScript) ([]types.Resource, error) {
			return p.listAppStreamResourcesFrom(ctx, pagedAppStream{fleets: lastPage(), stacks: s}, defaultRegion)
		}},
		{"Lightsail", func(s *pageScript) ([]types.Resource, error) {
			return p.listLightsailInstancesFrom(ctx, pagedLightsail{pageScript: s}, defaultRegion)
		}},
		{"Amplify", func(s *pageScript) ([]types.Resource, error) {
			return p.listAmplifyAppsFrom(ctx, pagedAmplify{s}, defaultRegion)
		}},
		{"WAF Classic regional", func(s *pageScript) ([]types.Resource, error) {
			return p.listRegionalWebACLsFrom(ctx, pagedWAFRegional{s}, defaultRegion)
		}},
		{"WAFv2", func(s *pageScript) ([]types.Resource, error) {
			return p.listWAFv2WebACLsFrom(ctx, pagedWAFv2{s}, defaultRegion, wafv2types.ScopeRegional)
		}},
		{"WAF Classic global", func(s *pageScript) ([]types.Resource, error) {
			return p.listGlobalWebACLsFrom(ctx, pagedWAFGlobal{s})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("empty page with token", func(t *testing.T) {
				s := &pageScript{pages: []scriptedPage{{next: "t1"}, {items: 2}}}
				resources, err := tc.list(s)
				if err != nil || len(resources) != 2 || s.calls != 2 {
					t.Errorf("resources = %d, calls = %d, err = %v; want 2 resources from 2 calls", len(resources), s.calls, err)
				}
			})
			t.Run("repeated token", func(t *testing.T) {
				s := &pageScript{pages: []scriptedPage{{items: 1, next: "t1"}, {items: 1, next: "t1"}}}
				resources, err := tc.list(s)
				if !errors.Is(err, errRepeatedPageToken) || resources != nil || s.calls != 2 {
					t.Errorf("resources = %d, calls = %d, err = %v; want errRepeatedPageToken after 2 calls", len(resources), s.calls, err)
				}
			})
		})
	}
}

func TestPaginate(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name      string
		next      []*string
		fetchErr  error
		wantCalls int
		wantErr   error
	}{
		{"nil token ends", []*string{aws.String("a"), nil}, nil, 2, nil},
		{"empty token ends", []*string{aws.String("a"), aws.String("")}, nil, 2, nil},
		{"token cycle fails", []*string{aws.String("a"), aws.String("b"), aws.String("a")}, nil, 3, errRepeatedPageToken},
		{"fetch error stops", []*string{aws.String("a")}, boom, 1, boom},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			err := paginate(func(*string) (*string, error) {
				if calls == len(tc.next) {
					t.Fatalf("fetch called %d times, script has %d pages", calls+1, len(tc.next))
				}
				next := tc.next[calls]
				calls++
				return next, tc.fetchErr
			})
			if !errors.Is(err, tc.wantErr) || calls != tc.wantCalls {
				t.Errorf("err = %v, calls = %d; want %v after %d calls", err, calls, tc.wantErr, tc.wantCalls)
			}
		})
	}
}

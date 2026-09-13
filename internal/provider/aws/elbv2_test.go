package aws

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/smithy-go"
)

// mockELBv2Client serves DescribeLoadBalancers pages and records tag batches.
type mockELBv2Client struct {
	pages       [][]elbv2types.LoadBalancer
	targetPages [][]elbv2types.TargetGroup
	tgCalls     int
	tags        map[string][]elbv2types.Tag
	listErr     error
	tagsErr     error
	gone        map[string]bool
	omit        map[string]bool
	tagCalls    int
	calls       int
	batchSizes  []int
	describeAll []string
}

func (m *mockELBv2Client) DescribeLoadBalancers(ctx context.Context, params *elbv2.DescribeLoadBalancersInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeLoadBalancersOutput, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	page := m.pages[m.calls]
	m.calls++
	out := &elbv2.DescribeLoadBalancersOutput{LoadBalancers: page}
	if m.calls < len(m.pages) {
		out.NextMarker = aws.String("next")
	}
	return out, nil
}

func (m *mockELBv2Client) DescribeTargetGroups(ctx context.Context, params *elbv2.DescribeTargetGroupsInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeTargetGroupsOutput, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.tgCalls >= len(m.targetPages) {
		return &elbv2.DescribeTargetGroupsOutput{}, nil
	}
	page := m.targetPages[m.tgCalls]
	m.tgCalls++
	out := &elbv2.DescribeTargetGroupsOutput{TargetGroups: page}
	if m.tgCalls < len(m.targetPages) {
		out.NextMarker = aws.String("next")
	}
	return out, nil
}

func (m *mockELBv2Client) DescribeTags(ctx context.Context, params *elbv2.DescribeTagsInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeTagsOutput, error) {
	m.tagCalls++
	if m.tagsErr != nil {
		return nil, m.tagsErr
	}
	for _, arn := range params.ResourceArns {
		if m.gone[arn] {
			return nil, &elbv2types.LoadBalancerNotFoundException{Message: aws.String("not found")}
		}
	}
	m.batchSizes = append(m.batchSizes, len(params.ResourceArns))
	m.describeAll = append(m.describeAll, params.ResourceArns...)

	descs := make([]elbv2types.TagDescription, 0, len(params.ResourceArns))
	for _, arn := range params.ResourceArns {
		if m.omit[arn] {
			continue
		}
		descs = append(descs, elbv2types.TagDescription{
			ResourceArn: aws.String(arn),
			Tags:        m.tags[arn],
		})
	}
	return &elbv2.DescribeTagsOutput{TagDescriptions: descs}, nil
}

func TestListLoadBalancers(t *testing.T) {
	arn := "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/web/abc123"
	created := time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)

	mock := &mockELBv2Client{
		pages: [][]elbv2types.LoadBalancer{{
			{
				LoadBalancerArn:  aws.String(arn),
				LoadBalancerName: aws.String(webCluster),
				CreatedTime:      aws.Time(created),
			},
		}},
		tags: map[string][]elbv2types.Tag{
			arn: {{Key: aws.String("environment"), Value: aws.String(envProd)}},
		},
	}

	resources, err := testProvider().listLoadBalancersFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("listLoadBalancersFrom() error = %v", err)
	}

	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(resources))
	}
	if resources[0].ID != webCluster || resources[0].Name != webCluster {
		t.Errorf("ID/Name = %q/%q, want web/web", resources[0].ID, resources[0].Name)
	}
	if resources[0].Type != "aws_lb" {
		t.Errorf("Type = %q, want %q", resources[0].Type, "aws_lb")
	}
	if resources[0].ARN != arn {
		t.Errorf("ARN = %q, want %q", resources[0].ARN, arn)
	}
	if resources[0].Tags["environment"] != envProd {
		t.Errorf("Tags[environment] = %q, want %q", resources[0].Tags["environment"], envProd)
	}
	if resources[0].CreatedAt == nil || !resources[0].CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", resources[0].CreatedAt, created)
	}
}

// DescribeTags accepts at most 20 ARNs per call, so a larger set must be split.
func TestListLoadBalancers_BatchesTagRequests(t *testing.T) {
	const total = 45

	var lbs []elbv2types.LoadBalancer
	tags := make(map[string][]elbv2types.Tag, total)
	for i := 0; i < total; i++ {
		arn := fmt.Sprintf("arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/lb%d/x", i)
		lbs = append(lbs, elbv2types.LoadBalancer{
			LoadBalancerArn:  aws.String(arn),
			LoadBalancerName: aws.String(fmt.Sprintf("lb%d", i)),
		})
		tags[arn] = []elbv2types.Tag{{Key: aws.String("index"), Value: aws.String(fmt.Sprint(i))}}
	}

	mock := &mockELBv2Client{pages: [][]elbv2types.LoadBalancer{lbs}, tags: tags}

	resources, err := testProvider().listLoadBalancersFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("listLoadBalancersFrom() error = %v", err)
	}

	if len(resources) != total {
		t.Fatalf("got %d resources, want %d", len(resources), total)
	}

	wantBatches := []int{20, 20, 5}
	if len(mock.batchSizes) != len(wantBatches) {
		t.Fatalf("got %d tag batches (%v), want %d (%v)", len(mock.batchSizes), mock.batchSizes, len(wantBatches), wantBatches)
	}
	for i, want := range wantBatches {
		if mock.batchSizes[i] != want {
			t.Errorf("batch %d size = %d, want %d", i, mock.batchSizes[i], want)
		}
	}
	if len(mock.describeAll) != total {
		t.Errorf("DescribeTags covered %d ARNs, want %d", len(mock.describeAll), total)
	}

	// Every load balancer must carry the tags from its own batch.
	for _, r := range resources {
		if r.Tags["index"] == "" {
			t.Errorf("load balancer %q lost its tags across batching", r.Name)
		}
	}
}

func TestListLoadBalancers_UnreadableTagsSkipLoadBalancerNotReportUntagged(t *testing.T) {
	mock := &mockELBv2Client{
		pages: [][]elbv2types.LoadBalancer{{
			{
				LoadBalancerArn:  aws.String("arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/web/abc"),
				LoadBalancerName: aws.String(webCluster),
			},
		}},
		tagsErr: errors.New("access denied"),
	}

	p := testProvider()
	resources, err := p.listLoadBalancersFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("listLoadBalancersFrom() error = %v, want nil", err)
	}
	if len(resources) != 0 {
		t.Fatalf("resources = %+v, want none", resources)
	}
	if errors.Join(p.skipped.errs()...) == nil {
		t.Error("the skipped load balancer was not recorded")
	}
}

func TestListLoadBalancers_DeletedLoadBalancerDropsOnlyItselfFromItsBatch(t *testing.T) {
	const total = 25
	arnOf := func(i int) string {
		return fmt.Sprintf("arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/lb%d/x", i)
	}
	lbs := make([]elbv2types.LoadBalancer, 0, total)
	for i := range total {
		lbs = append(lbs, elbv2types.LoadBalancer{LoadBalancerArn: aws.String(arnOf(i)), LoadBalancerName: aws.String(fmt.Sprintf("lb%d", i))})
	}
	mock := &mockELBv2Client{pages: [][]elbv2types.LoadBalancer{lbs}, gone: map[string]bool{arnOf(3): true}}

	p := testProvider()
	resources, err := p.listLoadBalancersFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("listLoadBalancersFrom() error = %v, want nil", err)
	}
	if len(resources) != total-1 {
		t.Fatalf("got %d load balancers, want %d (only the deleted one dropped)", len(resources), total-1)
	}
	if err := errors.Join(p.skipped.errs()...); err != nil {
		t.Errorf("a deleted load balancer was counted as skipped: %v", err)
	}
}

func TestListLoadBalancers_BatchDeniedIsNotRetriedPerARN(t *testing.T) {
	lbs := make([]elbv2types.LoadBalancer, 0, elbTagBatchSize)
	for i := range elbTagBatchSize {
		lbs = append(lbs, elbv2types.LoadBalancer{
			LoadBalancerArn:  aws.String(fmt.Sprintf("arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/lb%d/x", i)),
			LoadBalancerName: aws.String(fmt.Sprintf("lb%d", i)),
		})
	}
	mock := &mockELBv2Client{
		pages:   [][]elbv2types.LoadBalancer{lbs},
		tagsErr: &smithy.GenericAPIError{Code: "AccessDenied", Message: "not authorized to perform elasticloadbalancing:DescribeTags"},
	}

	p := testProvider()
	resources, err := p.listLoadBalancersFrom(context.Background(), mock, "us-east-1")
	if err != nil || len(resources) != 0 {
		t.Fatalf("resources = %+v, err = %v, want none and no error", resources, err)
	}
	if mock.tagCalls != 1 {
		t.Errorf("DescribeTags calls = %d, want 1 for one denied batch", mock.tagCalls)
	}
}

func TestListLoadBalancers_ARNMissingFromDescribeTagsIsSkipped(t *testing.T) {
	arn := "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/web/abc"
	mock := &mockELBv2Client{
		pages: [][]elbv2types.LoadBalancer{{{LoadBalancerArn: aws.String(arn), LoadBalancerName: aws.String(webCluster)}}},
		omit:  map[string]bool{arn: true},
	}

	p := testProvider()
	resources, err := p.listLoadBalancersFrom(context.Background(), mock, "us-east-1")
	if err != nil || len(resources) != 0 {
		t.Fatalf("resources = %+v, err = %v, want none and no error", resources, err)
	}
	if errors.Join(p.skipped.errs()...) == nil {
		t.Error("the load balancer left out of DescribeTags was not recorded")
	}
}

func TestListTargetGroups_UnreadableFallbackTagsSkipTargetGroup(t *testing.T) {
	mock := &mockELBv2Client{
		targetPages: [][]elbv2types.TargetGroup{{{
			TargetGroupArn:  aws.String("arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/web/abc"),
			TargetGroupName: aws.String(webCluster),
		}}},
		tagsErr: errors.New("access denied"),
	}

	p := testProvider()
	resources, err := p.listTargetGroupsFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("listTargetGroupsFrom() error = %v, want nil", err)
	}
	if len(resources) != 0 {
		t.Fatalf("resources = %+v, want none", resources)
	}
	if errors.Join(p.skipped.errs()...) == nil {
		t.Error("the skipped target group was not recorded")
	}
}

func TestListLoadBalancers_Empty(t *testing.T) {
	mock := &mockELBv2Client{pages: [][]elbv2types.LoadBalancer{{}}}

	resources, err := testProvider().listLoadBalancersFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("listLoadBalancersFrom() error = %v", err)
	}
	if len(resources) != 0 {
		t.Errorf("got %d resources, want 0", len(resources))
	}
	if len(mock.batchSizes) != 0 {
		t.Errorf("DescribeTags called %d times for zero load balancers, want 0", len(mock.batchSizes))
	}
}

func TestListLoadBalancers_ListError(t *testing.T) {
	mock := &mockELBv2Client{listErr: errors.New("boom")}

	if _, err := testProvider().listLoadBalancersFrom(context.Background(), mock, "us-east-1"); err == nil {
		t.Fatal("listLoadBalancersFrom() error = nil, want an error")
	}
}

func TestListTargetGroups(t *testing.T) {
	web := "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/web/abc"
	api := "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/api/def"
	mock := &mockELBv2Client{
		targetPages: [][]elbv2types.TargetGroup{
			{{TargetGroupArn: aws.String(web), TargetGroupName: aws.String(webCluster)}},
			{{TargetGroupArn: aws.String(api), TargetGroupName: aws.String("api")}},
		},
		tags: map[string][]elbv2types.Tag{web: {{Key: aws.String("environment"), Value: aws.String(envProd)}}},
	}

	resources, err := testProvider().listTargetGroupsFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("got %d target groups across 2 pages, want 2", len(resources))
	}
	for _, r := range resources {
		if r.Type != "aws_lb_target_group" || r.Tags == nil {
			t.Errorf("unexpected resource: %+v", r)
		}
		if r.ID == webCluster && r.Tags["environment"] != envProd {
			t.Errorf("web tags = %v", r.Tags)
		}
	}
}

func TestListTargetGroups_BulkTagsSkipDescribeTags(t *testing.T) {
	web := "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/web/abc"
	p := testProvider()
	p.tagSources = map[string]*tagSource{defaultRegion: staticTagSource(map[string]map[string]string{web: {"owner": "x"}})}
	mock := &mockELBv2Client{
		targetPages: [][]elbv2types.TargetGroup{{{TargetGroupArn: aws.String(web), TargetGroupName: aws.String(webCluster)}}},
		tagsErr:     errors.New("must not be called"),
	}

	resources, err := p.listTargetGroupsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
	if len(mock.batchSizes) != 0 {
		t.Error("DescribeTags called although bulk tags were available")
	}
}

package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/accessanalyzer"
	aatypes "github.com/aws/aws-sdk-go-v2/service/accessanalyzer/types"
	"github.com/aws/aws-sdk-go-v2/service/acmpca"
	acmpcatypes "github.com/aws/aws-sdk-go-v2/service/acmpca/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cloudtrailtypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
	"github.com/aws/aws-sdk-go-v2/service/configservice"
	configtypes "github.com/aws/aws-sdk-go-v2/service/configservice/types"
	"github.com/aws/aws-sdk-go-v2/service/directoryservice"
	dstypes "github.com/aws/aws-sdk-go-v2/service/directoryservice/types"
	"github.com/aws/aws-sdk-go-v2/service/fms"
	fmstypes "github.com/aws/aws-sdk-go-v2/service/fms/types"
	"github.com/aws/aws-sdk-go-v2/service/guardduty"
	"github.com/aws/aws-sdk-go-v2/service/networkfirewall"
	nfwtypes "github.com/aws/aws-sdk-go-v2/service/networkfirewall/types"
	"github.com/aws/aws-sdk-go-v2/service/rolesanywhere"
	ratypes "github.com/aws/aws-sdk-go-v2/service/rolesanywhere/types"
	"github.com/aws/aws-sdk-go-v2/service/wafregional"
	wafregionaltypes "github.com/aws/aws-sdk-go-v2/service/wafregional/types"
	"github.com/aws/aws-sdk-go-v2/service/wafv2"
	wafv2types "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/aws/smithy-go"
)

type mockAccessAnalyzerClient struct {
	analyzers []aatypes.AnalyzerSummary
	err       error
}

func (m *mockAccessAnalyzerClient) ListAnalyzers(ctx context.Context, params *accessanalyzer.ListAnalyzersInput, optFns ...func(*accessanalyzer.Options)) (*accessanalyzer.ListAnalyzersOutput, error) {
	return &accessanalyzer.ListAnalyzersOutput{Analyzers: m.analyzers}, m.err
}

type mockACMPCAClient struct {
	cas []acmpcatypes.CertificateAuthority
	err error
}

func (m *mockACMPCAClient) ListCertificateAuthorities(ctx context.Context, params *acmpca.ListCertificateAuthoritiesInput, optFns ...func(*acmpca.Options)) (*acmpca.ListCertificateAuthoritiesOutput, error) {
	return &acmpca.ListCertificateAuthoritiesOutput{CertificateAuthorities: m.cas}, m.err
}

type mockCloudTrailClient struct {
	trails   []cloudtrailtypes.Trail
	tags     map[string][]cloudtrailtypes.Tag
	err      error
	tagsErr  error
	tagCalls int
}

func (m *mockCloudTrailClient) DescribeTrails(ctx context.Context, params *cloudtrail.DescribeTrailsInput, optFns ...func(*cloudtrail.Options)) (*cloudtrail.DescribeTrailsOutput, error) {
	return &cloudtrail.DescribeTrailsOutput{TrailList: m.trails}, m.err
}

func (m *mockCloudTrailClient) ListTags(ctx context.Context, params *cloudtrail.ListTagsInput, optFns ...func(*cloudtrail.Options)) (*cloudtrail.ListTagsOutput, error) {
	m.tagCalls++
	if m.tagsErr != nil {
		return nil, m.tagsErr
	}
	out := &cloudtrail.ListTagsOutput{}
	for _, id := range params.ResourceIdList {
		out.ResourceTagList = append(out.ResourceTagList, cloudtrailtypes.ResourceTag{ResourceId: aws.String(id), TagsList: m.tags[id]})
	}
	return out, nil
}

type mockConfigClient struct {
	rules []configtypes.ConfigRule
	err   error
}

func (m *mockConfigClient) DescribeConfigRules(ctx context.Context, params *configservice.DescribeConfigRulesInput, optFns ...func(*configservice.Options)) (*configservice.DescribeConfigRulesOutput, error) {
	return &configservice.DescribeConfigRulesOutput{ConfigRules: m.rules}, m.err
}

type mockDirectoryClient struct {
	dirs []dstypes.DirectoryDescription
	err  error
}

func (m *mockDirectoryClient) DescribeDirectories(ctx context.Context, params *directoryservice.DescribeDirectoriesInput, optFns ...func(*directoryservice.Options)) (*directoryservice.DescribeDirectoriesOutput, error) {
	return &directoryservice.DescribeDirectoriesOutput{DirectoryDescriptions: m.dirs}, m.err
}

type mockFMSClient struct {
	policies []fmstypes.PolicySummary
	err      error
}

func (m *mockFMSClient) ListPolicies(ctx context.Context, params *fms.ListPoliciesInput, optFns ...func(*fms.Options)) (*fms.ListPoliciesOutput, error) {
	return &fms.ListPoliciesOutput{PolicyList: m.policies}, m.err
}

type mockGuardDutyClient struct {
	ids  []string
	tags map[string]map[string]string
	err  error
}

func (m *mockGuardDutyClient) ListDetectors(ctx context.Context, params *guardduty.ListDetectorsInput, optFns ...func(*guardduty.Options)) (*guardduty.ListDetectorsOutput, error) {
	return &guardduty.ListDetectorsOutput{DetectorIds: m.ids}, m.err
}

func (m *mockGuardDutyClient) GetDetector(ctx context.Context, params *guardduty.GetDetectorInput, optFns ...func(*guardduty.Options)) (*guardduty.GetDetectorOutput, error) {
	return &guardduty.GetDetectorOutput{Tags: m.tags[aws.ToString(params.DetectorId)], CreatedAt: aws.String("2026-01-02T03:04:05.000Z")}, nil
}

type mockNetworkFirewallClient struct {
	firewalls []nfwtypes.FirewallMetadata
	err       error
}

func (m *mockNetworkFirewallClient) ListFirewalls(ctx context.Context, params *networkfirewall.ListFirewallsInput, optFns ...func(*networkfirewall.Options)) (*networkfirewall.ListFirewallsOutput, error) {
	return &networkfirewall.ListFirewallsOutput{Firewalls: m.firewalls}, m.err
}

type mockRolesAnywhereClient struct {
	anchors []ratypes.TrustAnchorDetail
	err     error
}

func (m *mockRolesAnywhereClient) ListTrustAnchors(ctx context.Context, params *rolesanywhere.ListTrustAnchorsInput, optFns ...func(*rolesanywhere.Options)) (*rolesanywhere.ListTrustAnchorsOutput, error) {
	return &rolesanywhere.ListTrustAnchorsOutput{TrustAnchors: m.anchors}, m.err
}

type mockWAFRegionalClient struct {
	acls []wafregionaltypes.WebACLSummary
	err  error
}

func (m *mockWAFRegionalClient) ListWebACLs(ctx context.Context, params *wafregional.ListWebACLsInput, optFns ...func(*wafregional.Options)) (*wafregional.ListWebACLsOutput, error) {
	return &wafregional.ListWebACLsOutput{WebACLs: m.acls}, m.err
}

type mockWAFv2Client struct {
	acls  []wafv2types.WebACLSummary
	err   error
	scope wafv2types.Scope
}

func (m *mockWAFv2Client) ListWebACLs(ctx context.Context, params *wafv2.ListWebACLsInput, optFns ...func(*wafv2.Options)) (*wafv2.ListWebACLsOutput, error) {
	m.scope = params.Scope
	return &wafv2.ListWebACLsOutput{WebACLs: m.acls}, m.err
}

// apiError builds a smithy API error with the given code.
type apiError struct{ code string }

func (e apiError) Error() string                 { return e.code }
func (e apiError) ErrorCode() string             { return e.code }
func (e apiError) ErrorMessage() string          { return e.code }
func (e apiError) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

// apiErrorMsg is an API error with a distinct message.
type apiErrorMsg struct{ code, msg string }

func (e apiErrorMsg) Error() string                 { return e.code + ": " + e.msg }
func (e apiErrorMsg) ErrorCode() string             { return e.code }
func (e apiErrorMsg) ErrorMessage() string          { return e.msg }
func (e apiErrorMsg) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

func TestListAnalyzers(t *testing.T) {
	mock := &mockAccessAnalyzerClient{analyzers: []aatypes.AnalyzerSummary{{Name: aws.String("acct"), Arn: aws.String("arn:aa:acct"), Tags: map[string]string{"owner": "x"}}}}
	resources, err := testProvider().listAnalyzersFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Type != "aws_accessanalyzer_analyzer" || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListCertificateAuthorities_SkipsDeleted(t *testing.T) {
	live := "arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/abc"
	p := bulkProvider(map[string]map[string]string{live: {"owner": "x"}})
	mock := &mockACMPCAClient{cas: []acmpcatypes.CertificateAuthority{
		{Arn: aws.String(live), Status: acmpcatypes.CertificateAuthorityStatusActive,
			CertificateAuthorityConfiguration: &acmpcatypes.CertificateAuthorityConfiguration{Subject: &acmpcatypes.ASN1Subject{CommonName: aws.String("Root CA")}}},
		{Arn: aws.String("arn:gone"), Status: acmpcatypes.CertificateAuthorityStatusDeleted},
	}}
	resources, err := p.listCertificateAuthoritiesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].ID != "abc" || resources[0].Name != "Root CA" || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListTrails_HomeRegionOnlyWithBulkTags(t *testing.T) {
	home := "arn:aws:cloudtrail:us-east-1:123456789012:trail/main"
	p := bulkProvider(map[string]map[string]string{home: {"environment": envProd}})
	mock := &mockCloudTrailClient{trails: []cloudtrailtypes.Trail{
		{Name: aws.String("main"), TrailARN: aws.String(home), HomeRegion: aws.String(defaultRegion)},
		{Name: aws.String("eu"), TrailARN: aws.String("arn:eu"), HomeRegion: aws.String("eu-west-1")},
	}}
	resources, err := p.listTrailsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Name != "main" || resources[0].Tags["environment"] != envProd {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
	if mock.tagCalls != 0 {
		t.Error("ListTags called although bulk tags were available")
	}
}

func TestListTrails_FallsBackToListTags(t *testing.T) {
	arn := "arn:aws:cloudtrail:us-east-1:123456789012:trail/main"
	mock := &mockCloudTrailClient{
		trails: []cloudtrailtypes.Trail{{Name: aws.String("main"), TrailARN: aws.String(arn), HomeRegion: aws.String(defaultRegion)}},
		tags:   map[string][]cloudtrailtypes.Tag{arn: {{Key: aws.String("owner"), Value: aws.String("x")}}},
	}
	resources, err := testProvider().listTrailsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Tags["owner"] != "x" || mock.tagCalls != 1 {
		t.Errorf("resources = %+v, err = %v, tagCalls = %d", resources, err, mock.tagCalls)
	}

	mock.tagsErr = errors.New("denied")
	resources, err = testProvider().listTrailsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 0 {
		t.Errorf("unreadable tags must skip the trails, got %+v, %v", resources, err)
	}
}

func TestListConfigRules_SkipsServiceCreated(t *testing.T) {
	p := bulkProvider(map[string]map[string]string{"arn:config:r": {"owner": "x"}})
	mock := &mockConfigClient{rules: []configtypes.ConfigRule{
		{ConfigRuleName: aws.String("r"), ConfigRuleArn: aws.String("arn:config:r")},
		{ConfigRuleName: aws.String("securityhub-x"), ConfigRuleArn: aws.String("arn:config:sh"), CreatedBy: aws.String("securityhub.amazonaws.com")},
	}}
	resources, err := p.listConfigRulesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Type != "aws_config_config_rule" || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListDirectories(t *testing.T) {
	arn := "arn:aws:ds:us-east-1:123456789012:directory/d-1"
	p := bulkProvider(map[string]map[string]string{arn: {"owner": "x"}})
	mock := &mockDirectoryClient{dirs: []dstypes.DirectoryDescription{{DirectoryId: aws.String("d-1"), Name: aws.String("corp.example.com")}}}
	resources, err := p.listDirectoriesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].ARN != arn || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListFMSPolicies(t *testing.T) {
	p := bulkProvider(nil)
	mock := &mockFMSClient{policies: []fmstypes.PolicySummary{{PolicyId: aws.String("p1"), PolicyName: aws.String("waf"), PolicyArn: aws.String("arn:fms:p1")}}}
	resources, err := p.listFMSPoliciesFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Type != "aws_fms_policy" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}

	for _, notAdmin := range []error{
		apiError{"InvalidOperationException"},
		apiErrorMsg{"AccessDeniedException", "Operation ListPolicies is only available to AWS Firewall Manager Administrators."},
		apiErrorMsg{"AccessDeniedException", "No default admin could be found for account 123456789012 in Region us-east-1"},
	} {
		if resources, err := p.listFMSPoliciesFrom(context.Background(), &mockFMSClient{err: notAdmin}, defaultRegion); err != nil || len(resources) != 0 {
			t.Errorf("%v: non-admin account must be skipped silently, got %+v, %v", notAdmin, resources, err)
		}
	}
	if _, err := p.listFMSPoliciesFrom(context.Background(), &mockFMSClient{err: apiErrorMsg{"AccessDeniedException", "no permission"}}, defaultRegion); err == nil {
		t.Error("a plain AccessDenied must stay an error")
	}
}

func TestListDetectors(t *testing.T) {
	mock := &mockGuardDutyClient{ids: []string{"det-1"}, tags: map[string]map[string]string{"det-1": {"owner": "x"}}}
	resources, err := testProvider().listDetectorsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].ARN != "arn:aws:guardduty:us-east-1:123456789012:detector/det-1" || resources[0].Tags["owner"] != "x" || resources[0].CreatedAt == nil {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListFirewalls(t *testing.T) {
	p := bulkProvider(map[string]map[string]string{"arn:nfw:fw": {"owner": "x"}})
	mock := &mockNetworkFirewallClient{firewalls: []nfwtypes.FirewallMetadata{{FirewallName: aws.String("fw"), FirewallArn: aws.String("arn:nfw:fw")}}}
	resources, err := p.listFirewallsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListTrustAnchors(t *testing.T) {
	p := bulkProvider(nil)
	mock := &mockRolesAnywhereClient{anchors: []ratypes.TrustAnchorDetail{{TrustAnchorId: aws.String("ta-1"), Name: aws.String("corp"), TrustAnchorArn: aws.String("arn:ra:ta-1")}}}
	resources, err := p.listTrustAnchorsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 1 || resources[0].ID != "ta-1" || resources[0].Name != "corp" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
}

func TestListWebACLs(t *testing.T) {
	regionalARN := "arn:aws:waf-regional:us-east-1:123456789012:webacl/w1"
	p := bulkProvider(map[string]map[string]string{regionalARN: {"owner": "x"}, "arn:wafv2:w2": {"owner": "y"}})

	classic, err := p.listRegionalWebACLsFrom(context.Background(), &mockWAFRegionalClient{acls: []wafregionaltypes.WebACLSummary{{WebACLId: aws.String("w1"), Name: aws.String("classic")}}}, defaultRegion)
	if err != nil || len(classic) != 1 || classic[0].ARN != regionalARN || classic[0].Tags["owner"] != "x" {
		t.Errorf("classic = %+v, err = %v", classic, err)
	}

	mock := &mockWAFv2Client{acls: []wafv2types.WebACLSummary{{Id: aws.String("w2"), Name: aws.String("v2"), ARN: aws.String("arn:wafv2:w2")}}}
	v2, err := p.listWAFv2WebACLsFrom(context.Background(), mock, defaultRegion, wafv2types.ScopeRegional)
	if err != nil || len(v2) != 1 || v2[0].Tags["owner"] != "y" || mock.scope != wafv2types.ScopeRegional {
		t.Errorf("v2 = %+v, err = %v, scope = %s", v2, err, mock.scope)
	}
}

func TestSecurityListers_SkipWithoutBulkTags(t *testing.T) {
	p := testProvider()
	ctx := context.Background()
	for name, list := range map[string]func() (int, error){
		"acmpca": func() (int, error) {
			r, err := p.listCertificateAuthoritiesFrom(ctx, &mockACMPCAClient{cas: []acmpcatypes.CertificateAuthority{{Arn: aws.String("arn:x")}}}, defaultRegion)
			return len(r), err
		},
		"config": func() (int, error) {
			r, err := p.listConfigRulesFrom(ctx, &mockConfigClient{rules: []configtypes.ConfigRule{{ConfigRuleName: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"ds": func() (int, error) {
			r, err := p.listDirectoriesFrom(ctx, &mockDirectoryClient{dirs: []dstypes.DirectoryDescription{{DirectoryId: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"fms": func() (int, error) {
			r, err := p.listFMSPoliciesFrom(ctx, &mockFMSClient{policies: []fmstypes.PolicySummary{{PolicyId: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"nfw": func() (int, error) {
			r, err := p.listFirewallsFrom(ctx, &mockNetworkFirewallClient{firewalls: []nfwtypes.FirewallMetadata{{FirewallName: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"rolesanywhere": func() (int, error) {
			r, err := p.listTrustAnchorsFrom(ctx, &mockRolesAnywhereClient{anchors: []ratypes.TrustAnchorDetail{{TrustAnchorId: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"waf-regional": func() (int, error) {
			r, err := p.listRegionalWebACLsFrom(ctx, &mockWAFRegionalClient{acls: []wafregionaltypes.WebACLSummary{{WebACLId: aws.String("x")}}}, defaultRegion)
			return len(r), err
		},
		"wafv2": func() (int, error) {
			r, err := p.listWAFv2WebACLsFrom(ctx, &mockWAFv2Client{acls: []wafv2types.WebACLSummary{{Id: aws.String("x")}}}, defaultRegion, wafv2types.ScopeRegional)
			return len(r), err
		},
	} {
		if n, err := list(); err != nil || n != 0 {
			t.Errorf("%s: want no resources and no error without bulk tags, got %d, %v", name, n, err)
		}
	}
}

func TestSecurityListers_Error(t *testing.T) {
	boom := errors.New("boom")
	p := bulkProvider(nil)
	ctx := context.Background()
	for name, list := range map[string]func() error{
		"accessanalyzer": func() error {
			_, err := p.listAnalyzersFrom(ctx, &mockAccessAnalyzerClient{err: boom}, defaultRegion)
			return err
		},
		"acmpca": func() error {
			_, err := p.listCertificateAuthoritiesFrom(ctx, &mockACMPCAClient{err: boom}, defaultRegion)
			return err
		},
		"cloudtrail": func() error {
			_, err := p.listTrailsFrom(ctx, &mockCloudTrailClient{err: boom}, defaultRegion)
			return err
		},
		"config": func() error {
			_, err := p.listConfigRulesFrom(ctx, &mockConfigClient{err: boom}, defaultRegion)
			return err
		},
		"ds": func() error {
			_, err := p.listDirectoriesFrom(ctx, &mockDirectoryClient{err: boom}, defaultRegion)
			return err
		},
		"fms": func() error {
			_, err := p.listFMSPoliciesFrom(ctx, &mockFMSClient{err: boom}, defaultRegion)
			return err
		},
		"guardduty": func() error {
			_, err := p.listDetectorsFrom(ctx, &mockGuardDutyClient{err: boom}, defaultRegion)
			return err
		},
		"nfw": func() error {
			_, err := p.listFirewallsFrom(ctx, &mockNetworkFirewallClient{err: boom}, defaultRegion)
			return err
		},
		"rolesanywhere": func() error {
			_, err := p.listTrustAnchorsFrom(ctx, &mockRolesAnywhereClient{err: boom}, defaultRegion)
			return err
		},
		"waf-regional": func() error {
			_, err := p.listRegionalWebACLsFrom(ctx, &mockWAFRegionalClient{err: boom}, defaultRegion)
			return err
		},
		"wafv2": func() error {
			_, err := p.listWAFv2WebACLsFrom(ctx, &mockWAFv2Client{err: boom}, defaultRegion, wafv2types.ScopeRegional)
			return err
		},
	} {
		if list() == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

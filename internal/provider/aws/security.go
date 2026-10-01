package aws

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/accessanalyzer"
	"github.com/aws/aws-sdk-go-v2/service/acmpca"
	acmpcatypes "github.com/aws/aws-sdk-go-v2/service/acmpca/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cloudtrailtypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
	"github.com/aws/aws-sdk-go-v2/service/configservice"
	"github.com/aws/aws-sdk-go-v2/service/directoryservice"
	"github.com/aws/aws-sdk-go-v2/service/fms"
	"github.com/aws/aws-sdk-go-v2/service/guardduty"
	"github.com/aws/aws-sdk-go-v2/service/networkfirewall"
	"github.com/aws/aws-sdk-go-v2/service/rolesanywhere"
	"github.com/aws/aws-sdk-go-v2/service/wafregional"
	"github.com/aws/aws-sdk-go-v2/service/wafv2"
	wafv2types "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/aws/smithy-go"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// Security, identity and governance services.

type accessAnalyzerAPI interface {
	ListAnalyzers(ctx context.Context, params *accessanalyzer.ListAnalyzersInput, optFns ...func(*accessanalyzer.Options)) (*accessanalyzer.ListAnalyzersOutput, error)
}

type acmpcaAPI interface {
	ListCertificateAuthorities(ctx context.Context, params *acmpca.ListCertificateAuthoritiesInput, optFns ...func(*acmpca.Options)) (*acmpca.ListCertificateAuthoritiesOutput, error)
}

type cloudTrailAPI interface {
	DescribeTrails(ctx context.Context, params *cloudtrail.DescribeTrailsInput, optFns ...func(*cloudtrail.Options)) (*cloudtrail.DescribeTrailsOutput, error)
	ListTags(ctx context.Context, params *cloudtrail.ListTagsInput, optFns ...func(*cloudtrail.Options)) (*cloudtrail.ListTagsOutput, error)
}

type configAPI interface {
	DescribeConfigRules(ctx context.Context, params *configservice.DescribeConfigRulesInput, optFns ...func(*configservice.Options)) (*configservice.DescribeConfigRulesOutput, error)
}

type directoryAPI interface {
	DescribeDirectories(ctx context.Context, params *directoryservice.DescribeDirectoriesInput, optFns ...func(*directoryservice.Options)) (*directoryservice.DescribeDirectoriesOutput, error)
}

type fmsAPI interface {
	ListPolicies(ctx context.Context, params *fms.ListPoliciesInput, optFns ...func(*fms.Options)) (*fms.ListPoliciesOutput, error)
}

type guardDutyAPI interface {
	ListDetectors(ctx context.Context, params *guardduty.ListDetectorsInput, optFns ...func(*guardduty.Options)) (*guardduty.ListDetectorsOutput, error)
	GetDetector(ctx context.Context, params *guardduty.GetDetectorInput, optFns ...func(*guardduty.Options)) (*guardduty.GetDetectorOutput, error)
}

type networkFirewallAPI interface {
	ListFirewalls(ctx context.Context, params *networkfirewall.ListFirewallsInput, optFns ...func(*networkfirewall.Options)) (*networkfirewall.ListFirewallsOutput, error)
}

type rolesAnywhereAPI interface {
	ListTrustAnchors(ctx context.Context, params *rolesanywhere.ListTrustAnchorsInput, optFns ...func(*rolesanywhere.Options)) (*rolesanywhere.ListTrustAnchorsOutput, error)
}

type wafRegionalAPI interface {
	ListWebACLs(ctx context.Context, params *wafregional.ListWebACLsInput, optFns ...func(*wafregional.Options)) (*wafregional.ListWebACLsOutput, error)
}

type wafv2API interface {
	ListWebACLs(ctx context.Context, params *wafv2.ListWebACLsInput, optFns ...func(*wafv2.Options)) (*wafv2.ListWebACLsOutput, error)
}

// cloudTrailTagBatch is the maximum number of trails per ListTags call.
const cloudTrailTagBatch = 20

func (p *Provider) listAnalyzers(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listAnalyzersFrom(ctx, regionalClient(p, region, accessanalyzer.NewFromConfig), region)
}

func (p *Provider) listAnalyzersFrom(ctx context.Context, client accessAnalyzerAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	paginator := accessanalyzer.NewListAnalyzersPaginator(client, &accessanalyzer.ListAnalyzersInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_analyzers", "", err)
		}
		for _, a := range output.Analyzers {
			name := aws.ToString(a.Name)
			resources = append(resources, p.resource(region, "aws_accessanalyzer_analyzer", name, name, aws.ToString(a.Arn), a.Tags, a.CreatedAt))
		}
	}
	log.Debug("AWS Access Analyzer: Found %d analyzers in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listCertificateAuthorities(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listCertificateAuthoritiesFrom(ctx, regionalClient(p, region, acmpca.NewFromConfig), region)
}

func (p *Provider) listCertificateAuthoritiesFrom(ctx context.Context, client acmpcaAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "ACM PCA") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := acmpca.NewListCertificateAuthoritiesPaginator(client, &acmpca.ListCertificateAuthoritiesInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_certificate_authorities", "", err)
		}
		for _, ca := range output.CertificateAuthorities {
			if ca.Status == acmpcatypes.CertificateAuthorityStatusDeleted {
				continue
			}
			arn := aws.ToString(ca.Arn)
			name := nameFromARN(arn)
			if ca.CertificateAuthorityConfiguration != nil && ca.CertificateAuthorityConfiguration.Subject != nil {
				if cn := aws.ToString(ca.CertificateAuthorityConfiguration.Subject.CommonName); cn != "" {
					name = cn
				}
			}
			resources = append(resources, p.bulkResource(region, "aws_acmpca_certificate_authority", nameFromARN(arn), name, arn, ca.CreatedAt))
		}
	}
	log.Debug("AWS ACM PCA: Found %d certificate authorities in %s", len(resources), region)
	return resources, nil
}

// listTrails lists the trails whose home region is region, so a multi-region
// trail is reported once.
func (p *Provider) listTrails(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listTrailsFrom(ctx, regionalClient(p, region, cloudtrail.NewFromConfig), region)
}

func (p *Provider) listTrailsFrom(ctx context.Context, client cloudTrailAPI, region string) ([]types.Resource, error) {
	output, err := client.DescribeTrails(ctx, &cloudtrail.DescribeTrailsInput{IncludeShadowTrails: aws.Bool(false)})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_trails", "", err)
	}
	var trails []cloudtrailtypes.Trail
	for _, t := range output.TrailList {
		if aws.ToString(t.HomeRegion) == region {
			trails = append(trails, t)
		}
	}

	arns := make([]string, 0, len(trails))
	for _, t := range trails {
		arns = append(arns, aws.ToString(t.TrailARN))
	}
	tags, err := p.trailTags(ctx, client, region, arns)
	if err != nil {
		for _, t := range trails {
			p.skipResource(ctx, "CloudTrail", region, "trail "+aws.ToString(t.Name), err)
		}
		return nil, nil
	}

	resources := make([]types.Resource, 0, len(trails))
	for _, t := range trails {
		name := aws.ToString(t.Name)
		arn := aws.ToString(t.TrailARN)
		resources = append(resources, p.resource(region, "aws_cloudtrail", name, name, arn, tags[arn], nil))
	}
	log.Debug("AWS CloudTrail: Found %d trails in %s", len(resources), region)
	return resources, nil
}

// trailTags reads trail tags from the bulk source, or through ListTags in
// batches when it is unavailable.
func (p *Provider) trailTags(ctx context.Context, client cloudTrailAPI, region string, arns []string) (map[string]map[string]string, error) {
	tags := make(map[string]map[string]string, len(arns))
	if p.tagsFor(region).available() {
		for _, arn := range arns {
			tags[arn] = p.bulkTags(region, arn)
		}
		return tags, nil
	}
	for _, batch := range chunk(arns, cloudTrailTagBatch) {
		output, err := client.ListTags(ctx, &cloudtrail.ListTagsInput{ResourceIdList: batch})
		if err != nil {
			return nil, err
		}
		for _, rt := range output.ResourceTagList {
			tags[aws.ToString(rt.ResourceId)] = tagsToMap(rt.TagsList,
				func(t cloudtrailtypes.Tag) *string { return t.Key },
				func(t cloudtrailtypes.Tag) *string { return t.Value })
		}
	}
	return tags, nil
}

func (p *Provider) listConfigRules(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listConfigRulesFrom(ctx, regionalClient(p, region, configservice.NewFromConfig), region)
}

// listConfigRulesFrom lists the account's own Config rules. Rules created by
// another service (Security Hub, conformance packs) carry CreatedBy and are
// managed by that service.
func (p *Provider) listConfigRulesFrom(ctx context.Context, client configAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "Config") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := configservice.NewDescribeConfigRulesPaginator(client, &configservice.DescribeConfigRulesInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_config_rules", "", err)
		}
		for _, r := range output.ConfigRules {
			if r.CreatedBy != nil {
				continue
			}
			name := aws.ToString(r.ConfigRuleName)
			resources = append(resources, p.bulkResource(region, "aws_config_config_rule", name, name, aws.ToString(r.ConfigRuleArn), nil))
		}
	}
	log.Debug("AWS Config: Found %d rules in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listDirectories(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listDirectoriesFrom(ctx, regionalClient(p, region, directoryservice.NewFromConfig), region)
}

func (p *Provider) listDirectoriesFrom(ctx context.Context, client directoryAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "Directory Service") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := directoryservice.NewDescribeDirectoriesPaginator(client, &directoryservice.DescribeDirectoriesInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_directories", "", err)
		}
		for _, d := range output.DirectoryDescriptions {
			id := aws.ToString(d.DirectoryId)
			arn := fmt.Sprintf("arn:aws:ds:%s:%s:directory/%s", region, p.accountID, id)
			resources = append(resources, p.bulkResource(region, "aws_directory_service_directory", id, aws.ToString(d.Name), arn, d.LaunchTime))
		}
	}
	log.Debug("AWS Directory Service: Found %d directories in %s", len(resources), region)
	return resources, nil
}

// listFMSPolicies lists Firewall Manager policies. Outside the FMS
// administrator account the API rejects the call, which is not an error.
func (p *Provider) listFMSPolicies(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listFMSPoliciesFrom(ctx, regionalClient(p, region, fms.NewFromConfig), region)
}

func (p *Provider) listFMSPoliciesFrom(ctx context.Context, client fmsAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "Firewall Manager") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := fms.NewListPoliciesPaginator(client, &fms.ListPoliciesInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			if notSubscribed(err) || isFMSNonAdmin(err) {
				log.Debug("AWS Firewall Manager: not the administrator account in %s, skipping", region)
				return nil, nil
			}
			return nil, provider.NewProviderError(providerName, "list_fms_policies", "", err)
		}
		for _, pol := range output.PolicyList {
			resources = append(resources, p.bulkResource(region, "aws_fms_policy", aws.ToString(pol.PolicyId), aws.ToString(pol.PolicyName), aws.ToString(pol.PolicyArn), nil))
		}
	}
	log.Debug("AWS Firewall Manager: Found %d policies in %s", len(resources), region)
	return resources, nil
}

// isFMSNonAdmin reports the AccessDenied FMS answers from any account that is
// not the Firewall Manager administrator, or whose organization has none.
func isFMSNonAdmin(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDeniedException" {
		return false
	}
	msg := apiErr.ErrorMessage()
	return strings.Contains(msg, "Firewall Manager Administrator") ||
		strings.Contains(msg, "No default admin could be found")
}

func (p *Provider) listDetectors(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listDetectorsFrom(ctx, regionalClient(p, region, guardduty.NewFromConfig), region)
}

func (p *Provider) listDetectorsFrom(ctx context.Context, client guardDutyAPI, region string) ([]types.Resource, error) {
	var ids []string
	paginator := guardduty.NewListDetectorsPaginator(client, &guardduty.ListDetectorsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_detectors", "", err)
		}
		ids = append(ids, output.DetectorIds...)
	}

	resources := forEachConcurrently(ids, func(id string) []types.Resource {
		detector, err := client.GetDetector(ctx, &guardduty.GetDetectorInput{DetectorId: aws.String(id)})
		if err != nil {
			p.skipResource(ctx, "GuardDuty", region, "detector "+id, err)
			return nil
		}
		arn := fmt.Sprintf("arn:aws:guardduty:%s:%s:detector/%s", region, p.accountID, id)
		return one(p.resource(region, "aws_guardduty_detector", id, id, arn, detector.Tags, parseRFC3339(detector.CreatedAt)))
	})
	log.Debug("AWS GuardDuty: Found %d detectors in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listFirewalls(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listFirewallsFrom(ctx, regionalClient(p, region, networkfirewall.NewFromConfig), region)
}

func (p *Provider) listFirewallsFrom(ctx context.Context, client networkFirewallAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "Network Firewall") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := networkfirewall.NewListFirewallsPaginator(client, &networkfirewall.ListFirewallsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_firewalls", "", err)
		}
		for _, f := range output.Firewalls {
			name := aws.ToString(f.FirewallName)
			resources = append(resources, p.bulkResource(region, "aws_networkfirewall_firewall", name, name, aws.ToString(f.FirewallArn), nil))
		}
	}
	log.Debug("AWS Network Firewall: Found %d firewalls in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listTrustAnchors(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listTrustAnchorsFrom(ctx, regionalClient(p, region, rolesanywhere.NewFromConfig), region)
}

func (p *Provider) listTrustAnchorsFrom(ctx context.Context, client rolesAnywhereAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "Roles Anywhere") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := rolesanywhere.NewListTrustAnchorsPaginator(client, &rolesanywhere.ListTrustAnchorsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_trust_anchors", "", err)
		}
		for _, ta := range output.TrustAnchors {
			resources = append(resources, p.bulkResource(region, "aws_rolesanywhere_trust_anchor", aws.ToString(ta.TrustAnchorId), aws.ToString(ta.Name), aws.ToString(ta.TrustAnchorArn), ta.CreatedAt))
		}
	}
	log.Debug("AWS Roles Anywhere: Found %d trust anchors in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listRegionalWebACLs(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listRegionalWebACLsFrom(ctx, regionalClient(p, region, wafregional.NewFromConfig), region)
}

// listRegionalWebACLsFrom lists WAF Classic regional web ACLs.
func (p *Provider) listRegionalWebACLsFrom(ctx context.Context, client wafRegionalAPI, region string) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "WAF Classic") {
		return nil, nil
	}
	var resources []types.Resource
	var marker *string
	for {
		output, err := client.ListWebACLs(ctx, &wafregional.ListWebACLsInput{NextMarker: marker})
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_waf_regional_web_acls", "", err)
		}
		for _, acl := range output.WebACLs {
			id := aws.ToString(acl.WebACLId)
			arn := fmt.Sprintf("arn:aws:waf-regional:%s:%s:webacl/%s", region, p.accountID, id)
			resources = append(resources, p.bulkResource(region, "aws_wafregional_web_acl", id, aws.ToString(acl.Name), arn, nil))
		}
		if output.NextMarker == nil || len(output.WebACLs) == 0 {
			break
		}
		marker = output.NextMarker
	}
	log.Debug("AWS WAF Classic: Found %d regional web ACLs in %s", len(resources), region)
	return resources, nil
}

func (p *Provider) listWAFv2RegionalWebACLs(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listWAFv2WebACLsFrom(ctx, regionalClient(p, region, wafv2.NewFromConfig), region, wafv2types.ScopeRegional)
}

// listWAFv2WebACLsFrom lists WAFv2 web ACLs of one scope. The CLOUDFRONT
// scope only exists in us-east-1 and is handled by the global listers.
func (p *Provider) listWAFv2WebACLsFrom(ctx context.Context, client wafv2API, region string, scope wafv2types.Scope) ([]types.Resource, error) {
	if !p.requireBulkTags(region, "WAFv2") {
		return nil, nil
	}
	var resources []types.Resource
	var marker *string
	for {
		output, err := client.ListWebACLs(ctx, &wafv2.ListWebACLsInput{Scope: scope, NextMarker: marker})
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_wafv2_web_acls", "", err)
		}
		for _, acl := range output.WebACLs {
			resources = append(resources, p.bulkResource(region, "aws_wafv2_web_acl", aws.ToString(acl.Id), aws.ToString(acl.Name), aws.ToString(acl.ARN), nil))
		}
		if output.NextMarker == nil || len(output.WebACLs) == 0 {
			break
		}
		marker = output.NextMarker
	}
	log.Debug("AWS WAFv2: Found %d %s web ACLs in %s", len(resources), scope, region)
	return resources, nil
}

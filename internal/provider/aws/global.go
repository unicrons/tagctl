package aws

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/route53"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// Global services are discovered once, not per region, and report their tags
// through the us-east-1 bulk source.

const globalRegion = defaultRegion

type route53API interface {
	ListHostedZones(ctx context.Context, params *route53.ListHostedZonesInput, optFns ...func(*route53.Options)) (*route53.ListHostedZonesOutput, error)
}

type cloudFrontAPI interface {
	ListDistributions(ctx context.Context, params *cloudfront.ListDistributionsInput, optFns ...func(*cloudfront.Options)) (*cloudfront.ListDistributionsOutput, error)
}

type iamAPI interface {
	ListRoles(ctx context.Context, params *iam.ListRolesInput, optFns ...func(*iam.Options)) (*iam.ListRolesOutput, error)
}

// globalLister names a discovery function that runs once per account.
type globalLister struct {
	label string
	list  func(ctx context.Context) ([]types.Resource, error)
}

// globalListers returns the account-wide discovery functions. S3 is handled
// separately because its buckets are filtered by region.
func (p *Provider) globalListers() []globalLister {
	return []globalLister{
		{"Route 53 hosted zones", p.listHostedZones},
		{"CloudFront distributions", p.listDistributions},
		{"IAM roles", p.listIAMRoles},
		{"Shield protections", p.listProtections},
		{"WAF Classic global web ACLs", p.listGlobalWebACLs},
		{"WAFv2 CloudFront web ACLs", p.listCloudFrontWebACLs},
		{"Global Accelerator accelerators", p.listAccelerators},
	}
}

func (p *Provider) listHostedZones(ctx context.Context) ([]types.Resource, error) {
	return p.listHostedZonesFrom(ctx, p.getRoute53Client())
}

func (p *Provider) listHostedZonesFrom(ctx context.Context, client route53API) ([]types.Resource, error) {
	if !p.requireBulkTags(globalRegion, "Route 53") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := route53.NewListHostedZonesPaginator(client, &route53.ListHostedZonesInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError("aws", "list_hosted_zones", "", err)
		}
		for _, z := range output.HostedZones {
			id := strings.TrimPrefix(aws.ToString(z.Id), "/hostedzone/")
			arn := "arn:aws:route53:::hostedzone/" + id
			resources = append(resources, types.Resource{
				ID: id, Name: strings.TrimSuffix(aws.ToString(z.Name), "."), ARN: arn, Type: "aws_route53_zone",
				Region: regionGlobal, Account: p.accountID, Provider: "aws",
				Tags: p.bulkTags(globalRegion, arn),
			})
		}
	}
	log.Debug("AWS Route 53: Found %d hosted zones", len(resources))
	return resources, nil
}

func (p *Provider) listDistributions(ctx context.Context) ([]types.Resource, error) {
	return p.listDistributionsFrom(ctx, p.getCloudFrontClient())
}

func (p *Provider) listDistributionsFrom(ctx context.Context, client cloudFrontAPI) ([]types.Resource, error) {
	if !p.requireBulkTags(globalRegion, "CloudFront") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := cloudfront.NewListDistributionsPaginator(client, &cloudfront.ListDistributionsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError("aws", "list_distributions", "", err)
		}
		if output.DistributionList == nil {
			continue
		}
		for _, d := range output.DistributionList.Items {
			arn := aws.ToString(d.ARN)
			resources = append(resources, types.Resource{
				ID: aws.ToString(d.Id), Name: aws.ToString(d.DomainName), ARN: arn, Type: "aws_cloudfront_distribution",
				Region: regionGlobal, Account: p.accountID, Provider: "aws",
				Tags: p.bulkTags(globalRegion, arn),
			})
		}
	}
	log.Debug("AWS CloudFront: Found %d distributions", len(resources))
	return resources, nil
}

// listIAMRoles lists customer-managed IAM roles. Service-linked roles are
// owned by AWS and cannot be tagged.
func (p *Provider) listIAMRoles(ctx context.Context) ([]types.Resource, error) {
	return p.listIAMRolesFrom(ctx, p.getIAMClient())
}

func (p *Provider) listIAMRolesFrom(ctx context.Context, client iamAPI) ([]types.Resource, error) {
	if !p.requireBulkTags(globalRegion, "IAM") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := iam.NewListRolesPaginator(client, &iam.ListRolesInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError("aws", "list_iam_roles", "", err)
		}
		for _, r := range output.Roles {
			if strings.HasPrefix(aws.ToString(r.Path), "/aws-service-role/") {
				continue
			}
			name := aws.ToString(r.RoleName)
			arn := aws.ToString(r.Arn)
			resources = append(resources, types.Resource{
				ID: name, Name: name, ARN: arn, Type: "aws_iam_role",
				Region: regionGlobal, Account: p.accountID, Provider: "aws",
				Tags: p.bulkTags(globalRegion, arn), CreatedAt: r.CreateDate,
			})
		}
	}
	log.Debug("AWS IAM: Found %d roles", len(resources))
	return resources, nil
}

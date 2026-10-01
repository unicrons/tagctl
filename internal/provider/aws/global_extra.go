package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/globalaccelerator"
	gatypes "github.com/aws/aws-sdk-go-v2/service/globalaccelerator/types"
	"github.com/aws/aws-sdk-go-v2/service/shield"
	"github.com/aws/aws-sdk-go-v2/service/waf"
	"github.com/aws/aws-sdk-go-v2/service/wafv2"
	wafv2types "github.com/aws/aws-sdk-go-v2/service/wafv2/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// Global services beyond global.go: Shield, WAF Classic (CloudFront) and
// WAFv2 CLOUDFRONT scope live in the partition's global region; Global
// Accelerator in us-west-2 of the commercial partition.

// globalAcceleratorRegion is the only endpoint of the Global Accelerator API.
const globalAcceleratorRegion = "us-west-2"

type shieldAPI interface {
	ListProtections(ctx context.Context, params *shield.ListProtectionsInput, optFns ...func(*shield.Options)) (*shield.ListProtectionsOutput, error)
}

type wafGlobalAPI interface {
	ListWebACLs(ctx context.Context, params *waf.ListWebACLsInput, optFns ...func(*waf.Options)) (*waf.ListWebACLsOutput, error)
}

type globalAcceleratorAPI interface {
	ListAccelerators(ctx context.Context, params *globalaccelerator.ListAcceleratorsInput, optFns ...func(*globalaccelerator.Options)) (*globalaccelerator.ListAcceleratorsOutput, error)
	ListTagsForResource(ctx context.Context, params *globalaccelerator.ListTagsForResourceInput, optFns ...func(*globalaccelerator.Options)) (*globalaccelerator.ListTagsForResourceOutput, error)
	TagResource(ctx context.Context, params *globalaccelerator.TagResourceInput, optFns ...func(*globalaccelerator.Options)) (*globalaccelerator.TagResourceOutput, error)
}

// listProtections lists Shield Advanced protections; without a subscription
// the API answers ResourceNotFound, which is not an error.
func (p *Provider) listProtections(ctx context.Context) ([]types.Resource, error) {
	return p.listProtectionsFrom(ctx, regionalClient(p, p.globalRegion(), shield.NewFromConfig))
}

func (p *Provider) listProtectionsFrom(ctx context.Context, client shieldAPI) ([]types.Resource, error) {
	if !p.requireBulkTags(p.globalRegion(), "Shield") {
		return nil, nil
	}
	var resources []types.Resource
	paginator := shield.NewListProtectionsPaginator(client, &shield.ListProtectionsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			if notSubscribed(err) {
				log.Debug("AWS Shield: no Shield Advanced subscription, skipping")
				return nil, nil
			}
			return nil, provider.NewProviderError(providerName, "list_shield_protections", "", err)
		}
		for _, pr := range output.Protections {
			r := p.bulkResource(p.globalRegion(), "aws_shield_protection", aws.ToString(pr.Id), aws.ToString(pr.Name), aws.ToString(pr.ProtectionArn), nil)
			r.Region = regionGlobal
			resources = append(resources, r)
		}
	}
	log.Debug("AWS Shield: Found %d protections", len(resources))
	return resources, nil
}

func (p *Provider) listGlobalWebACLs(ctx context.Context) ([]types.Resource, error) {
	return p.listGlobalWebACLsFrom(ctx, regionalClient(p, p.globalRegion(), waf.NewFromConfig))
}

// listGlobalWebACLsFrom lists WAF Classic web ACLs attached to CloudFront.
func (p *Provider) listGlobalWebACLsFrom(ctx context.Context, client wafGlobalAPI) ([]types.Resource, error) {
	if !p.requireBulkTags(p.globalRegion(), "WAF Classic") {
		return nil, nil
	}
	var resources []types.Resource
	err := paginate(func(marker *string) (*string, error) {
		output, err := client.ListWebACLs(ctx, &waf.ListWebACLsInput{NextMarker: marker})
		if err != nil {
			return nil, err
		}
		for _, acl := range output.WebACLs {
			id := aws.ToString(acl.WebACLId)
			arn := p.buildARN("waf", "", p.accountID, "webacl/"+id)
			r := p.bulkResource(p.globalRegion(), "aws_waf_web_acl", id, aws.ToString(acl.Name), arn, nil)
			r.Region = regionGlobal
			resources = append(resources, r)
		}
		return output.NextMarker, nil
	})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_waf_web_acls", "", err)
	}
	log.Debug("AWS WAF Classic: Found %d global web ACLs", len(resources))
	return resources, nil
}

// listCloudFrontWebACLs lists WAFv2 web ACLs of the CLOUDFRONT scope.
func (p *Provider) listCloudFrontWebACLs(ctx context.Context) ([]types.Resource, error) {
	resources, err := p.listWAFv2WebACLsFrom(ctx, regionalClient(p, p.globalRegion(), wafv2.NewFromConfig), p.globalRegion(), wafv2types.ScopeCloudfront)
	for i := range resources {
		resources[i].Region = regionGlobal
	}
	return resources, err
}

func (p *Provider) listAccelerators(ctx context.Context) ([]types.Resource, error) {
	if p.partitionID() != partitionAWS {
		log.Debug("AWS Global Accelerator: not available in partition %s", p.partitionID())
		return nil, nil
	}
	return p.listAcceleratorsFrom(ctx, regionalClient(p, globalAcceleratorRegion, globalaccelerator.NewFromConfig))
}

// listAcceleratorsFrom lists Global Accelerator accelerators. Their tags are
// read through the service API because the bulk source only covers the
// configured regions and the global region, not us-west-2.
func (p *Provider) listAcceleratorsFrom(ctx context.Context, client globalAcceleratorAPI) ([]types.Resource, error) {
	var accelerators []gatypes.Accelerator
	paginator := globalaccelerator.NewListAcceleratorsPaginator(client, &globalaccelerator.ListAcceleratorsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_accelerators", "", err)
		}
		accelerators = append(accelerators, output.Accelerators...)
	}

	resources := forEachConcurrently(accelerators, func(a gatypes.Accelerator) []types.Resource {
		arn := aws.ToString(a.AcceleratorArn)
		output, err := client.ListTagsForResource(ctx, &globalaccelerator.ListTagsForResourceInput{ResourceArn: aws.String(arn)})
		if err != nil {
			p.skipResource(ctx, "Global Accelerator", regionGlobal, arn, err)
			return nil
		}
		tags := tagsToMap(output.Tags,
			func(t gatypes.Tag) *string { return t.Key },
			func(t gatypes.Tag) *string { return t.Value })
		return one(p.resource(regionGlobal, "aws_globalaccelerator_accelerator", nameFromARN(arn), aws.ToString(a.Name), arn, tags, a.CreatedTime))
	})
	log.Debug("AWS Global Accelerator: Found %d accelerators", len(resources))
	return resources, nil
}

// applyGlobalAcceleratorTags tags an accelerator through its own API, which
// only exists in us-west-2.
func (p *Provider) applyGlobalAcceleratorTags(ctx context.Context, arn string, tags map[string]string) error {
	return applyGlobalAcceleratorTagsWith(ctx, regionalClient(p, globalAcceleratorRegion, globalaccelerator.NewFromConfig), arn, tags)
}

func applyGlobalAcceleratorTagsWith(ctx context.Context, client globalAcceleratorAPI, arn string, tags map[string]string) error {
	gaTags := make([]gatypes.Tag, 0, len(tags))
	for k, v := range tags {
		gaTags = append(gaTags, gatypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	if _, err := client.TagResource(ctx, &globalaccelerator.TagResourceInput{ResourceArn: aws.String(arn), Tags: gaTags}); err != nil {
		return provider.NewProviderError(providerName, "apply_global_accelerator_tags", arn, err)
	}
	log.Debug("AWS Global Accelerator: Applied %d tags to %s", len(tags), arn)
	return nil
}

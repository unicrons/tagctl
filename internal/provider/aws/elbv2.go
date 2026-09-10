package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// elbTagBatchSize is the maximum number of ARNs accepted by DescribeTags.
const elbTagBatchSize = 20

// elbv2API is the subset of the ELBv2 API used for discovery and tagging.
type elbv2API interface {
	DescribeLoadBalancers(ctx context.Context, params *elbv2.DescribeLoadBalancersInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeLoadBalancersOutput, error)
	DescribeTags(ctx context.Context, params *elbv2.DescribeTagsInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeTagsOutput, error)
	DescribeTargetGroups(ctx context.Context, params *elbv2.DescribeTargetGroupsInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeTargetGroupsOutput, error)
}

// listLoadBalancers lists all Application and Network Load Balancers in a region.
func (p *Provider) listLoadBalancers(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listLoadBalancersFrom(ctx, p.getELBv2Client(region), region)
}

// listLoadBalancersFrom lists load balancers using the given client.
func (p *Provider) listLoadBalancersFrom(ctx context.Context, client elbv2API, region string) ([]types.Resource, error) {
	log.Debug("AWS ELBv2: Listing load balancers in region %s...", region)

	var loadBalancers []elbv2types.LoadBalancer
	paginator := elbv2.NewDescribeLoadBalancersPaginator(client, &elbv2.DescribeLoadBalancersInput{})

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError("aws", "list_load_balancers", "", err)
		}
		loadBalancers = append(loadBalancers, output.LoadBalancers...)
	}

	if len(loadBalancers) == 0 {
		log.Debug("AWS ELBv2: Found 0 load balancers in %s", region)
		return nil, nil
	}

	arns := make([]string, 0, len(loadBalancers))
	for _, lb := range loadBalancers {
		if arn := aws.ToString(lb.LoadBalancerArn); arn != "" {
			arns = append(arns, arn)
		}
	}

	tagsByARN := p.getELBv2Tags(ctx, client, arns)

	resources := make([]types.Resource, 0, len(loadBalancers))
	for _, lb := range loadBalancers {
		arn := aws.ToString(lb.LoadBalancerArn)
		name := aws.ToString(lb.LoadBalancerName)

		tags, ok := tagsByARN[arn]
		if !ok {
			tags = map[string]string{}
		}

		resource := types.Resource{
			ID:       name,
			ARN:      arn,
			Type:     "aws_lb",
			Name:     name,
			Region:   region,
			Account:  p.accountID,
			Provider: "aws",
			Tags:     tags,
		}

		if lb.CreatedTime != nil {
			resource.CreatedAt = lb.CreatedTime
		}

		log.Debug("AWS ELBv2: Load balancer %s in %s has %d tags", name, region, len(resource.Tags))
		resources = append(resources, resource)
	}

	log.Debug("AWS ELBv2: Found %d load balancers in %s", len(resources), region)
	return resources, nil
}

// getELBv2Tags fetches tags for load balancer ARNs in batches.
// A batch that cannot be read leaves its load balancers untagged rather than
// failing the whole region scan.
func (p *Provider) getELBv2Tags(ctx context.Context, client elbv2API, arns []string) map[string]map[string]string {
	tagsByARN := make(map[string]map[string]string, len(arns))

	for start := 0; start < len(arns); start += elbTagBatchSize {
		end := start + elbTagBatchSize
		if end > len(arns) {
			end = len(arns)
		}

		output, err := client.DescribeTags(ctx, &elbv2.DescribeTagsInput{
			ResourceArns: arns[start:end],
		})
		if err != nil {
			log.Debug("AWS ELBv2: Failed to get tags for batch starting at %d: %v", start, err)
			continue
		}

		for _, desc := range output.TagDescriptions {
			arn := aws.ToString(desc.ResourceArn)
			if arn == "" {
				continue
			}
			tagsByARN[arn] = elbv2TagsToMap(desc.Tags)
		}
	}

	return tagsByARN
}

// applyELBv2Tags applies tags to a load balancer.
func (p *Provider) applyELBv2Tags(ctx context.Context, arn string, tags map[string]string) error {
	region := extractRegionFromARN(arn)
	if region == "" && len(p.regions) > 0 {
		region = p.regions[0]
	}

	tagList := make([]elbv2types.Tag, 0, len(tags))
	for k, v := range tags {
		tagList = append(tagList, elbv2types.Tag{Key: aws.String(k), Value: aws.String(v)})
	}

	client := p.getELBv2Client(region)
	_, err := client.AddTags(ctx, &elbv2.AddTagsInput{
		ResourceArns: []string{arn},
		Tags:         tagList,
	})
	if err != nil {
		return provider.NewProviderError("aws", "apply_elbv2_tags", arn, err)
	}

	log.Debug("AWS ELBv2: Applied %d tags to %s", len(tags), arn)
	return nil
}

// elbv2TagsToMap converts ELBv2 tags to a map.
func elbv2TagsToMap(tags []elbv2types.Tag) map[string]string {
	result := make(map[string]string)
	for _, tag := range tags {
		if tag.Key != nil && tag.Value != nil {
			result[*tag.Key] = *tag.Value
		}
	}
	return result
}

// listTargetGroups lists ALB/NLB target groups in a region.
func (p *Provider) listTargetGroups(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listTargetGroupsFrom(ctx, p.getELBv2Client(region), region)
}

func (p *Provider) listTargetGroupsFrom(ctx context.Context, client elbv2API, region string) ([]types.Resource, error) {
	var groups []elbv2types.TargetGroup
	paginator := elbv2.NewDescribeTargetGroupsPaginator(client, &elbv2.DescribeTargetGroupsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError("aws", "list_target_groups", "", err)
		}
		groups = append(groups, output.TargetGroups...)
	}
	if len(groups) == 0 {
		return nil, nil
	}

	arns := make([]string, 0, len(groups))
	for _, g := range groups {
		arns = append(arns, aws.ToString(g.TargetGroupArn))
	}
	useBulk := p.tagsFor(region).available()
	var tagsByARN map[string]map[string]string
	if !useBulk {
		tagsByARN = p.getELBv2Tags(ctx, client, arns)
	}

	resources := make([]types.Resource, 0, len(groups))
	for _, g := range groups {
		arn := aws.ToString(g.TargetGroupArn)
		tags := tagsByARN[arn]
		if useBulk {
			tags = p.bulkTags(region, arn)
		}
		if tags == nil {
			tags = map[string]string{}
		}
		name := aws.ToString(g.TargetGroupName)
		resources = append(resources, types.Resource{
			ID: name, Name: name, ARN: arn, Type: "aws_lb_target_group",
			Region: region, Account: p.accountID, Provider: "aws", Tags: tags,
		})
	}
	log.Debug("AWS ELBv2: Found %d target groups in %s", len(resources), region)
	return resources, nil
}

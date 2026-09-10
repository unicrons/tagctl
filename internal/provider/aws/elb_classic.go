package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	elb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// classicELBAPI is the subset of the classic ELB API used for discovery and tagging.
type classicELBAPI interface {
	DescribeLoadBalancers(ctx context.Context, params *elb.DescribeLoadBalancersInput, optFns ...func(*elb.Options)) (*elb.DescribeLoadBalancersOutput, error)
	DescribeTags(ctx context.Context, params *elb.DescribeTagsInput, optFns ...func(*elb.Options)) (*elb.DescribeTagsOutput, error)
	AddTags(ctx context.Context, params *elb.AddTagsInput, optFns ...func(*elb.Options)) (*elb.AddTagsOutput, error)
}

// listClassicLoadBalancers lists classic (v1) load balancers in a region.
func (p *Provider) listClassicLoadBalancers(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listClassicLoadBalancersFrom(ctx, p.getClassicELBClient(region), region)
}

func (p *Provider) listClassicLoadBalancersFrom(ctx context.Context, client classicELBAPI, region string) ([]types.Resource, error) {
	var lbs []elbtypes.LoadBalancerDescription
	paginator := elb.NewDescribeLoadBalancersPaginator(client, &elb.DescribeLoadBalancersInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError("aws", "list_classic_load_balancers", "", err)
		}
		lbs = append(lbs, output.LoadBalancerDescriptions...)
	}

	names := make([]string, 0, len(lbs))
	for _, lb := range lbs {
		names = append(names, aws.ToString(lb.LoadBalancerName))
	}

	tagsByName := make(map[string]map[string]string, len(names))
	useBulk := p.tagsFor(region).available()
	if !useBulk {
		for _, batch := range chunk(names, elbTagBatchSize) {
			output, err := client.DescribeTags(ctx, &elb.DescribeTagsInput{LoadBalancerNames: batch})
			if err != nil {
				return nil, provider.NewProviderError("aws", "describe_classic_elb_tags", "", err)
			}
			for _, desc := range output.TagDescriptions {
				m := make(map[string]string, len(desc.Tags))
				for _, tag := range desc.Tags {
					if tag.Key != nil && tag.Value != nil {
						m[*tag.Key] = *tag.Value
					}
				}
				tagsByName[aws.ToString(desc.LoadBalancerName)] = m
			}
		}
	}

	resources := make([]types.Resource, 0, len(lbs))
	for _, lb := range lbs {
		name := aws.ToString(lb.LoadBalancerName)
		arn := fmt.Sprintf("arn:aws:elasticloadbalancing:%s:%s:loadbalancer/%s", region, p.accountID, name)
		tags := tagsByName[name]
		if useBulk {
			tags = p.bulkTags(region, arn)
		}
		if tags == nil {
			tags = map[string]string{}
		}
		resources = append(resources, types.Resource{
			ID:        name,
			Name:      name,
			ARN:       arn,
			Type:      "aws_elb",
			Region:    region,
			Account:   p.accountID,
			Provider:  "aws",
			Tags:      tags,
			CreatedAt: lb.CreatedTime,
		})
	}
	log.Debug("AWS ELB: Found %d classic load balancers in %s", len(resources), region)
	return resources, nil
}

// applyClassicELBTags applies tags to a classic load balancer addressed by ARN.
func (p *Provider) applyClassicELBTags(ctx context.Context, arn string, tags map[string]string) error {
	tagList := make([]elbtypes.Tag, 0, len(tags))
	for k, v := range tags {
		tagList = append(tagList, elbtypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}

	client := p.getClassicELBClient(extractRegionFromARN(arn))
	_, err := client.AddTags(ctx, &elb.AddTagsInput{
		LoadBalancerNames: []string{nameFromARN(arn)},
		Tags:              tagList,
	})
	if err != nil {
		return provider.NewProviderError("aws", "apply_classic_elb_tags", arn, err)
	}

	log.Debug("AWS ELB: Applied %d tags to %s", len(tags), arn)
	return nil
}

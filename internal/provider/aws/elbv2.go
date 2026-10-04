package aws

import (
	"cmp"
	"context"
	"errors"
	"maps"

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
	return p.listLoadBalancersFrom(ctx, regionalClient(p, region, elbv2.NewFromConfig), region)
}

// listLoadBalancersFrom lists load balancers using the given client.
func (p *Provider) listLoadBalancersFrom(ctx context.Context, client elbv2API, region string) ([]types.Resource, error) {
	log.Debug("AWS ELBv2: Listing load balancers in region %s...", region)

	var loadBalancers []elbv2types.LoadBalancer
	paginator := elbv2.NewDescribeLoadBalancersPaginator(client, &elbv2.DescribeLoadBalancersInput{})

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_load_balancers", "", err)
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

	tagsByARN, tagErrs := getELBv2Tags(ctx, client, arns)

	resources := make([]types.Resource, 0, len(loadBalancers))
	for _, lb := range loadBalancers {
		arn := aws.ToString(lb.LoadBalancerArn)
		name := aws.ToString(lb.LoadBalancerName)
		tags, ok := tagsByARN[arn]
		if !ok {
			p.skipResource(ctx, "ELBv2", region, "load balancer "+name, cmp.Or(tagErrs[arn], errNotDescribed))
			continue
		}
		resources = append(resources, p.resource(region, "aws_lb", name, name, arn, tags, lb.CreatedTime))
	}

	log.Debug("AWS ELBv2: Found %d load balancers in %s", len(resources), region)
	return resources, nil
}

// errNotDescribed is the tag read error of an ARN a successful DescribeTags
// response left out.
var errNotDescribed = errors.New("missing from DescribeTags response")

// getELBv2Tags reads tags for ARNs in DescribeTags batches. An ARN that could
// not be read is absent from tags; errs holds the failure of its call.
func getELBv2Tags(ctx context.Context, client elbv2API, arns []string) (tags map[string]map[string]string, errs map[string]error) {
	tags = make(map[string]map[string]string, len(arns))
	errs = make(map[string]error)
	for _, batch := range chunk(arns, elbTagBatchSize) {
		output, err := client.DescribeTags(ctx, &elbv2.DescribeTagsInput{ResourceArns: batch})
		switch {
		case err == nil:
			for _, desc := range output.TagDescriptions {
				tags[aws.ToString(desc.ResourceArn)] = tagsToMap(desc.Tags,
					func(t elbv2types.Tag) *string { return t.Key },
					func(t elbv2types.Tag) *string { return t.Value })
			}
		case resourceGone(err) && len(batch) > 1:
			// One deleted ARN fails its whole batch: read the batch one ARN at a time.
			for _, arn := range batch {
				batchTags, batchErrs := getELBv2Tags(ctx, client, []string{arn})
				maps.Copy(tags, batchTags)
				maps.Copy(errs, batchErrs)
			}
		default:
			for _, arn := range batch {
				errs[arn] = err
			}
		}
	}
	return tags, errs
}

// applyELBv2Tags applies tags to a load balancer.
func (p *Provider) applyELBv2Tags(ctx context.Context, arn string, tags map[string]string) error {
	region, err := regionForARN(arn)
	if err != nil {
		return provider.NewProviderError(providerName, "apply_elbv2_tags", arn, err)
	}

	tagList := make([]elbv2types.Tag, 0, len(tags))
	for k, v := range tags {
		tagList = append(tagList, elbv2types.Tag{Key: aws.String(k), Value: aws.String(v)})
	}

	client := regionalClient(p, region, elbv2.NewFromConfig)
	_, err = client.AddTags(ctx, &elbv2.AddTagsInput{
		ResourceArns: []string{arn},
		Tags:         tagList,
	})
	if err != nil {
		return provider.NewProviderError(providerName, "apply_elbv2_tags", arn, err)
	}

	log.Debug("AWS ELBv2: Applied %d tags to %s", len(tags), arn)
	return nil
}

// listTargetGroups lists ALB/NLB target groups in a region.
func (p *Provider) listTargetGroups(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listTargetGroupsFrom(ctx, regionalClient(p, region, elbv2.NewFromConfig), region)
}

func (p *Provider) listTargetGroupsFrom(ctx context.Context, client elbv2API, region string) ([]types.Resource, error) {
	var groups []elbv2types.TargetGroup
	paginator := elbv2.NewDescribeTargetGroupsPaginator(client, &elbv2.DescribeTargetGroupsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_target_groups", "", err)
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
	useBulk := p.tagsFor(region).available(ctx)
	var tagsByARN map[string]map[string]string
	var tagErrs map[string]error
	if !useBulk {
		tagsByARN, tagErrs = getELBv2Tags(ctx, client, arns)
	}

	resources := make([]types.Resource, 0, len(groups))
	for _, g := range groups {
		arn := aws.ToString(g.TargetGroupArn)
		name := aws.ToString(g.TargetGroupName)
		tags, ok := tagsByARN[arn]
		if useBulk {
			tags, ok = p.bulkTags(ctx, region, arn), true
		}
		if !ok {
			p.skipResource(ctx, "ELBv2", region, "target group "+name, cmp.Or(tagErrs[arn], errNotDescribed))
			continue
		}
		resources = append(resources, p.resource(region, "aws_lb_target_group", name, name, arn, tags, nil))
	}
	log.Debug("AWS ELBv2: Found %d target groups in %s", len(resources), region)
	return resources, nil
}

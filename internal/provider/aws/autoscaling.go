package aws

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	asgtypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// autoscalingAPI is the subset of the Auto Scaling API used for discovery.
type autoscalingAPI interface {
	DescribeAutoScalingGroups(ctx context.Context, params *autoscaling.DescribeAutoScalingGroupsInput, optFns ...func(*autoscaling.Options)) (*autoscaling.DescribeAutoScalingGroupsOutput, error)
}

// listAutoScalingGroups lists all Auto Scaling groups in a region.
func (p *Provider) listAutoScalingGroups(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listAutoScalingGroupsFrom(ctx, regionalClient(p, region, autoscaling.NewFromConfig), region)
}

// listAutoScalingGroupsFrom lists Auto Scaling groups using the given client.
func (p *Provider) listAutoScalingGroupsFrom(ctx context.Context, client autoscalingAPI, region string) ([]types.Resource, error) {
	log.Debug("AWS AutoScaling: Listing groups in region %s...", region)
	var resources []types.Resource

	paginator := autoscaling.NewDescribeAutoScalingGroupsPaginator(client, &autoscaling.DescribeAutoScalingGroupsInput{})

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_autoscaling_groups", "", err)
		}

		for _, asg := range output.AutoScalingGroups {
			name := aws.ToString(asg.AutoScalingGroupName)
			tags := tagsToMap(asg.Tags,
				func(t asgtypes.TagDescription) *string { return t.Key },
				func(t asgtypes.TagDescription) *string { return t.Value })

			resource := types.Resource{
				ID:       name,
				ARN:      aws.ToString(asg.AutoScalingGroupARN),
				Type:     "aws_autoscaling_group",
				Name:     name,
				Region:   region,
				Account:  p.accountID,
				Provider: providerName,
				// Auto Scaling returns tags inline, so no extra call is needed.
				Tags: tags,
			}

			if asg.CreatedTime != nil {
				resource.CreatedAt = asg.CreatedTime
			}

			log.Debug("AWS AutoScaling: Group %s in %s has %d tags", name, region, len(resource.Tags))
			resources = append(resources, resource)
		}
	}

	log.Debug("AWS AutoScaling: Found %d groups in %s", len(resources), region)
	return resources, nil
}

// applyAutoScalingTags applies tags to an Auto Scaling group.
func (p *Provider) applyAutoScalingTags(ctx context.Context, arn string, tags map[string]string) error {
	region, err := regionForARN(arn)
	if err != nil {
		return provider.NewProviderError(providerName, "apply_autoscaling_tags", arn, err)
	}

	// Auto Scaling tags are keyed by group name, not by ARN.
	asgName := asgNameFromARN(arn)

	tagList := make([]asgtypes.Tag, 0, len(tags))
	for k, v := range tags {
		tagList = append(tagList, asgtypes.Tag{
			ResourceId:        aws.String(asgName),
			ResourceType:      aws.String("auto-scaling-group"),
			Key:               aws.String(k),
			Value:             aws.String(v),
			PropagateAtLaunch: aws.Bool(true),
		})
	}

	client := regionalClient(p, region, autoscaling.NewFromConfig)
	_, err = client.CreateOrUpdateTags(ctx, &autoscaling.CreateOrUpdateTagsInput{
		Tags: tagList,
	})
	if err != nil {
		return provider.NewProviderError(providerName, "apply_autoscaling_tags", arn, err)
	}

	log.Debug("AWS AutoScaling: Applied %d tags to %s", len(tags), asgName)
	return nil
}

// asgNameFromARN extracts the Auto Scaling group name from its ARN, which ends
// in autoScalingGroupName/<name>. A value that is already a bare name is
// returned unchanged.
func asgNameFromARN(arn string) string {
	if idx := strings.LastIndex(arn, "/"); idx != -1 {
		return arn[idx+1:]
	}
	if idx := strings.LastIndex(arn, ":"); idx != -1 {
		return arn[idx+1:]
	}
	return arn
}

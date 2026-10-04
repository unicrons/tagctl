package aws

import (
	"context"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// logsAPI is the subset of the CloudWatch Logs API used for discovery and tagging.
type logsAPI interface {
	DescribeLogGroups(ctx context.Context, params *cloudwatchlogs.DescribeLogGroupsInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.DescribeLogGroupsOutput, error)
	ListTagsForResource(ctx context.Context, params *cloudwatchlogs.ListTagsForResourceInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.ListTagsForResourceOutput, error)
	TagResource(ctx context.Context, params *cloudwatchlogs.TagResourceInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.TagResourceOutput, error)
}

// logGroup is the subset of a log group needed to build a resource.
type logGroup struct {
	name    string
	arn     string
	created *time.Time
}

// listLogGroups lists all CloudWatch log groups in a region.
func (p *Provider) listLogGroups(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listLogGroupsFrom(ctx, regionalClient(p, region, cloudwatchlogs.NewFromConfig), region)
}

// listLogGroupsFrom lists CloudWatch log groups using the given client.
func (p *Provider) listLogGroupsFrom(ctx context.Context, client logsAPI, region string) ([]types.Resource, error) {
	log.Debug("AWS Logs: Listing log groups in region %s...", region)

	var groups []logGroup
	paginator := cloudwatchlogs.NewDescribeLogGroupsPaginator(client, &cloudwatchlogs.DescribeLogGroupsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_log_groups", "", err)
		}
		for _, g := range output.LogGroups {
			groups = append(groups, logGroup{
				name:    aws.ToString(g.LogGroupName),
				arn:     logGroupARN(g.LogGroupArn, g.Arn),
				created: epochMillis(g.CreationTime),
			})
		}
	}

	resources := forEachConcurrently(ctx, groups, func(g logGroup) []types.Resource {
		tags, err := p.resourceTags(ctx, region, g.arn, func() (map[string]string, error) {
			output, err := client.ListTagsForResource(ctx, &cloudwatchlogs.ListTagsForResourceInput{ResourceArn: aws.String(g.arn)})
			if err != nil {
				return nil, err
			}
			if output.Tags == nil {
				return map[string]string{}, nil
			}
			return output.Tags, nil
		})
		if err != nil {
			p.skipResource(ctx, "Logs", region, "log group "+g.name, err)
			return nil
		}
		return one(types.Resource{
			ID:        g.name,
			Name:      g.name,
			ARN:       g.arn,
			Type:      "aws_cloudwatch_log_group",
			Region:    region,
			Account:   p.accountID,
			Provider:  providerName,
			Tags:      tags,
			CreatedAt: g.created,
		})
	})

	log.Debug("AWS Logs: Found %d log groups in %s", len(resources), region)
	return resources, nil
}

// logGroupARN prefers the plain ARN; the legacy Arn field ends in ":*", which
// the tagging APIs reject.
func logGroupARN(plain, legacy *string) string {
	if arn := aws.ToString(plain); arn != "" {
		return arn
	}
	return strings.TrimSuffix(aws.ToString(legacy), ":*")
}

// epochMillis converts a CloudWatch millisecond timestamp to time.
func epochMillis(ms *int64) *time.Time {
	if ms == nil {
		return nil
	}
	t := time.UnixMilli(*ms).UTC()
	return &t
}

// applyLogGroupTags applies tags to a log group addressed by ARN.
func (p *Provider) applyLogGroupTags(ctx context.Context, arn string, tags map[string]string) error {
	region, err := regionForARN(arn)
	if err != nil {
		return provider.NewProviderError(providerName, "apply_log_group_tags", arn, err)
	}
	client := regionalClient(p, region, cloudwatchlogs.NewFromConfig)
	_, err = client.TagResource(ctx, &cloudwatchlogs.TagResourceInput{
		ResourceArn: aws.String(arn),
		Tags:        tags,
	})
	if err != nil {
		return provider.NewProviderError(providerName, "apply_log_group_tags", arn, err)
	}

	log.Debug("AWS Logs: Applied %d tags to %s", len(tags), arn)
	return nil
}

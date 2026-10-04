package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// snsAPI is the subset of the SNS API used for discovery and tagging.
type snsAPI interface {
	ListTopics(ctx context.Context, params *sns.ListTopicsInput, optFns ...func(*sns.Options)) (*sns.ListTopicsOutput, error)
	ListTagsForResource(ctx context.Context, params *sns.ListTagsForResourceInput, optFns ...func(*sns.Options)) (*sns.ListTagsForResourceOutput, error)
}

// listSNSTopics lists all SNS topics in a region.
func (p *Provider) listSNSTopics(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listSNSTopicsFrom(ctx, p.getSNSClient(region), region)
}

// listSNSTopicsFrom lists SNS topics using the given client.
func (p *Provider) listSNSTopicsFrom(ctx context.Context, client snsAPI, region string) ([]types.Resource, error) {
	log.Debug("AWS SNS: Listing topics in region %s...", region)

	// First, collect all topic ARNs from pagination
	var arns []string
	paginator := sns.NewListTopicsPaginator(client, &sns.ListTopicsInput{})

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_sns_topics", "", err)
		}
		for _, topic := range output.Topics {
			if arn := aws.ToString(topic.TopicArn); arn != "" {
				arns = append(arns, arn)
			}
		}
	}

	resources := forEachConcurrently(ctx, arns, func(arn string) []types.Resource {
		name := nameFromARN(arn)
		tags, err := p.resourceTags(ctx, region, arn, func() (map[string]string, error) {
			return getSNSTags(ctx, client, arn)
		})
		if err != nil {
			p.skipResource(ctx, "SNS", region, "topic "+name, err)
			return nil
		}
		return one(p.resource(region, "aws_sns_topic", name, name, arn, tags, nil))
	})

	log.Debug("AWS SNS: Found %d topics in %s", len(resources), region)
	return resources, nil
}

// getSNSTags reads the tags of an SNS topic.
func getSNSTags(ctx context.Context, client snsAPI, arn string) (map[string]string, error) {
	output, err := client.ListTagsForResource(ctx, &sns.ListTagsForResourceInput{
		ResourceArn: aws.String(arn),
	})
	if err != nil {
		return nil, err
	}
	return snsTagsToMap(output.Tags), nil
}

// applySNSTags applies tags to an SNS topic.
func (p *Provider) applySNSTags(ctx context.Context, arn string, tags map[string]string) error {
	region, err := regionForARN(arn)
	if err != nil {
		return provider.NewProviderError(providerName, "apply_sns_tags", arn, err)
	}

	tagList := make([]snstypes.Tag, 0, len(tags))
	for k, v := range tags {
		tagList = append(tagList, snstypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}

	client := p.getSNSClient(region)
	_, err = client.TagResource(ctx, &sns.TagResourceInput{
		ResourceArn: aws.String(arn),
		Tags:        tagList,
	})
	if err != nil {
		return provider.NewProviderError(providerName, "apply_sns_tags", arn, err)
	}

	log.Debug("AWS SNS: Applied %d tags to %s", len(tags), arn)
	return nil
}

// snsTagsToMap converts SNS tags to a map.
func snsTagsToMap(tags []snstypes.Tag) map[string]string {
	result := make(map[string]string)
	for _, tag := range tags {
		if tag.Key != nil && tag.Value != nil {
			result[*tag.Key] = *tag.Value
		}
	}
	return result
}

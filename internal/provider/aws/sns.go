package aws

import (
	"context"
	"strings"
	"sync"

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

	if len(arns) == 0 {
		log.Debug("AWS SNS: Found 0 topics in %s", region)
		return nil, nil
	}

	// Fetch tags in parallel using semaphore
	sem := make(chan struct{}, maxConcurrentAPICalls)
	results := make(chan types.Resource, len(arns))
	var wg sync.WaitGroup

	for _, arn := range arns {
		arn := arn // capture loop variable
		wg.Add(1)

		go func() {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// The topic name is the last segment of the ARN
			name := arn
			if idx := strings.LastIndex(arn, ":"); idx != -1 {
				name = arn[idx+1:]
			}

			tags, _ := p.resourceTags(region, arn, func() (map[string]string, error) {
				return p.getSNSTags(ctx, client, arn), nil
			})

			log.Debug("AWS SNS: Topic %s in %s has %d tags", name, region, len(tags))
			results <- types.Resource{
				ID:       name,
				Name:     name,
				ARN:      arn,
				Type:     "aws_sns_topic",
				Region:   region,
				Account:  p.accountID,
				Provider: providerName,
				Tags:     tags,
			}
		}()
	}

	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	resources := make([]types.Resource, 0, cap(results))
	for resource := range results {
		resources = append(resources, resource)
	}

	log.Debug("AWS SNS: Found %d topics in %s", len(resources), region)
	return resources, nil
}

// getSNSTags fetches the tags for a single SNS topic.
// A topic whose tags cannot be read is reported as untagged rather than
// failing the whole region scan.
func (p *Provider) getSNSTags(ctx context.Context, client snsAPI, arn string) map[string]string {
	output, err := client.ListTagsForResource(ctx, &sns.ListTagsForResourceInput{
		ResourceArn: aws.String(arn),
	})
	if err != nil {
		log.Debug("AWS SNS: Failed to get tags for %s: %v", arn, err)
		return map[string]string{}
	}
	return snsTagsToMap(output.Tags)
}

// applySNSTags applies tags to an SNS topic.
func (p *Provider) applySNSTags(ctx context.Context, arn string, tags map[string]string) error {
	region := extractRegionFromARN(arn)
	if region == "" && len(p.regions) > 0 {
		region = p.regions[0]
	}

	tagList := make([]snstypes.Tag, 0, len(tags))
	for k, v := range tags {
		tagList = append(tagList, snstypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}

	client := p.getSNSClient(region)
	_, err := client.TagResource(ctx, &sns.TagResourceInput{
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

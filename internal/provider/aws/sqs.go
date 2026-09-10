package aws

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// sqsAPI is the subset of the SQS API used for discovery and tagging.
type sqsAPI interface {
	ListQueues(ctx context.Context, params *sqs.ListQueuesInput, optFns ...func(*sqs.Options)) (*sqs.ListQueuesOutput, error)
	GetQueueAttributes(ctx context.Context, params *sqs.GetQueueAttributesInput, optFns ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error)
	ListQueueTags(ctx context.Context, params *sqs.ListQueueTagsInput, optFns ...func(*sqs.Options)) (*sqs.ListQueueTagsOutput, error)
}

// listSQSQueues lists all SQS queues in a region.
func (p *Provider) listSQSQueues(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listSQSQueuesFrom(ctx, p.getSQSClient(region), region)
}

// listSQSQueuesFrom lists SQS queues using the given client.
func (p *Provider) listSQSQueuesFrom(ctx context.Context, client sqsAPI, region string) ([]types.Resource, error) {
	log.Debug("AWS SQS: Listing queues in region %s...", region)

	// First, collect all queue URLs from pagination
	var queueURLs []string
	paginator := sqs.NewListQueuesPaginator(client, &sqs.ListQueuesInput{})

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_sqs_queues", "", err)
		}
		for _, queueURL := range output.QueueUrls {
			if queueURL != "" {
				queueURLs = append(queueURLs, queueURL)
			}
		}
	}

	if len(queueURLs) == 0 {
		log.Debug("AWS SQS: Found 0 queues in %s", region)
		return nil, nil
	}

	// Fetch ARNs and tags in parallel using semaphore
	sem := make(chan struct{}, maxConcurrentAPICalls)
	results := make(chan types.Resource, len(queueURLs))
	var wg sync.WaitGroup

	for _, queueURL := range queueURLs {
		queueURL := queueURL // capture loop variable
		wg.Add(1)

		go func() {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// A queue whose ARN cannot be resolved is skipped: it cannot be tagged
			arn := p.getSQSQueueARN(ctx, client, queueURL)
			if arn == "" {
				log.Debug("AWS SQS: Skipping queue %s, could not resolve ARN", queueURL)
				return
			}

			// The queue name is the last segment of the queue URL
			name := queueURL
			if idx := strings.LastIndex(queueURL, "/"); idx != -1 {
				name = queueURL[idx+1:]
			}

			tags, _ := p.resourceTags(region, arn, func() (map[string]string, error) {
				return p.getSQSTags(ctx, client, queueURL), nil
			})

			log.Debug("AWS SQS: Queue %s in %s has %d tags", name, region, len(tags))
			results <- types.Resource{
				ID:       name,
				Name:     name,
				ARN:      arn,
				Type:     "aws_sqs_queue",
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

	log.Debug("AWS SQS: Found %d queues in %s", len(resources), region)
	return resources, nil
}

// getSQSQueueARN resolves a queue URL to its ARN, returning "" when unavailable.
func (p *Provider) getSQSQueueARN(ctx context.Context, client sqsAPI, queueURL string) string {
	output, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(queueURL),
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
	})
	if err != nil {
		log.Debug("AWS SQS: Failed to get attributes for %s: %v", queueURL, err)
		return ""
	}
	return output.Attributes[string(sqstypes.QueueAttributeNameQueueArn)]
}

// getSQSTags fetches the tags for a single SQS queue.
// A queue whose tags cannot be read is reported as untagged rather than
// failing the whole region scan.
func (p *Provider) getSQSTags(ctx context.Context, client sqsAPI, queueURL string) map[string]string {
	output, err := client.ListQueueTags(ctx, &sqs.ListQueueTagsInput{
		QueueUrl: aws.String(queueURL),
	})
	if err != nil {
		log.Debug("AWS SQS: Failed to get tags for %s: %v", queueURL, err)
		return map[string]string{}
	}
	if output.Tags == nil {
		return map[string]string{}
	}
	return output.Tags
}

// applySQSTags applies tags to an SQS queue.
func (p *Provider) applySQSTags(ctx context.Context, arn string, tags map[string]string) error {
	// SQS tagging needs the queue URL, not the ARN.
	region, queueURL, err := sqsQueueURLFromARN(arn)
	if err != nil {
		return provider.NewProviderError(providerName, "apply_sqs_tags", arn, err)
	}

	client := p.getSQSClient(region)
	if _, err := client.TagQueue(ctx, &sqs.TagQueueInput{
		QueueUrl: aws.String(queueURL),
		Tags:     tags,
	}); err != nil {
		return provider.NewProviderError(providerName, "apply_sqs_tags", arn, err)
	}

	log.Debug("AWS SQS: Applied %d tags to %s", len(tags), arn)
	return nil
}

// sqsQueueURLFromARN rebuilds a queue URL from an SQS ARN, which has the shape
// arn:aws:sqs:<region>:<account>:<queue-name>. It returns the region alongside
// the URL so callers can pick the right regional client.
func sqsQueueURLFromARN(arn string) (region, queueURL string, err error) {
	parts := strings.Split(arn, ":")
	if len(parts) < 6 || parts[3] == "" || parts[4] == "" || parts[5] == "" {
		return "", "", fmt.Errorf("malformed SQS ARN: %s", arn)
	}
	region = parts[3]
	queueURL = fmt.Sprintf("https://sqs.%s.amazonaws.com/%s/%s", region, parts[4], parts[5])
	return region, queueURL, nil
}

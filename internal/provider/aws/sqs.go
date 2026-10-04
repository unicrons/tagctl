package aws

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
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
	return p.listSQSQueuesFrom(ctx, regionalClient(p, region, sqs.NewFromConfig), region)
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

	resources := forEachConcurrently(ctx, queueURLs, func(queueURL string) []types.Resource {
		name := nameFromARN(queueURL)
		arn, err := getSQSQueueARN(ctx, client, queueURL)
		if err != nil {
			p.skipResource(ctx, "SQS", region, "queue "+name, err)
			return nil
		}
		tags, err := p.resourceTags(ctx, region, arn, func() (map[string]string, error) {
			return getSQSTags(ctx, client, queueURL)
		})
		if err != nil {
			p.skipResource(ctx, "SQS", region, "queue "+name, err)
			return nil
		}
		return one(p.resource(region, "aws_sqs_queue", name, name, arn, tags, nil))
	})

	log.Debug("AWS SQS: Found %d queues in %s", len(resources), region)
	return resources, nil
}

// getSQSQueueARN resolves a queue URL to its ARN. Without it the queue cannot
// be looked up in the bulk source nor tagged.
func getSQSQueueARN(ctx context.Context, client sqsAPI, queueURL string) (string, error) {
	output, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(queueURL),
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
	})
	if err != nil {
		return "", fmt.Errorf("get queue ARN: %w", err)
	}
	arn := output.Attributes[string(sqstypes.QueueAttributeNameQueueArn)]
	if arn == "" {
		return "", errors.New("get queue ARN: QueueArn attribute missing")
	}
	return arn, nil
}

// getSQSTags reads the tags of an SQS queue.
func getSQSTags(ctx context.Context, client sqsAPI, queueURL string) (map[string]string, error) {
	output, err := client.ListQueueTags(ctx, &sqs.ListQueueTagsInput{
		QueueUrl: aws.String(queueURL),
	})
	if err != nil {
		return nil, err
	}
	return output.Tags, nil
}

// applySQSTags applies tags to an SQS queue.
func (p *Provider) applySQSTags(ctx context.Context, arn string, tags map[string]string) error {
	// SQS tagging needs the queue URL, not the ARN.
	region, queueURL, err := sqsQueueURLFromARN(arn)
	if err != nil {
		return provider.NewProviderError(providerName, "apply_sqs_tags", arn, err)
	}

	client := regionalClient(p, region, sqs.NewFromConfig)
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
// arn:<partition>:sqs:<region>:<account>:<queue-name>. It returns the region
// alongside the URL so callers can pick the right regional client.
func sqsQueueURLFromARN(queueARN string) (region, queueURL string, err error) {
	parsed, err := arn.Parse(queueARN)
	if err != nil || parsed.Region == "" || parsed.AccountID == "" || parsed.Resource == "" {
		return "", "", fmt.Errorf("malformed SQS ARN: %s", queueARN)
	}
	queueURL = fmt.Sprintf("https://sqs.%s.%s/%s/%s", parsed.Region, dnsSuffix(parsed.Partition), parsed.AccountID, parsed.Resource)
	return parsed.Region, queueURL, nil
}

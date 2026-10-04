package aws

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// kinesisAPI is the subset of the Kinesis API used for discovery and tagging.
type kinesisAPI interface {
	ListStreams(ctx context.Context, params *kinesis.ListStreamsInput, optFns ...func(*kinesis.Options)) (*kinesis.ListStreamsOutput, error)
	ListTagsForStream(ctx context.Context, params *kinesis.ListTagsForStreamInput, optFns ...func(*kinesis.Options)) (*kinesis.ListTagsForStreamOutput, error)
	AddTagsToStream(ctx context.Context, params *kinesis.AddTagsToStreamInput, optFns ...func(*kinesis.Options)) (*kinesis.AddTagsToStreamOutput, error)
}

// kinesisStream is the subset of a stream summary needed to build a resource.
type kinesisStream struct {
	name    string
	arn     string
	created *time.Time
}

// listKinesisStreams lists all Kinesis data streams in a region.
func (p *Provider) listKinesisStreams(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listKinesisStreamsFrom(ctx, regionalClient(p, region, kinesis.NewFromConfig), region)
}

// listKinesisStreamsFrom lists Kinesis streams using the given client.
func (p *Provider) listKinesisStreamsFrom(ctx context.Context, client kinesisAPI, region string) ([]types.Resource, error) {
	log.Debug("AWS Kinesis: Listing streams in region %s...", region)

	var streams []kinesisStream
	paginator := kinesis.NewListStreamsPaginator(client, &kinesis.ListStreamsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_kinesis_streams", "", err)
		}
		for _, s := range output.StreamSummaries {
			streams = append(streams, kinesisStream{
				name:    aws.ToString(s.StreamName),
				arn:     aws.ToString(s.StreamARN),
				created: s.StreamCreationTimestamp,
			})
		}
	}

	resources := forEachConcurrently(ctx, streams, func(s kinesisStream) []types.Resource {
		tags, err := p.resourceTags(ctx, region, s.arn, func() (map[string]string, error) {
			return getKinesisTags(ctx, client, s.arn)
		})
		if err != nil {
			p.skipResource(ctx, "Kinesis", region, "stream "+s.name, err)
			return nil
		}
		return one(types.Resource{
			ID:        s.name,
			Name:      s.name,
			ARN:       s.arn,
			Type:      "aws_kinesis_stream",
			Region:    region,
			Account:   p.accountID,
			Provider:  providerName,
			Tags:      tags,
			CreatedAt: s.created,
		})
	})

	log.Debug("AWS Kinesis: Found %d streams in %s", len(resources), region)
	return resources, nil
}

// getKinesisTags reads every page of tags for a stream.
func getKinesisTags(ctx context.Context, client kinesisAPI, arn string) (map[string]string, error) {
	tags := make(map[string]string)
	var startAfter *string
	for {
		output, err := client.ListTagsForStream(ctx, &kinesis.ListTagsForStreamInput{
			StreamARN:            aws.String(arn),
			ExclusiveStartTagKey: startAfter,
		})
		if err != nil {
			return nil, err
		}
		for _, tag := range output.Tags {
			if tag.Key != nil && tag.Value != nil {
				tags[*tag.Key] = *tag.Value
			}
		}
		if !aws.ToBool(output.HasMoreTags) || len(output.Tags) == 0 {
			return tags, nil
		}
		startAfter = output.Tags[len(output.Tags)-1].Key
	}
}

// applyKinesisTags applies tags to a Kinesis stream addressed by ARN.
func (p *Provider) applyKinesisTags(ctx context.Context, arn string, tags map[string]string) error {
	region, err := regionForARN(arn)
	if err != nil {
		return provider.NewProviderError(providerName, "apply_kinesis_tags", arn, err)
	}
	client := regionalClient(p, region, kinesis.NewFromConfig)
	_, err = client.AddTagsToStream(ctx, &kinesis.AddTagsToStreamInput{
		StreamARN: aws.String(arn),
		Tags:      tags,
	})
	if err != nil {
		return provider.NewProviderError(providerName, "apply_kinesis_tags", arn, err)
	}

	log.Debug("AWS Kinesis: Applied %d tags to %s", len(tags), arn)
	return nil
}

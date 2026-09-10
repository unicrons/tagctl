package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// dynamoDBAPI is the subset of the DynamoDB API used for discovery and tagging.
type dynamoDBAPI interface {
	ListTables(ctx context.Context, params *dynamodb.ListTablesInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error)
	ListTagsOfResource(ctx context.Context, params *dynamodb.ListTagsOfResourceInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ListTagsOfResourceOutput, error)
	TagResource(ctx context.Context, params *dynamodb.TagResourceInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TagResourceOutput, error)
}

// listDynamoDBTables lists all DynamoDB tables in a region.
func (p *Provider) listDynamoDBTables(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listDynamoDBTablesFrom(ctx, p.getDynamoDBClient(region), region)
}

// listDynamoDBTablesFrom lists DynamoDB tables using the given client.
func (p *Provider) listDynamoDBTablesFrom(ctx context.Context, client dynamoDBAPI, region string) ([]types.Resource, error) {
	log.Debug("AWS DynamoDB: Listing tables in region %s...", region)

	var names []string
	paginator := dynamodb.NewListTablesPaginator(client, &dynamodb.ListTablesInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError("aws", "list_dynamodb_tables", "", err)
		}
		names = append(names, output.TableNames...)
	}

	resources := forEachConcurrently(names, func(name string) []types.Resource {
		arn := fmt.Sprintf("arn:aws:dynamodb:%s:%s:table/%s", region, p.accountID, name)

		tags, err := p.resourceTags(region, arn, func() (map[string]string, error) {
			return getDynamoDBTags(ctx, client, arn)
		})
		if err != nil {
			log.Error("AWS DynamoDB: Skipping table %s (%s): cannot read tags: %v", name, region, err)
			return nil
		}

		return one(types.Resource{
			ID:       name,
			Name:     name,
			ARN:      arn,
			Type:     "aws_dynamodb_table",
			Region:   region,
			Account:  p.accountID,
			Provider: "aws",
			Tags:     tags,
		})
	})

	log.Debug("AWS DynamoDB: Found %d tables in %s", len(resources), region)
	return resources, nil
}

// getDynamoDBTags reads every page of tags for a table.
func getDynamoDBTags(ctx context.Context, client dynamoDBAPI, arn string) (map[string]string, error) {
	tags := make(map[string]string)
	var next *string
	for {
		output, err := client.ListTagsOfResource(ctx, &dynamodb.ListTagsOfResourceInput{
			ResourceArn: aws.String(arn),
			NextToken:   next,
		})
		if err != nil {
			return nil, err
		}
		for _, tag := range output.Tags {
			if tag.Key != nil && tag.Value != nil {
				tags[*tag.Key] = *tag.Value
			}
		}
		if output.NextToken == nil {
			return tags, nil
		}
		next = output.NextToken
	}
}

// applyDynamoDBTags applies tags to a DynamoDB table addressed by ARN.
func (p *Provider) applyDynamoDBTags(ctx context.Context, arn string, tags map[string]string) error {
	tagList := make([]ddbtypes.Tag, 0, len(tags))
	for k, v := range tags {
		tagList = append(tagList, ddbtypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}

	client := p.getDynamoDBClient(extractRegionFromARN(arn))
	_, err := client.TagResource(ctx, &dynamodb.TagResourceInput{
		ResourceArn: aws.String(arn),
		Tags:        tagList,
	})
	if err != nil {
		return provider.NewProviderError("aws", "apply_dynamodb_tags", arn, err)
	}

	log.Debug("AWS DynamoDB: Applied %d tags to %s", len(tags), arn)
	return nil
}

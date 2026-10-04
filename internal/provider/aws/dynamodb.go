package aws

import (
	"context"
	"maps"

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
	return p.listDynamoDBTablesFrom(ctx, regionalClient(p, region, dynamodb.NewFromConfig), region)
}

// listDynamoDBTablesFrom lists DynamoDB tables using the given client.
func (p *Provider) listDynamoDBTablesFrom(ctx context.Context, client dynamoDBAPI, region string) ([]types.Resource, error) {
	log.Debug("AWS DynamoDB: Listing tables in region %s...", region)

	var names []string
	paginator := dynamodb.NewListTablesPaginator(client, &dynamodb.ListTablesInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_dynamodb_tables", "", err)
		}
		names = append(names, output.TableNames...)
	}

	resources := forEachConcurrently(ctx, names, func(name string) []types.Resource {
		arn := p.buildARN("dynamodb", region, p.accountID, "table/"+name)

		tags, err := p.resourceTags(ctx, region, arn, func() (map[string]string, error) {
			return getDynamoDBTags(ctx, client, arn)
		})
		if err != nil {
			p.skipResource(ctx, "DynamoDB", region, "table "+name, err)
			return nil
		}

		return one(types.Resource{
			ID:       name,
			Name:     name,
			ARN:      arn,
			Type:     "aws_dynamodb_table",
			Region:   region,
			Account:  p.accountID,
			Provider: providerName,
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
		maps.Copy(tags, tagsToMap(output.Tags,
			func(t ddbtypes.Tag) *string { return t.Key },
			func(t ddbtypes.Tag) *string { return t.Value }))
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

	region, err := regionForARN(arn)
	if err != nil {
		return provider.NewProviderError(providerName, "apply_dynamodb_tags", arn, err)
	}
	client := regionalClient(p, region, dynamodb.NewFromConfig)
	_, err = client.TagResource(ctx, &dynamodb.TagResourceInput{
		ResourceArn: aws.String(arn),
		Tags:        tagList,
	})
	if err != nil {
		return provider.NewProviderError(providerName, "apply_dynamodb_tags", arn, err)
	}

	log.Debug("AWS DynamoDB: Applied %d tags to %s", len(tags), arn)
	return nil
}

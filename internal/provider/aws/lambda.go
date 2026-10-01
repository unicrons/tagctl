package aws

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// lambdaAPI is the subset of the Lambda API used to discover functions.
type lambdaAPI interface {
	ListFunctions(ctx context.Context, params *lambda.ListFunctionsInput, optFns ...func(*lambda.Options)) (*lambda.ListFunctionsOutput, error)
	ListTags(ctx context.Context, params *lambda.ListTagsInput, optFns ...func(*lambda.Options)) (*lambda.ListTagsOutput, error)
}

// listLambdaFunctions lists all Lambda functions in a region.
func (p *Provider) listLambdaFunctions(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listLambdaFunctionsFrom(ctx, p.getLambdaClient(region), region)
}

// listLambdaFunctionsFrom lists Lambda functions using the given client.
func (p *Provider) listLambdaFunctionsFrom(ctx context.Context, client lambdaAPI, region string) ([]types.Resource, error) {
	log.Debug("AWS Lambda: Listing functions in region %s...", region)

	var functions []lambdatypes.FunctionConfiguration
	paginator := lambda.NewListFunctionsPaginator(client, &lambda.ListFunctionsInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_lambda_functions", "", err)
		}
		functions = append(functions, output.Functions...)
	}

	resources := forEachConcurrently(functions, func(fn lambdatypes.FunctionConfiguration) []types.Resource {
		name, arn := aws.ToString(fn.FunctionName), aws.ToString(fn.FunctionArn)
		tags, err := p.resourceTags(region, arn, func() (map[string]string, error) {
			return getLambdaTags(ctx, client, arn)
		})
		if err != nil {
			p.skipResource(ctx, "Lambda", region, "function "+name, err)
			return nil
		}

		var created *time.Time
		if t, err := time.Parse("2006-01-02T15:04:05.000+0000", aws.ToString(fn.LastModified)); err == nil {
			created = &t
		}
		return one(p.resource(region, "aws_lambda_function", name, name, arn, tags, created))
	})

	log.Debug("AWS Lambda: Found %d functions in %s", len(resources), region)
	return resources, nil
}

// getLambdaTags reads the tags of a Lambda function.
func getLambdaTags(ctx context.Context, client lambdaAPI, arn string) (map[string]string, error) {
	output, err := client.ListTags(ctx, &lambda.ListTagsInput{
		Resource: aws.String(arn),
	})
	if err != nil {
		return nil, err
	}
	return output.Tags, nil
}

// applyLambdaTags applies tags to a Lambda function.
func (p *Provider) applyLambdaTags(ctx context.Context, functionARN string, tags map[string]string) error {
	log.Debug("AWS Lambda: Applying tags to %s", functionARN)

	region, err := regionForARN(functionARN)
	if err != nil {
		return provider.NewProviderError(providerName, "tag_lambda_function", functionARN, err)
	}

	client := p.getLambdaClient(region)

	_, err = client.TagResource(ctx, &lambda.TagResourceInput{
		Resource: aws.String(functionARN),
		Tags:     tags,
	})

	if err != nil {
		log.Error("AWS Lambda: Failed to apply tags to %s: %v", functionARN, err)
		return provider.NewProviderError(providerName, "tag_lambda_function", functionARN, err)
	}

	log.Debug("AWS Lambda: Successfully applied %d tags to %s", len(tags), functionARN)
	return nil
}

package aws

import (
	"context"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// lambdaFunctionInfo holds function data needed for parallel tag fetching.
type lambdaFunctionInfo struct {
	functionName string
	functionARN  string
	lastModified *string
}

// listLambdaFunctions lists all Lambda functions in a region using parallel tag fetching.
func (p *Provider) listLambdaFunctions(ctx context.Context, region string) ([]types.Resource, error) {
	log.Debug("AWS Lambda: Listing functions in region %s...", region)
	client := p.getLambdaClient(region)

	// First, collect all functions from pagination
	var functions []lambdaFunctionInfo
	paginator := lambda.NewListFunctionsPaginator(client, &lambda.ListFunctionsInput{})

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError("aws", "list_lambda_functions", "", err)
		}

		for _, fn := range output.Functions {
			functions = append(functions, lambdaFunctionInfo{
				functionName: aws.ToString(fn.FunctionName),
				functionARN:  aws.ToString(fn.FunctionArn),
				lastModified: fn.LastModified,
			})
		}
	}

	if len(functions) == 0 {
		log.Debug("AWS Lambda: Found 0 functions in %s", region)
		return nil, nil
	}

	// Fetch tags in parallel using semaphore
	sem := make(chan struct{}, maxConcurrentAPICalls)
	results := make(chan types.Resource, len(functions))
	var wg sync.WaitGroup

	for _, fn := range functions {
		fn := fn // capture loop variable
		wg.Add(1)

		go func() {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Get tags for the function
			tags, _ := p.resourceTags(region, fn.functionARN, func() (map[string]string, error) {
				return p.getLambdaTags(ctx, region, fn.functionARN), nil
			})

			resource := types.Resource{
				ID:       fn.functionName,
				Name:     fn.functionName,
				ARN:      fn.functionARN,
				Type:     "aws_lambda_function",
				Region:   region,
				Account:  p.accountID,
				Provider: "aws",
				Tags:     tags,
			}

			// Parse last modified time
			if fn.lastModified != nil {
				if t, err := time.Parse("2006-01-02T15:04:05.000+0000", *fn.lastModified); err == nil {
					resource.CreatedAt = &t
				}
			}

			log.Debug("AWS Lambda: Function %s in %s has %d tags", fn.functionName, region, len(tags))
			results <- resource
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

	log.Debug("AWS Lambda: Found %d functions in %s", len(resources), region)
	return resources, nil
}

// getLambdaTags gets tags for a Lambda function.
func (p *Provider) getLambdaTags(ctx context.Context, region, functionARN string) map[string]string {
	log.Debug("AWS Lambda: Getting tags for %s (region: %s)", functionARN, region)
	client := p.getLambdaClient(region)
	tags := make(map[string]string)

	output, err := client.ListTags(ctx, &lambda.ListTagsInput{
		Resource: aws.String(functionARN),
	})
	if err != nil {
		log.Debug("AWS Lambda: Failed to get tags for %s: %v", functionARN, err)
		return tags
	}

	for k, v := range output.Tags {
		tags[k] = v
		log.Debug("AWS Lambda: %s has tag %s=%s", functionARN, k, v)
	}

	log.Debug("AWS Lambda: %s has %d tags", functionARN, len(tags))
	return tags
}

// applyLambdaTags applies tags to a Lambda function.
func (p *Provider) applyLambdaTags(ctx context.Context, functionARN string, tags map[string]string) error {
	log.Debug("AWS Lambda: Applying tags to %s", functionARN)

	// Extract region from ARN
	region := extractRegionFromARN(functionARN)
	if region == "" && len(p.regions) > 0 {
		log.Debug("AWS Lambda: Could not extract region from ARN, using default %s", p.regions[0])
		region = p.regions[0]
	} else {
		log.Debug("AWS Lambda: Extracted region %s from ARN", region)
	}

	client := p.getLambdaClient(region)

	_, err := client.TagResource(ctx, &lambda.TagResourceInput{
		Resource: aws.String(functionARN),
		Tags:     tags,
	})

	if err != nil {
		log.Error("AWS Lambda: Failed to apply tags to %s: %v", functionARN, err)
		return provider.NewProviderError("aws", "tag_lambda_function", functionARN, err)
	}

	log.Debug("AWS Lambda: Successfully applied %d tags to %s", len(tags), functionARN)
	return nil
}

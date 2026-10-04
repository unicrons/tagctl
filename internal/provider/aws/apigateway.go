package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/apigateway"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// restAPIsAPI is the subset of the API Gateway (REST) API used for discovery.
type restAPIsAPI interface {
	GetRestApis(ctx context.Context, params *apigateway.GetRestApisInput, optFns ...func(*apigateway.Options)) (*apigateway.GetRestApisOutput, error)
}

// httpAPIsAPI is the subset of the API Gateway v2 (HTTP/WebSocket) API used for discovery.
type httpAPIsAPI interface {
	GetApis(ctx context.Context, params *apigatewayv2.GetApisInput, optFns ...func(*apigatewayv2.Options)) (*apigatewayv2.GetApisOutput, error)
}

// listRestAPIs lists API Gateway REST APIs in a region. Tags come inline.
func (p *Provider) listRestAPIs(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listRestAPIsFrom(ctx, regionalClient(p, region, apigateway.NewFromConfig), region)
}

func (p *Provider) listRestAPIsFrom(ctx context.Context, client restAPIsAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	paginator := apigateway.NewGetRestApisPaginator(client, &apigateway.GetRestApisInput{})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, provider.NewProviderError(providerName, "list_rest_apis", "", err)
		}
		for _, api := range output.Items {
			id := aws.ToString(api.Id)
			tags := api.Tags
			if tags == nil {
				tags = map[string]string{}
			}
			resources = append(resources, types.Resource{
				ID:        id,
				Name:      aws.ToString(api.Name),
				ARN:       p.buildARN("apigateway", region, "", "/restapis/"+id),
				Type:      "aws_api_gateway_rest_api",
				Region:    region,
				Account:   p.accountID,
				Provider:  providerName,
				Tags:      tags,
				CreatedAt: api.CreatedDate,
			})
		}
	}
	log.Debug("AWS API Gateway: Found %d REST APIs in %s", len(resources), region)
	return resources, nil
}

// listHTTPAPIs lists API Gateway v2 HTTP and WebSocket APIs in a region.
func (p *Provider) listHTTPAPIs(ctx context.Context, region string) ([]types.Resource, error) {
	return p.listHTTPAPIsFrom(ctx, regionalClient(p, region, apigatewayv2.NewFromConfig), region)
}

func (p *Provider) listHTTPAPIsFrom(ctx context.Context, client httpAPIsAPI, region string) ([]types.Resource, error) {
	var resources []types.Resource
	err := paginate(func(token *string) (*string, error) {
		output, err := client.GetApis(ctx, &apigatewayv2.GetApisInput{NextToken: token})
		if err != nil {
			return nil, err
		}
		for _, api := range output.Items {
			id := aws.ToString(api.ApiId)
			tags := api.Tags
			if tags == nil {
				tags = map[string]string{}
			}
			resources = append(resources, types.Resource{
				ID:        id,
				Name:      aws.ToString(api.Name),
				ARN:       p.buildARN("apigateway", region, "", "/apis/"+id),
				Type:      "aws_apigatewayv2_api",
				Region:    region,
				Account:   p.accountID,
				Provider:  providerName,
				Tags:      tags,
				CreatedAt: api.CreatedDate,
			})
		}
		return output.NextToken, nil
	})
	if err != nil {
		return nil, provider.NewProviderError(providerName, "list_http_apis", "", err)
	}
	log.Debug("AWS API Gateway: Found %d HTTP/WebSocket APIs in %s", len(resources), region)
	return resources, nil
}

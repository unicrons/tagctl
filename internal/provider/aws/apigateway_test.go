package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/apigateway"
	apigwtypes "github.com/aws/aws-sdk-go-v2/service/apigateway/types"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apigwv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
)

type mockRestAPIsClient struct {
	items []apigwtypes.RestApi
	err   error
}

func (m *mockRestAPIsClient) GetRestApis(ctx context.Context, params *apigateway.GetRestApisInput, optFns ...func(*apigateway.Options)) (*apigateway.GetRestApisOutput, error) {
	return &apigateway.GetRestApisOutput{Items: m.items}, m.err
}

type mockHTTPAPIsClient struct {
	pages [][]apigwv2types.Api
	err   error
	calls int
}

func (m *mockHTTPAPIsClient) GetApis(ctx context.Context, params *apigatewayv2.GetApisInput, optFns ...func(*apigatewayv2.Options)) (*apigatewayv2.GetApisOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	page := m.pages[m.calls]
	m.calls++
	out := &apigatewayv2.GetApisOutput{Items: page}
	if m.calls < len(m.pages) {
		out.NextToken = aws.String("next")
	}
	return out, nil
}

func TestListRestAPIs(t *testing.T) {
	mock := &mockRestAPIsClient{items: []apigwtypes.RestApi{
		{Id: aws.String("abc123"), Name: aws.String("orders"), Tags: map[string]string{"environment": envProd}},
		{Id: aws.String("def456"), Name: aws.String("bare")},
	}}

	resources, err := testProvider().listRestAPIsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 2 {
		t.Fatalf("resources = %+v, err = %v", resources, err)
	}
	if resources[0].ARN != "arn:aws:apigateway:us-east-1::/restapis/abc123" || resources[0].Tags["environment"] != envProd {
		t.Errorf("first = %+v", resources[0])
	}
	if resources[1].Tags == nil || resources[1].Type != "aws_api_gateway_rest_api" {
		t.Errorf("second = %+v", resources[1])
	}
}

func TestListHTTPAPIs(t *testing.T) {
	mock := &mockHTTPAPIsClient{pages: [][]apigwv2types.Api{
		{{ApiId: aws.String("h1"), Name: aws.String("events"), Tags: map[string]string{"owner": "x"}}},
		{{ApiId: aws.String("h2"), Name: aws.String("ws")}},
	}}

	resources, err := testProvider().listHTTPAPIsFrom(context.Background(), mock, defaultRegion)
	if err != nil || len(resources) != 2 || mock.calls != 2 {
		t.Fatalf("resources = %+v, err = %v, calls = %d", resources, err, mock.calls)
	}
	if resources[0].ARN != "arn:aws:apigateway:us-east-1::/apis/h1" || resources[0].Tags["owner"] != "x" || resources[0].Type != "aws_apigatewayv2_api" {
		t.Errorf("first = %+v", resources[0])
	}
}

func TestListAPIs_Error(t *testing.T) {
	if _, err := testProvider().listRestAPIsFrom(context.Background(), &mockRestAPIsClient{err: errors.New("boom")}, defaultRegion); err == nil {
		t.Error("rest: expected error")
	}
	if _, err := testProvider().listHTTPAPIsFrom(context.Background(), &mockHTTPAPIsClient{err: errors.New("boom")}, defaultRegion); err == nil {
		t.Error("http: expected error")
	}
}

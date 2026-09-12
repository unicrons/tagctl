package aws

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

// mockLambdaClient serves ListFunctions pages and per-function tags.
type mockLambdaClient struct {
	pages   [][]lambdatypes.FunctionConfiguration
	tags    map[string]map[string]string
	listErr error
	tagErr  map[string]bool
	gone    map[string]bool
	calls   int
}

func (m *mockLambdaClient) ListFunctions(ctx context.Context, params *lambda.ListFunctionsInput, optFns ...func(*lambda.Options)) (*lambda.ListFunctionsOutput, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	page := m.pages[m.calls]
	m.calls++
	out := &lambda.ListFunctionsOutput{Functions: page}
	if m.calls < len(m.pages) {
		out.NextMarker = aws.String("next")
	}
	return out, nil
}

func (m *mockLambdaClient) ListTags(ctx context.Context, params *lambda.ListTagsInput, optFns ...func(*lambda.Options)) (*lambda.ListTagsOutput, error) {
	arn := aws.ToString(params.Resource)
	if m.tagErr[arn] {
		return nil, errors.New("access denied")
	}
	if m.gone[arn] {
		return nil, &lambdatypes.ResourceNotFoundException{Message: aws.String("Function not found")}
	}
	return &lambda.ListTagsOutput{Tags: m.tags[arn]}, nil
}

func lambdaFunction(name, lastModified string) lambdatypes.FunctionConfiguration {
	return lambdatypes.FunctionConfiguration{
		FunctionName: aws.String(name),
		FunctionArn:  aws.String("arn:aws:lambda:us-east-1:123456789012:function:" + name),
		LastModified: aws.String(lastModified),
	}
}

func TestListLambdaFunctions(t *testing.T) {
	mock := &mockLambdaClient{
		pages: [][]lambdatypes.FunctionConfiguration{
			{lambdaFunction("ingest", "2026-01-02T03:04:05.000+0000")},
			{lambdaFunction("report", "")},
		},
		tags: map[string]map[string]string{
			"arn:aws:lambda:us-east-1:123456789012:function:ingest": {"environment": envProd},
		},
	}

	resources, err := testProvider().listLambdaFunctionsFrom(context.Background(), mock, defaultRegion)
	if err != nil {
		t.Fatalf("listLambdaFunctionsFrom() error = %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("got %d functions across 2 pages, want 2", len(resources))
	}

	sort.Slice(resources, func(i, j int) bool { return resources[i].ID < resources[j].ID })
	ingest := resources[0]
	if ingest.ID != "ingest" || ingest.Type != "aws_lambda_function" || ingest.ARN != "arn:aws:lambda:us-east-1:123456789012:function:ingest" {
		t.Errorf("ingest = %+v", ingest)
	}
	if ingest.Tags["environment"] != envProd {
		t.Errorf("ingest tags = %v", ingest.Tags)
	}
	if want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC); ingest.CreatedAt == nil || !ingest.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, want %v", ingest.CreatedAt, want)
	}
	report := resources[1]
	if report.CreatedAt != nil {
		t.Errorf("CreatedAt = %v for an unparsable LastModified, want nil", report.CreatedAt)
	}
	if report.Tags == nil || len(report.Tags) != 0 {
		t.Errorf("untagged function tags = %v, want an empty map", report.Tags)
	}
}

func TestListLambdaFunctions_UnreadableTagsSkipFunctionNotReportUntagged(t *testing.T) {
	mock := &mockLambdaClient{
		pages:  [][]lambdatypes.FunctionConfiguration{{lambdaFunction("ingest", ""), lambdaFunction("denied", "")}},
		tagErr: map[string]bool{"arn:aws:lambda:us-east-1:123456789012:function:denied": true},
	}

	p := testProvider()
	resources, err := p.listLambdaFunctionsFrom(context.Background(), mock, defaultRegion)
	if err != nil {
		t.Fatalf("listLambdaFunctionsFrom() error = %v, want nil", err)
	}
	if len(resources) != 1 || resources[0].ID != "ingest" {
		t.Fatalf("resources = %+v, want only the readable function", resources)
	}
	if errors.Join(p.skipped.errs()...) == nil {
		t.Error("the skipped function was not recorded")
	}
}

func TestListLambdaFunctions_FunctionDeletedBeforeTagReadIsDroppedNotSkipped(t *testing.T) {
	mock := &mockLambdaClient{
		pages: [][]lambdatypes.FunctionConfiguration{{lambdaFunction("ingest", ""), lambdaFunction("deleted", "")}},
		gone:  map[string]bool{"arn:aws:lambda:us-east-1:123456789012:function:deleted": true},
	}

	p := testProvider()
	resources, err := p.listLambdaFunctionsFrom(context.Background(), mock, defaultRegion)
	if err != nil {
		t.Fatalf("listLambdaFunctionsFrom() error = %v, want nil", err)
	}
	if len(resources) != 1 || resources[0].ID != "ingest" {
		t.Fatalf("resources = %+v, want only the function that still exists", resources)
	}
	if err := errors.Join(p.skipped.errs()...); err != nil {
		t.Errorf("a deleted function was counted as skipped: %v", err)
	}
}

func TestListLambdaFunctions_ListError(t *testing.T) {
	mock := &mockLambdaClient{listErr: errors.New("boom")}

	if _, err := testProvider().listLambdaFunctionsFrom(context.Background(), mock, defaultRegion); err == nil {
		t.Fatal("listLambdaFunctionsFrom() error = nil, want an error")
	}
}

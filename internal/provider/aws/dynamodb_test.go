package aws

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type mockDynamoDBClient struct {
	pages      [][]string
	tags       map[string][]ddbtypes.Tag
	tagsErrFor map[string]bool
	listErr    error
	calls      int
	tagged     map[string]map[string]string
}

func (m *mockDynamoDBClient) ListTables(ctx context.Context, params *dynamodb.ListTablesInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	page := m.pages[m.calls]
	m.calls++
	out := &dynamodb.ListTablesOutput{TableNames: page}
	if m.calls < len(m.pages) {
		out.LastEvaluatedTableName = aws.String(page[len(page)-1])
	}
	return out, nil
}

func (m *mockDynamoDBClient) ListTagsOfResource(ctx context.Context, params *dynamodb.ListTagsOfResourceInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ListTagsOfResourceOutput, error) {
	arn := aws.ToString(params.ResourceArn)
	if m.tagsErrFor[arn] {
		return nil, errors.New("access denied")
	}
	// Serve the tag list in two pages to exercise NextToken handling.
	all := m.tags[arn]
	if params.NextToken == nil && len(all) > 1 {
		return &dynamodb.ListTagsOfResourceOutput{Tags: all[:1], NextToken: aws.String("more")}, nil
	}
	if params.NextToken != nil {
		return &dynamodb.ListTagsOfResourceOutput{Tags: all[1:]}, nil
	}
	return &dynamodb.ListTagsOfResourceOutput{Tags: all}, nil
}

func (m *mockDynamoDBClient) TagResource(ctx context.Context, params *dynamodb.TagResourceInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TagResourceOutput, error) {
	if m.tagged == nil {
		m.tagged = map[string]map[string]string{}
	}
	tags := map[string]string{}
	for _, t := range params.Tags {
		tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	m.tagged[aws.ToString(params.ResourceArn)] = tags
	return &dynamodb.TagResourceOutput{}, nil
}

func TestListDynamoDBTables(t *testing.T) {
	orders := "arn:aws:dynamodb:us-east-1:123456789012:table/orders"
	mock := &mockDynamoDBClient{
		pages: [][]string{{"orders"}, {"sessions"}},
		tags: map[string][]ddbtypes.Tag{
			orders: {
				{Key: aws.String("environment"), Value: aws.String(envProd)},
				{Key: aws.String("owner"), Value: aws.String("data@example.com")},
			},
		},
	}

	resources, err := testProvider().listDynamoDBTablesFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("got %d tables across 2 pages, want 2", len(resources))
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })

	r := resources[0]
	if r.ID != "orders" || r.ARN != orders || r.Type != "aws_dynamodb_table" || r.Region != defaultRegion {
		t.Errorf("unexpected resource: %+v", r)
	}
	if r.Tags["environment"] != envProd || r.Tags["owner"] != "data@example.com" {
		t.Errorf("tags = %v, want both pages merged", r.Tags)
	}
	if len(resources[1].Tags) != 0 {
		t.Errorf("untagged table has tags %v", resources[1].Tags)
	}
}

func TestListDynamoDBTables_UnreadableTableIsSkipped(t *testing.T) {
	denied := "arn:aws:dynamodb:us-east-1:123456789012:table/denied"
	mock := &mockDynamoDBClient{
		pages:      [][]string{{"denied", "ok"}},
		tagsErrFor: map[string]bool{denied: true},
	}

	resources, err := testProvider().listDynamoDBTablesFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 1 || resources[0].ID != "ok" {
		t.Errorf("got %+v, want only the readable table", resources)
	}
}

func TestListDynamoDBTables_ListError(t *testing.T) {
	mock := &mockDynamoDBClient{listErr: errors.New("boom")}
	if _, err := testProvider().listDynamoDBTablesFrom(context.Background(), mock, "us-east-1"); err == nil {
		t.Fatal("expected error")
	}
}

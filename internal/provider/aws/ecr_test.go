package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

type mockECRClient struct {
	pages      [][]ecrtypes.Repository
	tags       map[string][]ecrtypes.Tag
	tagsErrFor map[string]bool
	listErr    error
	calls      int
}

func (m *mockECRClient) DescribeRepositories(ctx context.Context, params *ecr.DescribeRepositoriesInput, optFns ...func(*ecr.Options)) (*ecr.DescribeRepositoriesOutput, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	page := m.pages[m.calls]
	m.calls++
	out := &ecr.DescribeRepositoriesOutput{Repositories: page}
	if m.calls < len(m.pages) {
		out.NextToken = aws.String("next")
	}
	return out, nil
}

func (m *mockECRClient) ListTagsForResource(ctx context.Context, params *ecr.ListTagsForResourceInput, optFns ...func(*ecr.Options)) (*ecr.ListTagsForResourceOutput, error) {
	arn := aws.ToString(params.ResourceArn)
	if m.tagsErrFor[arn] {
		return nil, errors.New("access denied")
	}
	return &ecr.ListTagsForResourceOutput{Tags: m.tags[arn]}, nil
}

func (m *mockECRClient) TagResource(ctx context.Context, params *ecr.TagResourceInput, optFns ...func(*ecr.Options)) (*ecr.TagResourceOutput, error) {
	return &ecr.TagResourceOutput{}, nil
}

func repository(name string) ecrtypes.Repository {
	return ecrtypes.Repository{
		RepositoryName: aws.String(name),
		RepositoryArn:  aws.String("arn:aws:ecr:us-east-1:123456789012:repository/" + name),
	}
}

func TestListECRRepositories(t *testing.T) {
	api := "arn:aws:ecr:us-east-1:123456789012:repository/api"
	mock := &mockECRClient{
		pages: [][]ecrtypes.Repository{{repository("api")}, {repository("worker")}},
		tags:  map[string][]ecrtypes.Tag{api: {{Key: aws.String("environment"), Value: aws.String(envProd)}}},
	}

	resources, err := testProvider().listECRRepositoriesFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("got %d repositories across 2 pages, want 2", len(resources))
	}
	for _, r := range resources {
		if r.Type != "aws_ecr_repository" || r.ARN == "" {
			t.Errorf("unexpected resource: %+v", r)
		}
		if r.ID == "api" && r.Tags["environment"] != envProd {
			t.Errorf("tags = %v", r.Tags)
		}
	}
}

func TestListECRRepositories_UnreadableRepositoryIsSkipped(t *testing.T) {
	denied := "arn:aws:ecr:us-east-1:123456789012:repository/denied"
	mock := &mockECRClient{
		pages:      [][]ecrtypes.Repository{{repository("denied"), repository("ok")}},
		tagsErrFor: map[string]bool{denied: true},
	}

	resources, err := testProvider().listECRRepositoriesFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 1 || resources[0].ID != "ok" {
		t.Errorf("got %+v, want only the readable repository", resources)
	}
}

func TestListECRRepositories_ListError(t *testing.T) {
	mock := &mockECRClient{listErr: errors.New("boom")}
	if _, err := testProvider().listECRRepositoriesFrom(context.Background(), mock, "us-east-1"); err == nil {
		t.Fatal("expected error")
	}
}

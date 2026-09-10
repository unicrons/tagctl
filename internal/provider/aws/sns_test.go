package aws

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
)

// mockSNSClient serves ListTopics pages and per-topic tags.
type mockSNSClient struct {
	pages      [][]snstypes.Topic
	tags       map[string][]snstypes.Tag
	listErr    error
	tagsErrFor map[string]bool
	calls      int
}

func (m *mockSNSClient) ListTopics(ctx context.Context, params *sns.ListTopicsInput, optFns ...func(*sns.Options)) (*sns.ListTopicsOutput, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	page := m.pages[m.calls]
	m.calls++
	out := &sns.ListTopicsOutput{Topics: page}
	if m.calls < len(m.pages) {
		out.NextToken = aws.String("next")
	}
	return out, nil
}

func (m *mockSNSClient) ListTagsForResource(ctx context.Context, params *sns.ListTagsForResourceInput, optFns ...func(*sns.Options)) (*sns.ListTagsForResourceOutput, error) {
	arn := aws.ToString(params.ResourceArn)
	if m.tagsErrFor[arn] {
		return nil, errors.New("access denied")
	}
	return &sns.ListTagsForResourceOutput{Tags: m.tags[arn]}, nil
}

func TestListSNSTopics(t *testing.T) {
	alerts := "arn:aws:sns:us-east-1:123456789012:alerts"
	billing := "arn:aws:sns:us-east-1:123456789012:billing"

	mock := &mockSNSClient{
		pages: [][]snstypes.Topic{
			{{TopicArn: aws.String(alerts)}},
			{{TopicArn: aws.String(billing)}},
		},
		tags: map[string][]snstypes.Tag{
			alerts: {{Key: aws.String("environment"), Value: aws.String(envProd)}},
		},
	}

	resources, err := testProvider().listSNSTopicsFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("listSNSTopicsFrom() error = %v", err)
	}

	if len(resources) != 2 {
		t.Fatalf("got %d resources across 2 pages, want 2", len(resources))
	}

	// Tags are fetched concurrently, so order is not guaranteed.
	sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })

	if resources[0].Name != "alerts" {
		t.Errorf("Name = %q, want %q (last ARN segment)", resources[0].Name, "alerts")
	}
	if resources[0].ID != "alerts" {
		t.Errorf("ID = %q, want %q", resources[0].ID, "alerts")
	}
	if resources[0].ARN != alerts {
		t.Errorf("ARN = %q, want %q", resources[0].ARN, alerts)
	}
	if resources[0].Type != "aws_sns_topic" {
		t.Errorf("Type = %q, want %q", resources[0].Type, "aws_sns_topic")
	}
	if resources[0].Tags["environment"] != envProd {
		t.Errorf("Tags[environment] = %q, want %q", resources[0].Tags["environment"], envProd)
	}
	if len(resources[1].Tags) != 0 {
		t.Errorf("got %d tags for untagged topic, want 0", len(resources[1].Tags))
	}
}

// A topic whose tags cannot be read is still reported, just without tags,
// so one denied topic does not fail the whole region scan.
func TestListSNSTopics_TagErrorYieldsUntaggedTopic(t *testing.T) {
	denied := "arn:aws:sns:us-east-1:123456789012:denied"

	mock := &mockSNSClient{
		pages:      [][]snstypes.Topic{{{TopicArn: aws.String(denied)}}},
		tagsErrFor: map[string]bool{denied: true},
	}

	resources, err := testProvider().listSNSTopicsFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("listSNSTopicsFrom() error = %v, want nil", err)
	}
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(resources))
	}
	if len(resources[0].Tags) != 0 {
		t.Errorf("got %d tags, want 0", len(resources[0].Tags))
	}
}

func TestListSNSTopics_ListError(t *testing.T) {
	mock := &mockSNSClient{listErr: errors.New("boom")}

	if _, err := testProvider().listSNSTopicsFrom(context.Background(), mock, "us-east-1"); err == nil {
		t.Fatal("listSNSTopicsFrom() error = nil, want an error")
	}
}

func TestListSNSTopics_Empty(t *testing.T) {
	mock := &mockSNSClient{pages: [][]snstypes.Topic{{}}}

	resources, err := testProvider().listSNSTopicsFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("listSNSTopicsFrom() error = %v", err)
	}
	if len(resources) != 0 {
		t.Errorf("got %d resources, want 0", len(resources))
	}
}

func TestSNSTagsToMap(t *testing.T) {
	tags := snsTagsToMap([]snstypes.Tag{
		{Key: aws.String("a"), Value: aws.String("1")},
		{Key: aws.String("b"), Value: aws.String("2")},
		{Key: nil, Value: aws.String("skipped")},
	})

	if len(tags) != 2 {
		t.Fatalf("got %d tags, want 2 (nil key dropped)", len(tags))
	}
	if tags["a"] != "1" || tags["b"] != "2" {
		t.Errorf("tags = %v, want a=1 b=2", tags)
	}
}

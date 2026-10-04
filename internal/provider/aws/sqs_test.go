package aws

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// mockSQSClient serves ListQueues pages plus per-queue attributes and tags.
type mockSQSClient struct {
	pages   [][]string
	arns    map[string]string
	tags    map[string]map[string]string
	listErr error
	attrErr map[string]bool
	tagErr  map[string]bool
	gone    map[string]bool
	calls   int
}

func (m *mockSQSClient) ListQueues(ctx context.Context, params *sqs.ListQueuesInput, optFns ...func(*sqs.Options)) (*sqs.ListQueuesOutput, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	page := m.pages[m.calls]
	m.calls++
	out := &sqs.ListQueuesOutput{QueueUrls: page}
	if m.calls < len(m.pages) {
		out.NextToken = aws.String("next")
	}
	return out, nil
}

func (m *mockSQSClient) GetQueueAttributes(ctx context.Context, params *sqs.GetQueueAttributesInput, optFns ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error) {
	url := aws.ToString(params.QueueUrl)
	if m.attrErr[url] {
		return nil, errors.New("access denied")
	}
	arn, ok := m.arns[url]
	if !ok {
		return &sqs.GetQueueAttributesOutput{Attributes: map[string]string{}}, nil
	}
	return &sqs.GetQueueAttributesOutput{
		Attributes: map[string]string{string(sqstypes.QueueAttributeNameQueueArn): arn},
	}, nil
}

func (m *mockSQSClient) ListQueueTags(ctx context.Context, params *sqs.ListQueueTagsInput, optFns ...func(*sqs.Options)) (*sqs.ListQueueTagsOutput, error) {
	url := aws.ToString(params.QueueUrl)
	if m.tagErr[url] {
		return nil, errors.New("access denied")
	}
	if m.gone[url] {
		return nil, &sqstypes.QueueDoesNotExist{Message: aws.String("The specified queue does not exist.")}
	}
	return &sqs.ListQueueTagsOutput{Tags: m.tags[url]}, nil
}

func TestListSQSQueues(t *testing.T) {
	jobsURL := "https://sqs.us-east-1.amazonaws.com/123456789012/jobs"
	dlqURL := "https://sqs.us-east-1.amazonaws.com/123456789012/jobs-dlq"

	mock := &mockSQSClient{
		pages: [][]string{{jobsURL}, {dlqURL}},
		arns: map[string]string{
			jobsURL: "arn:aws:sqs:us-east-1:123456789012:jobs",
			dlqURL:  "arn:aws:sqs:us-east-1:123456789012:jobs-dlq",
		},
		tags: map[string]map[string]string{
			jobsURL: {"environment": envProd, "owner": "platform"},
		},
	}

	resources, err := testProvider().listSQSQueuesFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("listSQSQueuesFrom() error = %v", err)
	}

	if len(resources) != 2 {
		t.Fatalf("got %d resources across 2 pages, want 2", len(resources))
	}

	// Attributes are fetched concurrently, so order is not guaranteed.
	sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })

	if resources[0].Name != "jobs" {
		t.Errorf("Name = %q, want %q (last URL segment)", resources[0].Name, "jobs")
	}
	if resources[0].ARN != "arn:aws:sqs:us-east-1:123456789012:jobs" {
		t.Errorf("ARN = %q, want the queue ARN", resources[0].ARN)
	}
	if resources[0].Type != "aws_sqs_queue" {
		t.Errorf("Type = %q, want %q", resources[0].Type, "aws_sqs_queue")
	}
	if len(resources[0].Tags) != 2 {
		t.Errorf("got %d tags, want 2", len(resources[0].Tags))
	}
	if resources[1].Name != "jobs-dlq" {
		t.Errorf("Name = %q, want %q", resources[1].Name, "jobs-dlq")
	}
}

// A queue whose ARN cannot be resolved is skipped, since it cannot be tagged.
func TestListSQSQueues_SkipsQueueWithoutARN(t *testing.T) {
	goodURL := "https://sqs.us-east-1.amazonaws.com/123456789012/good"
	badURL := "https://sqs.us-east-1.amazonaws.com/123456789012/bad"

	mock := &mockSQSClient{
		pages:   [][]string{{goodURL, badURL}},
		arns:    map[string]string{goodURL: "arn:aws:sqs:us-east-1:123456789012:good"},
		attrErr: map[string]bool{badURL: true},
	}

	resources, err := testProvider().listSQSQueuesFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("listSQSQueuesFrom() error = %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1 (queue without ARN skipped)", len(resources))
	}
	if resources[0].Name != "good" {
		t.Errorf("Name = %q, want %q", resources[0].Name, "good")
	}
}

func TestListSQSQueues_UnreadableTagsSkipQueueNotReportUntagged(t *testing.T) {
	jobsURL := "https://sqs.us-east-1.amazonaws.com/123456789012/jobs"
	deniedURL := "https://sqs.us-east-1.amazonaws.com/123456789012/denied"

	mock := &mockSQSClient{
		pages: [][]string{{jobsURL, deniedURL}},
		arns: map[string]string{
			jobsURL:   "arn:aws:sqs:us-east-1:123456789012:jobs",
			deniedURL: "arn:aws:sqs:us-east-1:123456789012:denied",
		},
		tagErr: map[string]bool{deniedURL: true},
	}

	p := testProvider()
	resources, err := p.listSQSQueuesFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("listSQSQueuesFrom() error = %v, want nil", err)
	}
	if len(resources) != 1 || resources[0].Name != "jobs" {
		t.Fatalf("resources = %+v, want only the readable queue", resources)
	}
	if errors.Join(p.skipped.errs()...) == nil {
		t.Error("the skipped queue was not recorded")
	}
}

func TestListSQSQueues_QueueDeletedBeforeTagReadIsDroppedNotSkipped(t *testing.T) {
	jobsURL := "https://sqs.us-east-1.amazonaws.com/123456789012/jobs"
	deletedURL := "https://sqs.us-east-1.amazonaws.com/123456789012/deleted"

	mock := &mockSQSClient{
		pages: [][]string{{jobsURL, deletedURL}},
		arns: map[string]string{
			jobsURL:    "arn:aws:sqs:us-east-1:123456789012:jobs",
			deletedURL: "arn:aws:sqs:us-east-1:123456789012:deleted",
		},
		gone: map[string]bool{deletedURL: true},
	}

	p := testProvider()
	resources, err := p.listSQSQueuesFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("listSQSQueuesFrom() error = %v, want nil", err)
	}
	if len(resources) != 1 || resources[0].Name != "jobs" {
		t.Fatalf("resources = %+v, want only the queue that still exists", resources)
	}
	if err := errors.Join(p.skipped.errs()...); err != nil {
		t.Errorf("a deleted queue was counted as skipped: %v", err)
	}
}

func TestListSQSQueues_ListError(t *testing.T) {
	mock := &mockSQSClient{listErr: errors.New("boom")}

	if _, err := testProvider().listSQSQueuesFrom(context.Background(), mock, "us-east-1"); err == nil {
		t.Fatal("listSQSQueuesFrom() error = nil, want an error")
	}
}

func TestSQSQueueURLFromARN(t *testing.T) {
	tests := []struct {
		name       string
		arn        string
		wantRegion string
		wantURL    string
		wantErr    bool
	}{
		{
			name:       "valid ARN",
			arn:        "arn:aws:sqs:eu-west-1:123456789012:my-queue",
			wantRegion: "eu-west-1",
			wantURL:    "https://sqs.eu-west-1.amazonaws.com/123456789012/my-queue",
		},
		{
			name:       "China partition",
			arn:        "arn:aws-cn:sqs:cn-north-1:123456789012:my-queue",
			wantRegion: "cn-north-1",
			wantURL:    "https://sqs.cn-north-1.amazonaws.com.cn/123456789012/my-queue",
		},
		{
			name:       "GovCloud partition",
			arn:        "arn:aws-us-gov:sqs:us-gov-west-1:123456789012:my-queue",
			wantRegion: "us-gov-west-1",
			wantURL:    "https://sqs.us-gov-west-1.amazonaws.com/123456789012/my-queue",
		},
		{
			name:    "no region",
			arn:     "arn:aws:sqs::123456789012:my-queue",
			wantErr: true,
		},
		{
			name:    "too few segments",
			arn:     "arn:aws:sqs:eu-west-1:123456789012",
			wantErr: true,
		},
		{
			name:    "empty queue name",
			arn:     "arn:aws:sqs:eu-west-1:123456789012:",
			wantErr: true,
		},
		{
			name:    "not an ARN",
			arn:     "my-queue",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			region, url, err := sqsQueueURLFromARN(tt.arn)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("sqsQueueURLFromARN(%q) error = nil, want an error", tt.arn)
				}
				return
			}
			if err != nil {
				t.Fatalf("sqsQueueURLFromARN(%q) error = %v", tt.arn, err)
			}
			if region != tt.wantRegion {
				t.Errorf("region = %q, want %q", region, tt.wantRegion)
			}
			if url != tt.wantURL {
				t.Errorf("url = %q, want %q", url, tt.wantURL)
			}
		})
	}
}

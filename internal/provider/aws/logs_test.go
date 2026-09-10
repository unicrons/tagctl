package aws

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	logstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

type mockLogsClient struct {
	mu         sync.Mutex
	pages      [][]logstypes.LogGroup
	tags       map[string]map[string]string
	tagsErrFor map[string]bool
	listErr    error
	calls      int
	asked      []string
}

func (m *mockLogsClient) DescribeLogGroups(ctx context.Context, params *cloudwatchlogs.DescribeLogGroupsInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.DescribeLogGroupsOutput, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	page := m.pages[m.calls]
	m.calls++
	out := &cloudwatchlogs.DescribeLogGroupsOutput{LogGroups: page}
	if m.calls < len(m.pages) {
		out.NextToken = aws.String("next")
	}
	return out, nil
}

func (m *mockLogsClient) ListTagsForResource(ctx context.Context, params *cloudwatchlogs.ListTagsForResourceInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.ListTagsForResourceOutput, error) {
	arn := aws.ToString(params.ResourceArn)
	m.mu.Lock()
	m.asked = append(m.asked, arn)
	m.mu.Unlock()
	if m.tagsErrFor[arn] {
		return nil, errors.New("access denied")
	}
	return &cloudwatchlogs.ListTagsForResourceOutput{Tags: m.tags[arn]}, nil
}

func (m *mockLogsClient) TagResource(ctx context.Context, params *cloudwatchlogs.TagResourceInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.TagResourceOutput, error) {
	return &cloudwatchlogs.TagResourceOutput{}, nil
}

const lambdaGroupARN = "arn:aws:logs:us-east-1:123456789012:log-group:/aws/lambda/fn"

func TestListLogGroups(t *testing.T) {
	created := int64(1_700_000_000_000)
	mock := &mockLogsClient{
		pages: [][]logstypes.LogGroup{
			{{
				LogGroupName: aws.String("/aws/lambda/fn"),
				LogGroupArn:  aws.String(lambdaGroupARN),
				Arn:          aws.String(lambdaGroupARN + ":*"),
				CreationTime: &created,
			}},
			{{
				// Older API responses only carry the legacy ARN with the ":*" suffix.
				LogGroupName: aws.String("/ecs/api"),
				Arn:          aws.String("arn:aws:logs:us-east-1:123456789012:log-group:/ecs/api:*"),
			}},
		},
		tags: map[string]map[string]string{lambdaGroupARN: {"environment": envProd}},
	}

	resources, err := testProvider().listLogGroupsFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("got %d log groups across 2 pages, want 2", len(resources))
	}
	for _, r := range resources {
		if r.Type != "aws_cloudwatch_log_group" || r.Tags == nil {
			t.Errorf("unexpected resource: %+v", r)
		}
		switch r.ID {
		case "/aws/lambda/fn":
			if r.Tags["environment"] != envProd || r.CreatedAt == nil || r.CreatedAt.UnixMilli() != created {
				t.Errorf("lambda group = %+v", r)
			}
		case "/ecs/api":
			if r.ARN != "arn:aws:logs:us-east-1:123456789012:log-group:/ecs/api" {
				t.Errorf("legacy ARN should lose its :* suffix, got %q", r.ARN)
			}
		}
	}
	for _, arn := range mock.asked {
		if len(arn) > 2 && arn[len(arn)-2:] == ":*" {
			t.Errorf("tags requested with a :* ARN, which the API rejects: %s", arn)
		}
	}
}

func TestListLogGroups_UnreadableGroupIsSkipped(t *testing.T) {
	mock := &mockLogsClient{
		pages: [][]logstypes.LogGroup{{
			{LogGroupName: aws.String("denied"), LogGroupArn: aws.String("arn:aws:logs:us-east-1:123456789012:log-group:denied")},
			{LogGroupName: aws.String("ok"), LogGroupArn: aws.String("arn:aws:logs:us-east-1:123456789012:log-group:ok")},
		}},
		tagsErrFor: map[string]bool{"arn:aws:logs:us-east-1:123456789012:log-group:denied": true},
	}

	resources, err := testProvider().listLogGroupsFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 1 || resources[0].ID != "ok" {
		t.Errorf("got %+v, want only the readable group", resources)
	}
}

func TestListLogGroups_ListError(t *testing.T) {
	mock := &mockLogsClient{listErr: errors.New("boom")}
	if _, err := testProvider().listLogGroupsFrom(context.Background(), mock, "us-east-1"); err == nil {
		t.Fatal("expected error")
	}
}

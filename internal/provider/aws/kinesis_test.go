package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	kinesistypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
)

type mockKinesisClient struct {
	pages      [][]kinesistypes.StreamSummary
	tags       map[string][]kinesistypes.Tag
	tagsErrFor map[string]bool
	listErr    error
	calls      int
}

func (m *mockKinesisClient) ListStreams(ctx context.Context, params *kinesis.ListStreamsInput, optFns ...func(*kinesis.Options)) (*kinesis.ListStreamsOutput, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	page := m.pages[m.calls]
	m.calls++
	out := &kinesis.ListStreamsOutput{StreamSummaries: page, HasMoreStreams: aws.Bool(false)}
	if m.calls < len(m.pages) {
		out.NextToken = aws.String("next")
		out.HasMoreStreams = aws.Bool(true)
	}
	return out, nil
}

func (m *mockKinesisClient) ListTagsForStream(ctx context.Context, params *kinesis.ListTagsForStreamInput, optFns ...func(*kinesis.Options)) (*kinesis.ListTagsForStreamOutput, error) {
	arn := aws.ToString(params.StreamARN)
	if m.tagsErrFor[arn] {
		return nil, errors.New("access denied")
	}
	all := m.tags[arn]
	// Two pages when there is more than one tag, to exercise ExclusiveStartTagKey.
	if params.ExclusiveStartTagKey == nil && len(all) > 1 {
		return &kinesis.ListTagsForStreamOutput{Tags: all[:1], HasMoreTags: aws.Bool(true)}, nil
	}
	if params.ExclusiveStartTagKey != nil {
		return &kinesis.ListTagsForStreamOutput{Tags: all[1:], HasMoreTags: aws.Bool(false)}, nil
	}
	return &kinesis.ListTagsForStreamOutput{Tags: all, HasMoreTags: aws.Bool(false)}, nil
}

func (m *mockKinesisClient) AddTagsToStream(ctx context.Context, params *kinesis.AddTagsToStreamInput, optFns ...func(*kinesis.Options)) (*kinesis.AddTagsToStreamOutput, error) {
	return &kinesis.AddTagsToStreamOutput{}, nil
}

func stream(name string) kinesistypes.StreamSummary {
	return kinesistypes.StreamSummary{
		StreamName: aws.String(name),
		StreamARN:  aws.String("arn:aws:kinesis:us-east-1:123456789012:stream/" + name),
	}
}

func TestListKinesisStreams(t *testing.T) {
	events := "arn:aws:kinesis:us-east-1:123456789012:stream/events"
	mock := &mockKinesisClient{
		pages: [][]kinesistypes.StreamSummary{{stream("events")}, {stream("audit")}},
		tags: map[string][]kinesistypes.Tag{events: {
			{Key: aws.String("environment"), Value: aws.String(envProd)},
			{Key: aws.String("owner"), Value: aws.String("data@example.com")},
		}},
	}

	resources, err := testProvider().listKinesisStreamsFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("got %d streams across 2 pages, want 2", len(resources))
	}
	for _, r := range resources {
		if r.Type != "aws_kinesis_stream" || r.ARN == "" {
			t.Errorf("unexpected resource: %+v", r)
		}
		if r.ID == "events" && (r.Tags["environment"] != envProd || r.Tags["owner"] != "data@example.com") {
			t.Errorf("tags = %v, want both tag pages merged", r.Tags)
		}
	}
}

func TestListKinesisStreams_UnreadableStreamIsSkipped(t *testing.T) {
	denied := "arn:aws:kinesis:us-east-1:123456789012:stream/denied"
	mock := &mockKinesisClient{
		pages:      [][]kinesistypes.StreamSummary{{stream("denied"), stream("ok")}},
		tagsErrFor: map[string]bool{denied: true},
	}

	resources, err := testProvider().listKinesisStreamsFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 1 || resources[0].ID != "ok" {
		t.Errorf("got %+v, want only the readable stream", resources)
	}
}

func TestListKinesisStreams_ListError(t *testing.T) {
	mock := &mockKinesisClient{listErr: errors.New("boom")}
	if _, err := testProvider().listKinesisStreamsFrom(context.Background(), mock, "us-east-1"); err == nil {
		t.Fatal("expected error")
	}
}

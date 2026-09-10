package aws

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	asgtypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
)

// mockAutoScalingClient serves DescribeAutoScalingGroups pages.
type mockAutoScalingClient struct {
	pages [][]asgtypes.AutoScalingGroup
	err   error
	calls int
}

func (m *mockAutoScalingClient) DescribeAutoScalingGroups(ctx context.Context, params *autoscaling.DescribeAutoScalingGroupsInput, optFns ...func(*autoscaling.Options)) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	page := m.pages[m.calls]
	m.calls++
	out := &autoscaling.DescribeAutoScalingGroupsOutput{AutoScalingGroups: page}
	if m.calls < len(m.pages) {
		out.NextToken = aws.String("next")
	}
	return out, nil
}

func TestListAutoScalingGroups(t *testing.T) {
	arn := "arn:aws:autoscaling:us-east-1:123456789012:autoScalingGroup:uuid:autoScalingGroupName/web-asg"
	created := time.Date(2026, 2, 3, 9, 0, 0, 0, time.UTC)

	mock := &mockAutoScalingClient{
		pages: [][]asgtypes.AutoScalingGroup{
			{
				{
					AutoScalingGroupName: aws.String("web-asg"),
					AutoScalingGroupARN:  aws.String(arn),
					CreatedTime:          aws.Time(created),
					// Auto Scaling returns tags inline, no extra call needed.
					Tags: []asgtypes.TagDescription{
						{Key: aws.String("environment"), Value: aws.String(envProd)},
						{Key: aws.String("owner"), Value: aws.String("platform")},
					},
				},
			},
			{
				{
					AutoScalingGroupName: aws.String("batch-asg"),
					AutoScalingGroupARN:  aws.String("arn:aws:autoscaling:us-east-1:123456789012:autoScalingGroup:uuid:autoScalingGroupName/batch-asg"),
				},
			},
		},
	}

	resources, err := testProvider().listAutoScalingGroupsFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("listAutoScalingGroupsFrom() error = %v", err)
	}

	if len(resources) != 2 {
		t.Fatalf("got %d resources across 2 pages, want 2", len(resources))
	}

	if resources[0].ID != "web-asg" || resources[0].Name != "web-asg" {
		t.Errorf("ID/Name = %q/%q, want web-asg/web-asg", resources[0].ID, resources[0].Name)
	}
	if resources[0].Type != "aws_autoscaling_group" {
		t.Errorf("Type = %q, want %q", resources[0].Type, "aws_autoscaling_group")
	}
	if resources[0].ARN != arn {
		t.Errorf("ARN = %q, want %q", resources[0].ARN, arn)
	}
	if len(resources[0].Tags) != 2 {
		t.Errorf("got %d tags, want 2", len(resources[0].Tags))
	}
	if resources[0].Tags["environment"] != envProd {
		t.Errorf("Tags[environment] = %q, want %q", resources[0].Tags["environment"], envProd)
	}
	if resources[0].CreatedAt == nil || !resources[0].CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", resources[0].CreatedAt, created)
	}

	if len(resources[1].Tags) != 0 {
		t.Errorf("got %d tags for untagged group, want 0", len(resources[1].Tags))
	}
	if resources[1].CreatedAt != nil {
		t.Errorf("CreatedAt = %v, want nil", resources[1].CreatedAt)
	}
}

func TestListAutoScalingGroups_Error(t *testing.T) {
	mock := &mockAutoScalingClient{err: errors.New("boom")}

	if _, err := testProvider().listAutoScalingGroupsFrom(context.Background(), mock, "us-east-1"); err == nil {
		t.Fatal("listAutoScalingGroupsFrom() error = nil, want an error")
	}
}

func TestListAutoScalingGroups_Empty(t *testing.T) {
	mock := &mockAutoScalingClient{pages: [][]asgtypes.AutoScalingGroup{{}}}

	resources, err := testProvider().listAutoScalingGroupsFrom(context.Background(), mock, "us-east-1")
	if err != nil {
		t.Fatalf("listAutoScalingGroupsFrom() error = %v", err)
	}
	if len(resources) != 0 {
		t.Errorf("got %d resources, want 0", len(resources))
	}
}

func TestASGNameFromARN(t *testing.T) {
	tests := []struct {
		name string
		arn  string
		want string
	}{
		{
			name: "full ARN",
			arn:  "arn:aws:autoscaling:us-east-1:123456789012:autoScalingGroup:uuid:autoScalingGroupName/web-asg",
			want: "web-asg",
		},
		{
			name: "colon-separated only",
			arn:  "arn:aws:autoscaling:us-east-1:123456789012:web-asg",
			want: "web-asg",
		},
		{
			name: "bare name",
			arn:  "web-asg",
			want: "web-asg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := asgNameFromARN(tt.arn); got != tt.want {
				t.Errorf("asgNameFromARN(%q) = %q, want %q", tt.arn, got, tt.want)
			}
		})
	}
}

func TestASGTagsToMap(t *testing.T) {
	tags := asgTagsToMap([]asgtypes.TagDescription{
		{Key: aws.String("a"), Value: aws.String("1")},
		{Key: aws.String("b"), Value: nil},
	})

	if len(tags) != 1 {
		t.Fatalf("got %d tags, want 1 (nil value dropped)", len(tags))
	}
	if tags["a"] != "1" {
		t.Errorf("tags[a] = %q, want %q", tags["a"], "1")
	}
}

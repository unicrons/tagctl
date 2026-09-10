package engine

import (
	"context"
	"testing"
	"time"

	"github.com/unicrons/tagctl/internal/types"
)

func TestMockApplier_Apply(t *testing.T) {
	applier := NewMockApplier()
	plan := &types.Plan{
		ID:        "test-plan",
		CreatedAt: time.Now(),
		Changes: []types.TagChange{
			{
				Resource: types.Resource{ID: "i-123", Type: "aws_instance"},
				Tag:      "environment",
				Action:   types.ActionAdd,
				NewValue: "prod",
			},
		},
	}

	result, err := applier.Apply(context.Background(), plan)

	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if result == nil {
		t.Fatal("Apply() returned nil result")
	}

	if result.TotalChanges != 1 {
		t.Errorf("TotalChanges = %d, want 1", result.TotalChanges)
	}

	if result.SuccessCount != 1 {
		t.Errorf("SuccessCount = %d, want 1", result.SuccessCount)
	}

	if result.ErrorCount != 0 {
		t.Errorf("ErrorCount = %d, want 0", result.ErrorCount)
	}
}

func TestMockApplier_Apply_EmptyPlan(t *testing.T) {
	applier := NewMockApplier()
	plan := &types.Plan{
		ID:        "empty-plan",
		CreatedAt: time.Now(),
		Changes:   []types.TagChange{},
	}

	result, err := applier.Apply(context.Background(), plan)

	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if result.TotalChanges != 0 {
		t.Errorf("TotalChanges = %d, want 0", result.TotalChanges)
	}
}

func TestTaggingIdentifier(t *testing.T) {
	cases := []struct {
		typ  string
		want string
	}{
		{"aws_instance", "i-1"},
		{"aws_s3_bucket", "i-1"},
		{"aws_db_instance", "arn:x"},
		{"aws_lambda_function", "arn:x"},
		{"aws_dynamodb_table", "arn:x"},
		{"aws_ecs_service", "arn:x"},
		{"aws_eks_cluster", "arn:x"},
		{"aws_elasticache_cluster", "arn:x"},
		{"aws_efs_file_system", "arn:x"},
		{"aws_ecr_repository", "arn:x"},
		{"aws_kms_key", "arn:x"},
		{"aws_kinesis_stream", "arn:x"},
		{"aws_cloudwatch_log_group", "arn:x"},
		{"aws_rds_cluster", "arn:x"},
		{"aws_elb", "arn:x"},
		{"aws_lb_target_group", "arn:x"},
		{"aws_sfn_state_machine", "arn:x"},
		{"aws_iam_role", "arn:x"},
		{"aws_ami", "i-1"},
		{"aws_eip", "i-1"},
		{"aws_lb", "arn:x"},
		{"aws_autoscaling_group", "arn:x"},
		{"aws_sns_topic", "arn:x"},
		{"aws_sqs_queue", "arn:x"},
		{"aws_cloudtrail", "arn:x"},
		{"aws_lightsail_instance", "arn:x"},
		{"aws_docdb_cluster", "arn:x"},
	}
	for _, tc := range cases {
		r := types.Resource{Provider: "aws", ID: "i-1", ARN: "arn:x", Type: tc.typ}
		if got := taggingIdentifier(r); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.typ, got, tc.want)
		}
	}
	if got := taggingIdentifier(types.Resource{Provider: "aws", ID: "i-1", Type: "aws_kms_key"}); got != "i-1" {
		t.Errorf("without ARN want the ID, got %q", got)
	}
}

// Changes for the same ID in two regions must reach two resources, not be
// applied twice to whichever one comes first.
func TestGroupChangesByResource_SeparatesRegions(t *testing.T) {
	changes := []types.TagChange{
		{Resource: types.Resource{ID: "/aws/lambda/fn", Provider: "aws", Account: "111", Region: "eu-west-1"}, Tag: "owner"},
		{Resource: types.Resource{ID: "/aws/lambda/fn", Provider: "aws", Account: "111", Region: "us-east-1"}, Tag: "owner"},
		{Resource: types.Resource{ID: "/aws/lambda/fn", Provider: "aws", Account: "111", Region: "us-east-1"}, Tag: "team"},
	}

	grouped := groupChangesByResource(changes)

	if len(grouped) != 2 {
		t.Fatalf("grouped into %d resources, want 2", len(grouped))
	}
	if got := len(grouped["aws/111/us-east-1//aws/lambda/fn"]); got != 2 {
		t.Errorf("us-east-1 copy has %d changes, want 2", got)
	}
}

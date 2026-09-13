package engine

import (
	"context"
	"testing"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/types"
)

func missing(tag string, r types.Resource) types.Violation {
	return types.Violation{Resource: r, Tag: tag, Reason: types.ReasonMissing}
}

func TestRealPlanner_TryInfer(t *testing.T) {
	planner, err := NewPlanner(config.RulesConfig{Infer: []config.InferRule{{
		Tag:      "environment",
		FromName: []config.NamePattern{{Pattern: "-prod-", Value: "prod"}, {Pattern: "-dev-", Value: "dev"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name      string
		violation types.Violation
		want      string
	}{
		{"name matches the second pattern", missing("environment", types.Resource{Name: "db-dev-1"}), "dev"},
		{"id is used when there is no name", missing("environment", types.Resource{ID: "web-prod-1"}), "prod"},
		{"no pattern matches", missing("environment", types.Resource{Name: "cache"}), ""},
		{"no rule for the tag", missing("owner", types.Resource{Name: "web-prod-1"}), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			change := planner.tryInfer(tc.violation)
			if got := newValue(change); got != tc.want {
				t.Errorf("tryInfer() value = %q, want %q", got, tc.want)
			}
			if change != nil && change.Reason != types.ReasonInferred {
				t.Errorf("tryInfer() reason = %s, want %s", change.Reason, types.ReasonInferred)
			}
		})
	}
}

func TestRealPlanner_TryDefault(t *testing.T) {
	planner, err := NewPlanner(config.RulesConfig{Defaults: []config.DefaultRule{
		{Resource: "aws_*_bucket", When: map[string]string{"tag:Environment": "prod"}, Set: map[string]string{"CostCenter": "CC-PROD"}},
		{Resource: "aws_*", When: map[string]string{"tag:Owner": "absent"}, Set: map[string]string{"CostCenter": "CC-1"}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name      string
		violation types.Violation
		want      string
	}{
		{"exact value condition wins by order", missing("CostCenter", types.Resource{Type: "aws_s3_bucket", Tags: map[string]string{"Environment": "prod"}}), "CC-PROD"},
		{"absent condition", missing("CostCenter", types.Resource{Type: "aws_instance"}), "CC-1"},
		{"condition key is case sensitive", missing("CostCenter", types.Resource{Type: "aws_instance", Tags: map[string]string{"owner": "a@example.com"}}), "CC-1"},
		{"absent condition fails when the tag is set", missing("CostCenter", types.Resource{Type: "aws_instance", Tags: map[string]string{"Owner": "a@example.com"}}), ""},
		{"resource glob does not match", missing("CostCenter", types.Resource{Type: "k8s_pod"}), ""},
		{"no rule sets the tag", missing("costcenter", types.Resource{Type: "aws_instance"}), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			change := planner.tryDefault(tc.violation)
			if got := newValue(change); got != tc.want {
				t.Errorf("tryDefault() value = %q, want %q", got, tc.want)
			}
			if change != nil && change.Reason != types.ReasonDefault {
				t.Errorf("tryDefault() reason = %s, want %s", change.Reason, types.ReasonDefault)
			}
		})
	}
}

func TestRealPlanner_CheckConditions(t *testing.T) {
	resource := types.Resource{Tags: map[string]string{"Environment": "prod"}}
	cases := []struct {
		name string
		when map[string]string
		want bool
	}{
		{"no conditions", nil, true},
		{"tag absent", map[string]string{"tag:Owner": "absent"}, true},
		{"tag present when absent is required", map[string]string{"tag:Environment": "absent"}, false},
		{"exact value", map[string]string{"tag:Environment": "prod"}, true},
		{"different value", map[string]string{"tag:Environment": "dev"}, false},
		{"every condition must hold", map[string]string{"tag:Environment": "prod", "tag:Owner": "someone"}, false},
		{"key without tag prefix fails closed", map[string]string{"Owner": "absent"}, false},
		{"key without tag name fails closed", map[string]string{"tag:": "absent"}, false},
	}
	planner := &RealPlanner{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := planner.checkConditions(tc.when, missing("CostCenter", resource)); got != tc.want {
				t.Errorf("checkConditions(%v) = %v, want %v", tc.when, got, tc.want)
			}
		})
	}
}

func TestRealPlanner_PlanFixesOnlyMissingTags(t *testing.T) {
	planner, err := NewPlanner(config.RulesConfig{Defaults: []config.DefaultRule{
		{Resource: "*", Set: map[string]string{"owner": "platform@example.com"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	bucket := types.Resource{ARN: "arn:aws:s3:::logs", Type: "aws_s3_bucket"}
	result := types.NewScanResult()
	result.Violations = []types.Violation{
		missing("owner", bucket),
		{Resource: bucket, Tag: "owner", Reason: types.ReasonInvalidFormat, Actual: "john"},
	}

	plan, err := planner.Plan(context.Background(), result)
	if err != nil {
		t.Fatalf("Plan() = %v", err)
	}
	if len(plan.Changes) != 1 || plan.Summary.TotalResources != 1 || plan.Changes[0].NewValue != "platform@example.com" {
		t.Errorf("plan = %+v, want one default change for the missing tag", plan)
	}
}

func newValue(change *types.TagChange) string {
	if change == nil {
		return ""
	}
	return change.NewValue
}

func TestMockPlanner_Plan(t *testing.T) {
	planner := NewMockPlanner()
	scanResult := types.NewScanResult()

	plan, err := planner.Plan(context.Background(), scanResult)

	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	if plan == nil {
		t.Fatal("Plan() returned nil")
	}

	if plan.IsEmpty() {
		t.Error("Plan should not be empty")
	}

	if plan.ID == "" {
		t.Error("Plan ID should not be empty")
	}

	if plan.Summary.TotalChanges != len(plan.Changes) {
		t.Errorf("Summary.TotalChanges = %d, want %d", plan.Summary.TotalChanges, len(plan.Changes))
	}
}

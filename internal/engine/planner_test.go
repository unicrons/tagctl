package engine

import (
	"context"
	"testing"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/types"
)

func missing(tag string, r types.Resource) types.Finding {
	return types.Finding{Resource: r, Tag: tag, Status: types.StatusFailed, Reason: types.ReasonMissing}
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
		name    string
		finding types.Finding
		want    string
	}{
		{"name matches the second pattern", missing("environment", types.Resource{Name: "db-dev-1"}), "dev"},
		{"id is used when there is no name", missing("environment", types.Resource{ID: "web-prod-1"}), "prod"},
		{"no pattern matches", missing("environment", types.Resource{Name: "cache"}), ""},
		{"no rule for the tag", missing("owner", types.Resource{Name: "web-prod-1"}), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			change := planner.tryInfer(tc.finding)
			if got := newValue(change); got != tc.want {
				t.Errorf("tryInfer() value = %q, want %q", got, tc.want)
			}
			if change != nil && change.Reason != types.ReasonInferred {
				t.Errorf("tryInfer() reason = %s, want %s", change.Reason, types.ReasonInferred)
			}
		})
	}
}

func TestRealPlanner_TryInferFromTag(t *testing.T) {
	planner, err := NewPlanner(config.RulesConfig{Infer: []config.InferRule{{
		Tag: "environment",
		FromTag: []config.TagSource{
			{Tag: "env", Values: map[string]string{"production": "prod", "development": "dev"}},
			{Tag: "Environment"},
		},
		FromName: []config.NamePattern{{Pattern: "-stg-", Value: "staging"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	tagged := func(name string, tags map[string]string) types.Finding {
		return missing("environment", types.Resource{Name: name, Tags: tags})
	}

	cases := []struct {
		name       string
		finding    types.Finding
		want       string
		wantSource string
	}{
		{"mapped value", tagged("db", map[string]string{"env": "production"}), "prod", "from tag 'env'"},
		{"value copied when there is no mapping", tagged("db", map[string]string{"Environment": "qa"}), "qa", "from tag 'Environment'"},
		{"unmapped value falls through to the next source", tagged("db", map[string]string{"env": "sandbox", "Environment": "dev"}), "dev", "from tag 'Environment'"},
		{"source tag wins over the name", tagged("db-stg-1", map[string]string{"env": "production"}), "prod", "from tag 'env'"},
		{"name is used when no source tag is usable", tagged("db-stg-1", map[string]string{"env": "sandbox"}), "staging", "name matches '-stg-'"},
		{"empty source value infers nothing", tagged("db", map[string]string{"Environment": ""}), "", ""},
		{"source tag key is case sensitive", tagged("db", map[string]string{"ENV": "production"}), "", ""},
		{"no rule for the tag", missing("owner", types.Resource{Tags: map[string]string{"env": "production"}}), "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			change := planner.tryInfer(tc.finding)
			if got := newValue(change); got != tc.want {
				t.Fatalf("tryInfer() value = %q, want %q", got, tc.want)
			}
			if change == nil {
				return
			}
			if change.Source != tc.wantSource || change.Reason != types.ReasonInferred || change.Action != types.ActionAdd {
				t.Errorf("tryInfer() = %+v, want an inferred add with source %q", change, tc.wantSource)
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
		name    string
		finding types.Finding
		want    string
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
			change := planner.tryDefault(tc.finding)
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
	missingOwner := missing("owner", bucket)
	invalidOwner := types.Finding{Resource: bucket, Tag: "owner", Status: types.StatusFailed, Reason: types.ReasonInvalidFormat, Actual: "john"}
	passingEnv := types.Finding{Resource: bucket, Tag: "environment", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "prod"}

	cases := []struct {
		name string
		scan *types.ScanResult
	}{
		{"findings", &types.ScanResult{Findings: []types.Finding{passingEnv, missingOwner, invalidOwner}}},
		{"legacy violations", &types.ScanResult{Violations: []types.Violation{types.Violation(missingOwner), types.Violation(invalidOwner)}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := planner.Plan(context.Background(), tc.scan)
			if err != nil {
				t.Fatalf("Plan() = %v", err)
			}
			if len(plan.Changes) != 1 || plan.Summary.TotalResources != 1 || plan.Changes[0].NewValue != "platform@example.com" {
				t.Errorf("plan = %+v, want one default change for the missing tag", plan)
			}
		})
	}
}

func newValue(change *types.TagChange) string {
	if change == nil {
		return ""
	}
	return change.NewValue
}

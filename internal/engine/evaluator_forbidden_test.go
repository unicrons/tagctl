package engine

import (
	"testing"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/types"
)

func forbiddenEvaluator(t *testing.T) *Evaluator {
	t.Helper()
	evaluator, err := NewEvaluator(config.PolicyConfig{
		Required: []config.TagRequirement{{Name: "environment", Values: []string{"dev", "prod", "test"}}},
		Optional: []config.TagRequirement{{Name: "team"}},
		Forbidden: []config.ForbiddenTag{
			{Name: "Env"},
			{Name: "environment", Values: []string{"test"}},
			{Name: "team", Pattern: "^tmp-"},
			{Name: "owner", Values: []string{"nobody"}, Pattern: "^unknown"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return evaluator
}

func TestEvaluator_ForbiddenTags(t *testing.T) {
	type want struct {
		status   types.FindingStatus
		reason   types.ViolationReason
		expected string
	}
	failedAs := func(expected string) want { return want{types.StatusFailed, types.ReasonForbidden, expected} }
	pass := want{types.StatusPass, types.ReasonCompliant, ""}

	cases := []struct {
		name     string
		resource types.Resource
		want     map[string]want
	}{
		{
			"forbidden key present",
			types.Resource{Tags: map[string]string{"environment": "prod", "Env": "prod"}},
			map[string]want{"environment": pass, "Env": failedAs("")},
		},
		{
			"forbidden key with an empty value",
			types.Resource{Tags: map[string]string{"environment": "prod", "Env": ""}},
			map[string]want{"environment": pass, "Env": failedAs("")},
		},
		{
			"forbidden key absent produces no finding",
			types.Resource{Tags: map[string]string{"environment": "prod"}},
			map[string]want{"environment": pass},
		},
		{
			"forbidden value of a required tag replaces its finding",
			types.Resource{Tags: map[string]string{"environment": "test"}},
			map[string]want{"environment": failedAs("not one of: test")},
		},
		{
			"forbidden pattern of an optional tag",
			types.Resource{Tags: map[string]string{"environment": "prod", "team": "tmp-squad"}},
			map[string]want{"environment": pass, "team": failedAs("not matching: ^tmp-")},
		},
		{
			"value outside the forbidden pattern",
			types.Resource{Tags: map[string]string{"environment": "prod", "team": "payments"}},
			map[string]want{"environment": pass, "team": pass},
		},
		{
			"forbidden value or pattern of an unchecked tag",
			types.Resource{Tags: map[string]string{"environment": "prod", "owner": "unknown-team"}},
			map[string]want{"environment": pass, "owner": failedAs("not matching: ^unknown")},
		},
		{
			"allowed value of an unchecked tag produces no finding",
			types.Resource{Tags: map[string]string{"environment": "prod", "owner": "team@example.com"}},
			map[string]want{"environment": pass},
		},
		{
			"key known only after apply is still forbidden",
			types.Resource{Tags: map[string]string{"environment": "prod"}, UnknownTags: []string{"Env"}},
			map[string]want{"environment": pass, "Env": failedAs("")},
		},
		{
			"value known only after apply cannot match a value rule",
			types.Resource{Tags: map[string]string{"environment": "prod"}, UnknownTags: []string{"owner"}},
			map[string]want{"environment": pass},
		},
	}

	evaluator := forbiddenEvaluator(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := evaluator.EvaluateResourceFindings(tc.resource)
			if len(findings) != len(tc.want) {
				t.Fatalf("findings = %+v, want %d", findings, len(tc.want))
			}
			for _, f := range findings {
				w, ok := tc.want[f.Tag]
				if !ok || f.Status != w.status || f.Reason != w.reason || f.Expected != w.expected {
					t.Errorf("finding for %q = %s/%s expected %q, want %+v", f.Tag, f.Status, f.Reason, f.Expected, w)
				}
				if f.Reason == types.ReasonForbidden && f.Actual != tc.resource.Tags[f.Tag] {
					t.Errorf("finding for %q: Actual = %q, want the tag value", f.Tag, f.Actual)
				}
			}
		})
	}
}

func TestEvaluator_ForbiddenTagsCountInTheScan(t *testing.T) {
	result := forbiddenEvaluator(t).EvaluateResources([]types.Resource{
		{ID: "a", Provider: "aws", Tags: map[string]string{"environment": "prod", "Env": "prod"}},
		{ID: "b", Provider: "aws", Tags: map[string]string{"environment": "test"}},
		{ID: "c", Provider: "aws", Tags: map[string]string{"environment": "dev"}},
	})

	if result.CompliantCount != 1 || result.ViolationCount != 2 {
		t.Errorf("compliant = %d, violations = %d, want 1 and 2", result.CompliantCount, result.ViolationCount)
	}
	env := result.ByTag["Env"]
	if env == nil || !env.Forbidden || env.Required || env.Present != 1 || env.Invalid != 1 || env.Missing != 0 || env.CompliancePct != 0 {
		t.Errorf("by_tag[Env] = %+v, want a forbidden tag carried once", env)
	}
	environment := result.ByTag["environment"]
	if !environment.Required || !environment.Forbidden || environment.Present != 3 || environment.Invalid != 1 {
		t.Errorf("by_tag[environment] = %+v, want required, 3 present and 1 invalid", environment)
	}
	if owner := result.ByTag["owner"]; owner == nil || owner.CompliancePct != 100 {
		t.Errorf("by_tag[owner] = %+v, want 100%% when no resource carries a forbidden value", owner)
	}
}

func TestNewEvaluator_RejectsInvalidForbiddenPattern(t *testing.T) {
	_, err := NewEvaluator(config.PolicyConfig{Forbidden: []config.ForbiddenTag{{Name: "Env", Pattern: "[a-"}}})
	if err == nil {
		t.Fatal("NewEvaluator() = nil, want an error for the pattern")
	}
}

package engine

import (
	"testing"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/types"
)

func TestEvaluate_UnknownTagValueSkipsValueAndPatternChecks(t *testing.T) {
	evaluator, err := NewEvaluator(config.PolicyConfig{
		Required: []config.TagRequirement{
			{Name: "environment", Values: []string{"staging", valueProd}},
			{Name: "Name", Pattern: "^app-[0-9a-f]+$"},
		},
	})
	if err != nil {
		t.Fatalf("NewEvaluator() error = %v", err)
	}

	resource := types.Resource{
		ID:          "aws_instance.web",
		Tags:        map[string]string{"environment": ""},
		UnknownTags: []string{"Name", "environment"},
	}

	result := evaluator.EvaluateResources([]types.Resource{resource})

	statusByTag := make(map[string]types.Finding, len(result.Findings))
	for _, finding := range result.Findings {
		statusByTag[finding.Tag] = finding
	}
	for _, tag := range []string{"environment", "Name"} {
		finding, present := statusByTag[tag]
		if !present {
			t.Errorf("no finding for %s", tag)
			continue
		}
		if finding.Status != types.StatusPass || finding.Reason != types.ReasonCompliant {
			t.Errorf("finding for %s = %s/%s, want PASS/compliant", tag, finding.Status, finding.Reason)
		}
	}
	if result.CompliantCount != 1 {
		t.Errorf("CompliantCount = %d, want 1", result.CompliantCount)
	}
	if !evaluator.IsCompliant(resource) {
		t.Error("IsCompliant() = false for a resource whose only tag values are unknown")
	}
}

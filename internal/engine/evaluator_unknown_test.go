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

	for _, finding := range evaluator.EvaluateResourceFindings(resource) {
		if finding.Status != types.StatusPass || finding.Reason != types.ReasonCompliant {
			t.Errorf("finding for %s = %s/%s, want PASS/compliant", finding.Tag, finding.Status, finding.Reason)
		}
	}
	if violations := evaluator.EvaluateResource(resource); len(violations) != 0 {
		t.Errorf("EvaluateResource() = %+v, want no violations", violations)
	}
	if result := evaluator.EvaluateResources([]types.Resource{resource}); result.CompliantCount != 1 {
		t.Errorf("CompliantCount = %d, want 1", result.CompliantCount)
	}
}

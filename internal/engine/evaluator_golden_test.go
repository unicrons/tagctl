package engine

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/types"
	"github.com/unicrons/tagctl/test/testutil"
)

const violationsGolden = "evaluate-violations.golden.json"

func goldenPolicy() config.PolicyConfig {
	return config.PolicyConfig{
		Required: []config.TagRequirement{
			{Name: "environment", Values: []string{"dev", "staging", valueProd}},
			{Name: "owner", Pattern: "^.+@.+$"},
			{Name: "cost-center", Values: []string{"ENG-1234", "ops-1"}, Pattern: `^[A-Z]{2,4}-\d{3,6}$`},
		},
		Optional: []config.TagRequirement{
			{Name: "team", Values: []string{"backend", "platform"}},
			{Name: "project", Pattern: "^[a-z-]+$"},
		},
	}
}

func goldenResources() []types.Resource {
	resources := testutil.MustLoadResources("resources-aws.json")
	return append(resources,
		types.Resource{ID: "i-bad", Type: "aws_instance", Account: "production", Region: "eu-west-1", Provider: "aws", Tags: map[string]string{
			"environment": "production", "owner": "john", "cost-center": "ops-1", "team": "Backend", "project": "Tagctl",
		}},
		types.Resource{ID: "/aws/lambda/fn", Type: "aws_cloudwatch_log_group", Account: "staging", Region: "eu-west-1", Provider: "aws", Tags: map[string]string{"project": "billing"}},
		types.Resource{ID: "/aws/lambda/fn", Type: "aws_cloudwatch_log_group", Account: "staging", Region: "us-east-1", Provider: "aws", Tags: map[string]string{}},
	)
}

// goldenView is the part of the scan JSON whose content must not move for
// required tags: the violations list, its counts and the required tag stats.
func goldenView(result *types.ScanResult) ([]byte, error) {
	required := map[string]*types.TagStats{}
	for name, stats := range result.ByTag {
		if stats.Required {
			required[name] = stats
		}
	}
	return json.MarshalIndent(map[string]any{
		"total_resources":    result.TotalResources,
		"compliant_count":    result.CompliantCount,
		"violation_count":    result.ViolationCount,
		"compliance_percent": result.CompliancePct,
		"violations":         result.Violations,
		"required_by_tag":    required,
	}, "", "  ")
}

func TestEvaluateResources_ViolationsMatchGolden(t *testing.T) {
	evaluator, err := NewEvaluator(goldenPolicy())
	if err != nil {
		t.Fatal(err)
	}

	got, err := goldenView(evaluator.EvaluateResources(goldenResources()))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(testutil.FixturePath(violationsGolden))
	if err != nil {
		t.Fatal(err)
	}
	if string(got)+"\n" != string(want) {
		t.Errorf("scan violations drifted from %s:\n%s", violationsGolden, got)
	}
}

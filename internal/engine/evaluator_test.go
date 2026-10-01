package engine

import (
	"slices"
	"testing"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/types"
)

func failures(e *Evaluator, r types.Resource) []types.Finding {
	return slices.DeleteFunc(e.EvaluateResourceFindings(r), func(f types.Finding) bool {
		return f.Status != types.StatusFailed
	})
}

func TestNewEvaluator(t *testing.T) {
	policy := config.PolicyConfig{
		Required: []config.TagRequirement{
			{Name: "environment", Values: []string{"dev", "staging", valueProd}},
			{Name: "owner", Pattern: "^.+@.+$"},
		},
	}

	evaluator, err := NewEvaluator(policy)
	if err != nil {
		t.Fatalf("NewEvaluator() error = %v", err)
	}

	if evaluator == nil {
		t.Fatal("NewEvaluator() returned nil")
	}
}

func TestNewEvaluator_InvalidPattern(t *testing.T) {
	policy := config.PolicyConfig{
		Required: []config.TagRequirement{
			{Name: "test", Pattern: "[invalid"},
		},
	}

	_, err := NewEvaluator(policy)
	if err == nil {
		t.Fatal("NewEvaluator() should return error for invalid pattern")
	}
}

func TestNewEvaluator_EachRequirementKeepsItsPattern(t *testing.T) {
	evaluator, err := NewEvaluator(config.PolicyConfig{
		Required: []config.TagRequirement{{Name: "owner", Pattern: "^.+@.+$"}},
		Optional: []config.TagRequirement{{Name: "owner", Pattern: "^team-"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	findings := evaluator.EvaluateResourceFindings(types.Resource{ID: "i-1", Tags: map[string]string{"owner": "a@example.com"}})

	if len(findings) != 2 || findings[0].Status != types.StatusPass || findings[1].Reason != types.ReasonInvalidFormat {
		t.Errorf("findings = %+v, want the required pattern to pass and the optional one to fail", findings)
	}
}

func TestEvaluate_Compliant(t *testing.T) {
	policy := config.PolicyConfig{
		Required: []config.TagRequirement{
			{Name: "environment", Values: []string{"dev", "staging", valueProd}},
			{Name: "owner", Pattern: "^.+@.+$"},
		},
	}

	evaluator, _ := NewEvaluator(policy)

	resource := types.Resource{
		ID:   "i-123",
		Name: "test-instance",
		Tags: map[string]string{
			"environment": valueProd,
			"owner":       "team@company.com",
		},
	}

	if failed := failures(evaluator, resource); len(failed) != 0 {
		t.Errorf("failed findings = %d, want 0", len(failed))
	}
}

func TestEvaluate_MissingTag(t *testing.T) {
	policy := config.PolicyConfig{
		Required: []config.TagRequirement{
			{Name: "environment", Values: []string{"dev", "staging", valueProd}},
			{Name: "owner"},
		},
	}

	evaluator, _ := NewEvaluator(policy)

	resource := types.Resource{
		ID:   "i-123",
		Name: "test-instance",
		Tags: map[string]string{
			"environment": valueProd,
		},
	}

	failed := failures(evaluator, resource)
	if len(failed) != 1 {
		t.Fatalf("failed findings = %d, want 1", len(failed))
	}

	if failed[0].Tag != "owner" {
		t.Errorf("finding.Tag = %q, want %q", failed[0].Tag, "owner")
	}
	if failed[0].Reason != types.ReasonMissing {
		t.Errorf("finding.Reason = %q, want %q", failed[0].Reason, types.ReasonMissing)
	}
}

func TestEvaluate_InvalidValue(t *testing.T) {
	policy := config.PolicyConfig{
		Required: []config.TagRequirement{
			{Name: "environment", Values: []string{"dev", "staging", valueProd}},
		},
	}

	evaluator, _ := NewEvaluator(policy)

	resource := types.Resource{
		ID:   "i-123",
		Name: "test-instance",
		Tags: map[string]string{
			"environment": "invalid-value",
		},
	}

	failed := failures(evaluator, resource)
	if len(failed) != 1 {
		t.Fatalf("failed findings = %d, want 1", len(failed))
	}

	if failed[0].Reason != types.ReasonInvalidValue {
		t.Errorf("finding.Reason = %q, want %q", failed[0].Reason, types.ReasonInvalidValue)
	}
	if failed[0].Actual != "invalid-value" {
		t.Errorf("finding.Actual = %q, want %q", failed[0].Actual, "invalid-value")
	}
}

func TestEvaluate_InvalidFormat(t *testing.T) {
	policy := config.PolicyConfig{
		Required: []config.TagRequirement{
			{Name: "owner", Pattern: "^.+@company\\.com$"},
		},
	}

	evaluator, _ := NewEvaluator(policy)

	resource := types.Resource{
		ID:   "i-123",
		Name: "test-instance",
		Tags: map[string]string{
			"owner": "john",
		},
	}

	failed := failures(evaluator, resource)
	if len(failed) != 1 {
		t.Fatalf("failed findings = %d, want 1", len(failed))
	}

	if failed[0].Reason != types.ReasonInvalidFormat {
		t.Errorf("finding.Reason = %q, want %q", failed[0].Reason, types.ReasonInvalidFormat)
	}
}

func TestEvaluate_OptionalTag(t *testing.T) {
	policy := config.PolicyConfig{
		Required: []config.TagRequirement{
			{Name: "environment"},
		},
		Optional: []config.TagRequirement{
			{Name: "cost-center", Values: []string{"eng", "sales", "ops"}},
		},
	}

	evaluator, _ := NewEvaluator(policy)

	resource := types.Resource{
		ID:   "i-123",
		Name: "test-instance",
		Tags: map[string]string{
			"environment": valueProd,
		},
	}

	if findings := evaluator.EvaluateResourceFindings(resource); len(findings) != 1 {
		t.Errorf("findings = %+v, want only the required tag checked", findings)
	}

	resource.Tags["cost-center"] = "invalid"
	failed := failures(evaluator, resource)
	if len(failed) != 1 {
		t.Fatalf("failed findings = %d, want 1", len(failed))
	}
	if failed[0].Tag != "cost-center" {
		t.Errorf("finding.Tag = %q, want %q", failed[0].Tag, "cost-center")
	}
}

func TestEvaluateResources(t *testing.T) {
	policy := config.PolicyConfig{
		Required: []config.TagRequirement{
			{Name: "environment", Values: []string{"dev", "staging", valueProd}},
		},
	}

	evaluator, _ := NewEvaluator(policy)

	resources := []types.Resource{
		{
			ID:       "i-1",
			Name:     "compliant-instance",
			Account:  "account1",
			Provider: "aws",
			Tags:     map[string]string{"environment": valueProd},
		},
		{
			ID:       "i-2",
			Name:     "non-compliant-instance",
			Account:  "account1",
			Provider: "aws",
			Tags:     map[string]string{},
		},
		{
			ID:       "i-3",
			Name:     "invalid-value-instance",
			Account:  "account2",
			Provider: "aws",
			Tags:     map[string]string{"environment": "invalid"},
		},
	}

	result := evaluator.EvaluateResources(resources)

	if result.TotalResources != 3 {
		t.Errorf("TotalResources = %d, want 3", result.TotalResources)
	}
	if result.CompliantCount != 1 {
		t.Errorf("CompliantCount = %d, want 1", result.CompliantCount)
	}
	if result.ViolationCount != 2 {
		t.Errorf("ViolationCount = %d, want 2", result.ViolationCount)
	}

	// Check account stats
	if result.ByAccount["account1"] == nil {
		t.Fatal("ByAccount[account1] is nil")
	}
	if result.ByAccount["account1"].Total != 2 {
		t.Errorf("ByAccount[account1].Total = %d, want 2", result.ByAccount["account1"].Total)
	}

	// Check tag stats
	if result.ByTag["environment"] == nil {
		t.Fatal("ByTag[environment] is nil")
	}
	if result.ByTag["environment"].Present != 2 {
		t.Errorf("ByTag[environment].Present = %d, want 2", result.ByTag["environment"].Present)
	}
	if result.ByTag["environment"].Missing != 1 {
		t.Errorf("ByTag[environment].Missing = %d, want 1", result.ByTag["environment"].Missing)
	}
}

func TestEvaluateResources_OptionalTagStats(t *testing.T) {
	evaluator, err := NewEvaluator(config.PolicyConfig{
		Required: []config.TagRequirement{{Name: "owner"}},
		Optional: []config.TagRequirement{
			{Name: "team", Values: []string{"backend"}},
			{Name: "project"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	result := evaluator.EvaluateResources([]types.Resource{
		{ID: "i-1", Tags: map[string]string{"owner": "a", "team": "backend"}},
		{ID: "i-2", Tags: map[string]string{"owner": "b", "team": "Backend"}},
		{ID: "i-3", Tags: map[string]string{"owner": "c"}},
		{ID: "i-4", Tags: map[string]string{}},
	})

	want := map[string]types.TagStats{
		"owner":   {Tag: "owner", Required: true, Present: 3, Missing: 1, CompliancePct: 75},
		"team":    {Tag: "team", Present: 2, Invalid: 1, CompliancePct: 50},
		"project": {Tag: "project", CompliancePct: 100},
	}
	for tag, stats := range want {
		if got := *result.ByTag[tag]; got != stats {
			t.Errorf("ByTag[%s] = %+v, want %+v", tag, got, stats)
		}
	}
	if result.CompliantCount != 2 || result.ViolationCount != 2 {
		t.Errorf("compliant/violations = %d/%d, want 2/2", result.CompliantCount, result.ViolationCount)
	}
}

func TestEvaluateResources_ViolationsAreFailedFindings(t *testing.T) {
	evaluator, err := NewEvaluator(goldenPolicy())
	if err != nil {
		t.Fatal(err)
	}

	result := evaluator.EvaluateResources(goldenResources())

	failed := slices.DeleteFunc(slices.Clone(result.Findings), func(f types.Finding) bool {
		return f.Status != types.StatusFailed
	})
	if len(failed) != len(result.Violations) || result.ViolationCount != len(failed) {
		t.Fatalf("violations = %d, count = %d, failed findings = %d", len(result.Violations), result.ViolationCount, len(failed))
	}
	for i := range failed {
		if got := result.Violations[i].ToFinding(); got.Tag != failed[i].Tag || got.Reason != failed[i].Reason ||
			got.Resource.Identity() != failed[i].Resource.Identity() || got.Actual != failed[i].Actual || got.Expected != failed[i].Expected {
			t.Errorf("violation %d = %+v, want failed finding %+v", i, got, failed[i])
		}
	}
}

func TestIsCompliant(t *testing.T) {
	policy := config.PolicyConfig{
		Required: []config.TagRequirement{
			{Name: "environment"},
		},
	}

	evaluator, _ := NewEvaluator(policy)

	compliantResource := types.Resource{
		ID:   "i-1",
		Tags: map[string]string{"environment": valueProd},
	}

	nonCompliantResource := types.Resource{
		ID:   "i-2",
		Tags: map[string]string{},
	}

	if !evaluator.IsCompliant(compliantResource) {
		t.Error("IsCompliant() = false for compliant resource")
	}
	if evaluator.IsCompliant(nonCompliantResource) {
		t.Error("IsCompliant() = true for non-compliant resource")
	}
}

func TestFormatAllowedValues(t *testing.T) {
	tests := []struct {
		values   []string
		expected string
	}{
		{nil, ""},
		{[]string{}, ""},
		{[]string{"one"}, "one"},
		{[]string{"one", "two"}, "[one, two]"},
		{[]string{"a", "b", "c"}, "[a, b, c]"},
	}

	for _, tt := range tests {
		result := formatAllowedValues(tt.values)
		if result != tt.expected {
			t.Errorf("formatAllowedValues(%v) = %q, want %q", tt.values, result, tt.expected)
		}
	}
}

func TestEvaluate_FailuresCarryFailedStatus(t *testing.T) {
	policy := config.PolicyConfig{
		Required: []config.TagRequirement{
			{Name: "environment", Values: []string{"dev", "staging", valueProd}},
			{Name: "owner"},
		},
	}

	evaluator, _ := NewEvaluator(policy)

	findings := evaluator.EvaluateResourceFindings(types.Resource{ID: "i-123", Name: "test-instance", Tags: map[string]string{}})
	if len(findings) != 2 {
		t.Fatalf("findings = %d, want 2", len(findings))
	}

	for _, f := range findings {
		if f.Status != types.StatusFailed {
			t.Errorf("finding.Status = %q, want %q", f.Status, types.StatusFailed)
		}
	}
}

func TestEvaluateResourceFindings(t *testing.T) {
	policy := config.PolicyConfig{
		Required: []config.TagRequirement{
			{Name: "environment", Values: []string{"dev", "staging", valueProd}},
			{Name: "owner"},
		},
	}

	evaluator, _ := NewEvaluator(policy)

	// Resource with one compliant tag and one missing tag
	resource := types.Resource{
		ID:   "i-123",
		Name: "test-instance",
		Tags: map[string]string{
			"environment": valueProd,
			// Missing "owner" tag
		},
	}

	findings := evaluator.EvaluateResourceFindings(resource)
	if len(findings) != 2 {
		t.Fatalf("EvaluateResourceFindings() returned %d findings, want 2", len(findings))
	}

	// Check environment finding (should be PASS)
	var envFinding, ownerFinding *types.Finding
	for i := range findings {
		if findings[i].Tag == "environment" {
			envFinding = &findings[i]
		}
		if findings[i].Tag == "owner" {
			ownerFinding = &findings[i]
		}
	}

	if envFinding == nil {
		t.Fatal("environment finding not found")
	}
	if envFinding.Status != types.StatusPass {
		t.Errorf("environment finding Status = %q, want %q", envFinding.Status, types.StatusPass)
	}
	if envFinding.Reason != types.ReasonCompliant {
		t.Errorf("environment finding Reason = %q, want %q", envFinding.Reason, types.ReasonCompliant)
	}
	if envFinding.Actual != valueProd {
		t.Errorf("environment finding Actual = %q, want %q", envFinding.Actual, valueProd)
	}

	// Check owner finding (should be FAILED)
	if ownerFinding == nil {
		t.Fatal("owner finding not found")
	}
	if ownerFinding.Status != types.StatusFailed {
		t.Errorf("owner finding Status = %q, want %q", ownerFinding.Status, types.StatusFailed)
	}
	if ownerFinding.Reason != types.ReasonMissing {
		t.Errorf("owner finding Reason = %q, want %q", ownerFinding.Reason, types.ReasonMissing)
	}
}

func TestEvaluateResourceFindings_InvalidValue(t *testing.T) {
	policy := config.PolicyConfig{
		Required: []config.TagRequirement{
			{Name: "environment", Values: []string{"dev", "staging", valueProd}},
		},
	}

	evaluator, _ := NewEvaluator(policy)

	resource := types.Resource{
		ID:   "i-123",
		Name: "test-instance",
		Tags: map[string]string{
			"environment": "invalid-value",
		},
	}

	findings := evaluator.EvaluateResourceFindings(resource)
	if len(findings) != 1 {
		t.Fatalf("EvaluateResourceFindings() returned %d findings, want 1", len(findings))
	}

	if findings[0].Status != types.StatusFailed {
		t.Errorf("finding Status = %q, want %q", findings[0].Status, types.StatusFailed)
	}
	if findings[0].Reason != types.ReasonInvalidValue {
		t.Errorf("finding Reason = %q, want %q", findings[0].Reason, types.ReasonInvalidValue)
	}
}

func TestEvaluateResources_Findings(t *testing.T) {
	policy := config.PolicyConfig{
		Required: []config.TagRequirement{
			{Name: "environment", Values: []string{"dev", "staging", valueProd}},
		},
	}

	evaluator, _ := NewEvaluator(policy)

	resources := []types.Resource{
		{
			ID:       "i-1",
			Name:     "compliant-instance",
			Account:  "account1",
			Provider: "aws",
			Tags:     map[string]string{"environment": valueProd},
		},
		{
			ID:       "i-2",
			Name:     "non-compliant-instance",
			Account:  "account1",
			Provider: "aws",
			Tags:     map[string]string{},
		},
	}

	result := evaluator.EvaluateResources(resources)

	// Should have 2 findings (one PASS, one FAILED)
	if len(result.Findings) != 2 {
		t.Fatalf("EvaluateResources() returned %d findings, want 2", len(result.Findings))
	}

	passCount := 0
	failCount := 0
	for _, f := range result.Findings {
		switch f.Status {
		case types.StatusPass:
			passCount++
		case types.StatusFailed:
			failCount++
		}
	}

	if passCount != 1 {
		t.Errorf("PASS findings = %d, want 1", passCount)
	}
	if failCount != 1 {
		t.Errorf("FAILED findings = %d, want 1", failCount)
	}
}

// A log group name repeated in two regions is two resources. Keying compliance
// by bare ID collapsed them and reported phantom compliant resources.
func TestEvaluateResources_SameIDAcrossRegions(t *testing.T) {
	policy := config.PolicyConfig{
		Required: []config.TagRequirement{{Name: "owner"}},
	}
	evaluator, _ := NewEvaluator(policy)

	resources := []types.Resource{
		{ID: "/aws/lambda/fn", Type: "aws_cloudwatch_log_group", Account: "111", Region: "eu-west-1", Provider: "aws", Tags: map[string]string{}},
		{ID: "/aws/lambda/fn", Type: "aws_cloudwatch_log_group", Account: "111", Region: "us-east-1", Provider: "aws", Tags: map[string]string{}},
	}

	result := evaluator.EvaluateResources(resources)

	if result.TotalResources != 2 || result.CompliantCount != 0 {
		t.Errorf("total/compliant = %d/%d, want 2/0", result.TotalResources, result.CompliantCount)
	}
	if got := result.ByAccount["111"].Compliant; got != result.CompliantCount {
		t.Errorf("ByAccount compliant = %d, CompliantCount = %d; they must agree", got, result.CompliantCount)
	}
}

package cli

import (
	"testing"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/engine"
	"github.com/unicrons/tagctl/internal/terraform"
)

func TestTerraform_TagValueKnownAfterApplyPassesTheGate(t *testing.T) {
	const plan = `{
      "format_version": "1.2",
      "planned_values": {"root_module": {"resources": [
        {"address": "aws_vpc_security_group_ingress_rule.app", "mode": "managed",
         "type": "aws_vpc_security_group_ingress_rule", "name": "app",
         "values": {"tags": {"environment": "prod"}}}
      ]}},
      "resource_changes": [
        {"address": "aws_vpc_security_group_ingress_rule.app",
         "change": {"actions": ["create"], "after_unknown": {"tags": {"Name": true}, "tags_all": true}}}
      ]}`

	parsed, err := readTerraformResources(writeFixture(t, t.TempDir(), "plan.json", plan), terraform.Options{ChangedOnly: true})
	if err != nil {
		t.Fatalf("readTerraformResources() error = %v", err)
	}

	evaluator, err := engine.NewEvaluator(config.PolicyConfig{
		Required: []config.TagRequirement{
			{Name: "environment", Values: []string{valueProd}},
			{Name: "Name", Pattern: "^app-[0-9a-f]+$"},
		},
	})
	if err != nil {
		t.Fatalf("NewEvaluator() error = %v", err)
	}
	result := evaluator.EvaluateResources(parsed.Resources)

	gate := gateOptions{gate: readGateFlags(commandWithGateFlags(t, "--fail-under", "100")).gate}
	gateResult, err := gate.evaluate(result)
	if err != nil {
		t.Fatalf("evaluate() error = %v", err)
	}
	if !gateResult.Passed {
		t.Errorf("gate failed on a tag whose value is only known after apply: %v", gateResult.Reasons)
	}
}

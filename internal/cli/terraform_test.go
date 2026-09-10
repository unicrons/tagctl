package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/unicrons/tagctl/internal/terraform"
	"github.com/unicrons/tagctl/internal/types"
)

const terraformPlanFixture = `{
  "format_version": "1.2",
  "planned_values": {
    "root_module": {
      "resources": [
        {
          "address": "aws_instance.web",
          "mode": "managed",
          "type": "aws_instance",
          "name": "web",
          "provider_name": "registry.terraform.io/hashicorp/aws",
          "values": {"tags": {"environment": "prod"}}
        }
      ]
    }
  },
  "resource_changes": [
    {"address": "aws_instance.web", "change": {"actions": ["create"]}}
  ]
}`

func writeFixture(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestReadTerraformResources_FromFile(t *testing.T) {
	path := writeFixture(t, t.TempDir(), "plan.json", terraformPlanFixture)

	resources, err := readTerraformResources(path, terraform.Options{})
	if err != nil {
		t.Fatalf("readTerraformResources() error = %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(resources))
	}
	if resources[0].ID != "aws_instance.web" {
		t.Errorf("address = %q, want aws_instance.web", resources[0].ID)
	}
	if resources[0].Tags["environment"] != valueProd {
		t.Errorf("environment = %q, want prod", resources[0].Tags["environment"])
	}
}

func TestReadTerraformResources_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.json")

	if _, err := readTerraformResources(path, terraform.Options{}); err == nil {
		t.Error("readTerraformResources() on a missing file returned nil error")
	}
}

func TestReadTerraformResources_MalformedFile(t *testing.T) {
	path := writeFixture(t, t.TempDir(), "bad.json", "{not json")

	if _, err := readTerraformResources(path, terraform.Options{}); err == nil {
		t.Error("readTerraformResources() on malformed JSON returned nil error")
	}
}

func TestReadTerraformResources_ChangedOnly(t *testing.T) {
	const twoResources = `{
      "planned_values": {"root_module": {"resources": [
        {"address":"aws_instance.web","mode":"managed","type":"aws_instance","name":"web",
         "values":{"tags":{}}},
        {"address":"aws_instance.old","mode":"managed","type":"aws_instance","name":"old",
         "values":{"tags":{}}}
      ]}},
      "resource_changes": [
        {"address":"aws_instance.web","change":{"actions":["create"]}},
        {"address":"aws_instance.old","change":{"actions":["delete"]}}
      ]}`

	path := writeFixture(t, t.TempDir(), "plan.json", twoResources)

	all, err := readTerraformResources(path, terraform.Options{})
	if err != nil {
		t.Fatalf("readTerraformResources() error = %v", err)
	}
	if len(all) != 2 {
		t.Errorf("got %d resources without --changed-only, want 2", len(all))
	}

	changed, err := readTerraformResources(path, terraform.Options{ChangedOnly: true})
	if err != nil {
		t.Fatalf("readTerraformResources() error = %v", err)
	}
	if len(changed) != 1 || changed[0].ID != "aws_instance.web" {
		t.Errorf("changed-only returned %+v, want just the created resource", changed)
	}
}

func TestFailedFindings(t *testing.T) {
	result := &types.ScanResult{
		Findings: []types.Finding{
			{Resource: types.Resource{ID: "i-1"}, Tag: "owner", Status: types.StatusFailed},
			{Resource: types.Resource{ID: "i-2"}, Tag: "owner", Status: types.StatusPass},
			{Resource: types.Resource{ID: "i-3"}, Tag: "env", Status: types.StatusFailed},
		},
	}

	failures := failedFindings(result)

	if len(failures) != 2 {
		t.Fatalf("got %d failures, want 2", len(failures))
	}
	for _, finding := range failures {
		if finding.Status != types.StatusFailed {
			t.Errorf("finding for %s has status %q, want FAILED", finding.Resource.ID, finding.Status)
		}
	}
}

func TestFailedFindings_AllPassing(t *testing.T) {
	result := &types.ScanResult{
		Findings: []types.Finding{
			{Resource: types.Resource{ID: "i-1"}, Tag: "owner", Status: types.StatusPass},
		},
	}

	if failures := failedFindings(result); len(failures) != 0 {
		t.Errorf("got %d failures for an all-passing scan, want 0", len(failures))
	}
}

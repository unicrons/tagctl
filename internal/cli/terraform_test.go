package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unicrons/tagctl/internal/log"
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

	parsed, err := readTerraformResources(path, terraform.Options{})
	if err != nil {
		t.Fatalf("readTerraformResources() error = %v", err)
	}
	resources := parsed.Resources
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
      "format_version": "1.2",
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
	if len(all.Resources) != 2 {
		t.Errorf("got %d resources without --changed-only, want 2", len(all.Resources))
	}

	changed, err := readTerraformResources(path, terraform.Options{ChangedOnly: true})
	if err != nil {
		t.Fatalf("readTerraformResources() error = %v", err)
	}
	if len(changed.Resources) != 1 || changed.Resources[0].ID != "aws_instance.web" {
		t.Errorf("changed-only returned %+v, want just the created resource", changed.Resources)
	}
}

func TestLogUncheckedTags_NamesSkippedResourcesAndUncheckedKeys(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	logUncheckedTags(terraform.Result{
		Unreadable: []string{"aws_sqs_queue.jobs"},
		Resources: []types.Resource{
			{ID: "aws_instance.web", UnknownTags: []string{"Name", "ref"}},
			{ID: "aws_instance.known", Tags: map[string]string{"Name": "known"}},
		},
	})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("logged %d lines, want one per resource with unchecked tags:\n%s", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], "aws_sqs_queue.jobs") {
		t.Errorf("first line = %q, want the skipped address", lines[0])
	}
	if !strings.Contains(lines[1], "aws_instance.web") || !strings.Contains(lines[1], "Name, ref") {
		t.Errorf("second line = %q, want the address and its unchecked keys", lines[1])
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

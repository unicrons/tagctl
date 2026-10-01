package cli

import (
	"path/filepath"
	"testing"
)

func TestExecute_TerraformWithoutTaggableResourcesWritesReports(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", "policy:\n  required:\n    - name: owner\n")
	tfEmpty := writeFixture(t, dir, "empty.json", `{"format_version": "1.2", "planned_values": {"root_module": {}}}`)

	t.Run("to a file, without running the gate", func(t *testing.T) {
		sarif := filepath.Join(dir, "empty.sarif")

		run := execute(t, "-c", policy, "terraform", "--plan", tfEmpty, "--sarif", sarif, "--fail-under", "100")
		if run.err != nil {
			t.Fatalf("terraform error = %v", run.err)
		}

		got := readSARIF(t, sarif)
		if len(got.Results) != 0 {
			t.Errorf("got %d SARIF results for input with no taggable resources, want 0", len(got.Results))
		}
		if got.AutomationDetails == nil || got.AutomationDetails.ID != "tagctl/terraform/" {
			t.Errorf("automationDetails = %+v, want id tagctl/terraform/", got.AutomationDetails)
		}
	})

	t.Run("to stdout", func(t *testing.T) {
		run := execute(t, "-c", policy, "terraform", "--plan", tfEmpty, "--sarif", "-")
		if run.err != nil {
			t.Fatalf("terraform error = %v", run.err)
		}
		if !isSARIF(run.stdout) {
			t.Errorf("stdout is not a SARIF log:\n%s", run.stdout)
		}
	})
}

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/unicrons/tagctl/internal/report"
)

func readSARIF(t *testing.T, path string) report.SARIFRun {
	t.Helper()

	data, err := os.ReadFile(path) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatalf("reading SARIF: %v", err)
	}
	var log report.SARIFLog
	if err := json.Unmarshal(data, &log); err != nil {
		t.Fatalf("SARIF is not valid JSON: %v", err)
	}
	if len(log.Runs) != 1 {
		t.Fatalf("got %d SARIF runs, want 1", len(log.Runs))
	}
	return log.Runs[0]
}

func TestExecute_SARIFAnchorsToTheConfigFileRead(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	const policy = "policy:\n  required:\n    - name: owner\n"
	writeFixture(t, dir, "tagctl.yaml", policy)
	if err := os.Mkdir(filepath.Join(dir, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	configured := writeFixture(t, filepath.Join(dir, "config"), "prod.yaml", policy)
	resources := writeFixture(t, dir, "resources.json", `[{"id": "i-1", "type": "aws_instance", "provider": "aws", "tags": {}}]`)
	tfPlan := writeFixture(t, dir, "plan.json", terraformPlanFixture)
	sarif := filepath.Join(dir, "out.sarif")

	tests := []struct {
		name        string
		args        []string
		wantURI     string
		wantRunID   string
		wantResults bool
	}{
		{name: "scan with a discovered config", args: []string{"scan", "--mock"}, wantURI: "tagctl.yaml", wantRunID: "tagctl/scan/"},
		{name: "scan with an absolute --config", args: []string{"-c", configured, "scan", "--mock"}, wantURI: "config/prod.yaml", wantRunID: "tagctl/scan/"},
		{name: "terraform with an absolute --config", args: []string{"-c", configured, "terraform", "--plan", tfPlan}, wantURI: "config/prod.yaml", wantRunID: "tagctl/terraform/"},
		{name: "evaluate with an absolute --policy", args: []string{"evaluate", "--resources", resources, "--policy", configured}, wantURI: "config/prod.yaml", wantRunID: "tagctl/evaluate/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := execute(t, append(tt.args, "--sarif", sarif)...)
			if code := ExitCode(run.err); code != exitOK {
				t.Fatalf("tagctl %v exited %d: %v", tt.args, code, run.err)
			}

			got := readSARIF(t, sarif)
			if got.AutomationDetails == nil || got.AutomationDetails.ID != tt.wantRunID {
				t.Errorf("automationDetails = %+v, want id %q", got.AutomationDetails, tt.wantRunID)
			}
			if len(got.Invocations) != 1 || !got.Invocations[0].ExecutionSuccessful {
				t.Errorf("invocations = %+v, want one successful invocation", got.Invocations)
			}
			if len(got.Results) == 0 {
				t.Fatal("no SARIF results to check the anchor of")
			}
			for _, result := range got.Results {
				if uri := result.Locations[0].PhysicalLocation.ArtifactLocation.URI; uri != tt.wantURI {
					t.Errorf("artifact URI = %q, want %q", uri, tt.wantURI)
				}
			}
		})
	}
}

func TestExecute_SARIFAnchorsToADiscoveredYMLConfig(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	writeFixture(t, dir, "tagctl.yml", "policy:\n  required:\n    - name: owner\n")
	sarif := filepath.Join(dir, "out.sarif")

	run := execute(t, "scan", "--mock", "--sarif", sarif)
	if run.err != nil {
		t.Fatalf("scan error = %v", run.err)
	}

	got := readSARIF(t, sarif)
	if len(got.Results) == 0 {
		t.Fatal("no SARIF results to check the anchor of")
	}
	if uri := got.Results[0].Locations[0].PhysicalLocation.ArtifactLocation.URI; uri != "tagctl.yml" {
		t.Errorf("artifact URI = %q, want tagctl.yml", uri)
	}
}

func TestRunScan_PartialScanSARIFIsNotSuccessful(t *testing.T) {
	sarif := filepath.Join(t.TempDir(), "partial.sarif")

	if _, err := runPartialScan(t, map[string]string{"sarif": sarif, "allow-partial": "true"}); err != nil {
		t.Fatalf("runScan() error = %v", err)
	}

	got := readSARIF(t, sarif)
	if len(got.Invocations) != 1 {
		t.Fatalf("got %d invocations, want 1", len(got.Invocations))
	}
	invocation := got.Invocations[0]
	if invocation.ExecutionSuccessful {
		t.Error("executionSuccessful = true for a partial scan")
	}
	if len(invocation.ToolExecutionNotifications) != 1 {
		t.Fatalf("notifications = %+v, want the one discovery error", invocation.ToolExecutionNotifications)
	}
}

package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unicrons/tagctl/internal/types"
)

func isJSON(stdout string) bool { return json.Valid([]byte(stdout)) }

func isSARIF(stdout string) bool {
	var sarif struct {
		Version string `json:"version"`
	}
	return json.Unmarshal([]byte(stdout), &sarif) == nil && sarif.Version == "2.1.0"
}

func isFindingsCSV(stdout string) bool {
	return strings.HasPrefix(stdout, strings.Join(csvHeader, ",")+"\n")
}

func TestExecute_StdoutCarriesOnlyTheSelectedOutput(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", "policy:\n  required:\n    - name: environment\n")
	resources := writeFixture(t, dir, "resources.json", `[
		{"id": "i-1", "type": "aws_instance", "provider": "aws", "tags": {"environment": "prod"}},
		{"id": "i-2", "type": "aws_instance", "provider": "aws", "tags": {"environment": "Prod"}}
	]`)
	tfPlan := writeFixture(t, dir, "plan.json", terraformPlanFixture)
	tfEmpty := writeFixture(t, dir, "empty.json", `{"format_version": "1.2", "planned_values": {"root_module": {}}}`)
	baseline := writeScan(t, dir, "baseline.json", failedScan(0))
	normalizePlan := filepath.Join(dir, "normalize-plan.json")

	tests := []struct {
		name  string
		args  []string
		valid func(string) bool
	}{
		{name: "scan -o json", args: []string{"scan", "--mock", "-o", "json"}, valid: isJSON},
		{name: "scan -o JSON", args: []string{"scan", "--mock", "-o", "JSON"}, valid: isJSON},
		{name: "scan -o csv", args: []string{"scan", "--mock", "-o", "csv"}, valid: isFindingsCSV},
		{name: "scan --sarif -", args: []string{"scan", "--mock", "--sarif", "-"}, valid: isSARIF},
		{name: "plan -o json", args: []string{"plan", "-o", "json"}, valid: isJSON},
		{name: "diff -o json", args: []string{"diff", baseline, baseline, "-o", "json"}, valid: isJSON},
		{name: "normalize -o json --out", args: []string{"normalize", "--resources", resources, "-o", "json", "--out", normalizePlan}, valid: isJSON},
		{name: "terraform -o json", args: []string{"terraform", "--plan", tfPlan, "-o", "json"}, valid: isJSON},
		{name: "terraform -o json without taggable resources", args: []string{"terraform", "--plan", tfEmpty, "-o", "json"}, valid: isJSON},
		{name: "evaluate", args: []string{"evaluate", "--resources", resources, "--policy", policy}, valid: isJSON},
		{name: "evaluate --sarif -", args: []string{"evaluate", "--resources", resources, "--policy", policy, "--sarif", "-"}, valid: isSARIF},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := execute(t, append([]string{"-c", policy}, tt.args...)...)

			if run.err != nil {
				t.Fatalf("tagctl %v error = %v", tt.args, run.err)
			}
			if !tt.valid(run.stdout) {
				t.Errorf("tagctl %v stdout is not the selected output alone:\n%s", tt.args, run.stdout)
			}
		})
	}
}

func TestExecute_NoticesGoToStderr(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", "policy:\n  required:\n    - name: owner\n")

	run := execute(t, "-c", policy, "scan", "--mock")

	if run.err != nil {
		t.Fatalf("scan error = %v", run.err)
	}
	if !strings.Contains(run.stdout, "Tag Compliance Report") {
		t.Errorf("stdout lacks the table:\n%s", run.stdout)
	}
	for _, notice := range []string{"Audit, fix, and enforce cloud resource tags", "Detailed results saved to"} {
		if strings.Contains(run.stdout, notice) || !strings.Contains(run.stderr, notice) {
			t.Errorf("%q is not on stderr only", notice)
		}
	}
}

func TestExecute_RejectsUnusableOutput(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", "policy:\n  required:\n    - name: owner\n")
	resources := writeFixture(t, dir, "resources.json", `[{"id": "i-1", "type": "aws_instance", "tags": {}}]`)
	scan := writeScan(t, dir, "scan.json", &types.ScanResult{})

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "scan -o yaml", args: []string{"scan", "--mock", "-o", "yaml"}, wantErr: `unsupported output format "yaml" for scan`},
		{name: "plan -o csv", args: []string{"plan", "-o", "csv"}, wantErr: `unsupported output format "csv" for plan`},
		{name: "diff -o csv", args: []string{"diff", scan, scan, "-o", "csv"}, wantErr: `unsupported output format "csv" for diff`},
		{name: "cost -o yaml", args: []string{"cost", "-o", "yaml"}, wantErr: `unsupported output format "yaml" for cost`},
		{name: "evaluate -o table", args: []string{"evaluate", "--resources", resources, "--policy", policy, "-o", "table"}, wantErr: `unsupported output format "table" for evaluate`},
		{name: "version -o json", args: []string{"version", "-o", "json"}, wantErr: `unsupported output format "json" for version`},
		{name: "two reports on stdout", args: []string{"scan", "--mock", "--sarif", "-", "--junit", "-"}, wantErr: "--sarif and --junit both write to stdout"},
		{name: "report and -o on stdout", args: []string{"scan", "--mock", "-o", "json", "--ocsf", "-"}, wantErr: "--ocsf - replaces the json output on stdout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := execute(t, append([]string{"-c", policy}, tt.args...)...)

			if run.err == nil || !strings.Contains(run.err.Error(), tt.wantErr) {
				t.Fatalf("tagctl %v error = %v, want one containing %q", tt.args, run.err, tt.wantErr)
			}
			if code := ExitCode(run.err); code != exitError {
				t.Errorf("tagctl %v exited %d, want %d", tt.args, code, exitError)
			}
			if run.stdout != "" {
				t.Errorf("tagctl %v wrote to stdout before rejecting the flags:\n%s", tt.args, run.stdout)
			}
		})
	}
}

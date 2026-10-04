package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const demoPolicy = "policy:\n  required:\n    - name: owner\n"

func fileNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestScan_OutputDirReceivesTheReports(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", demoPolicy)
	reports := filepath.Join(dir, "nested", "reports")

	run := execute(t, "-c", policy, "scan", "--mock", "--output-dir", reports)
	if run.err != nil {
		t.Fatalf("scan error = %v", run.err)
	}

	names := fileNames(t, reports)
	if len(names) != 3 {
		t.Fatalf("files in --output-dir = %v, want the JSON, CSV and HTML reports", names)
	}
	for _, name := range names {
		info, err := os.Stat(filepath.Join(reports, name))
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s has mode %o, want 600", name, perm)
		}
	}
	if left := fileNames(t, OutputDir); len(left) != 0 {
		t.Errorf("default output directory got %v, want nothing", left)
	}
	if !strings.Contains(run.stderr, reports) {
		t.Errorf("stderr does not name the report directory:\n%s", run.stderr)
	}
}

func TestScan_NoFilesWritesOnlyTheRequestedReports(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", demoPolicy)
	sarif := filepath.Join(dir, "out.sarif")

	run := execute(t, "-c", policy, "scan", "--mock", "--no-files", "--sarif", sarif)
	if run.err != nil {
		t.Fatalf("scan error = %v", run.err)
	}

	if left := fileNames(t, OutputDir); len(left) != 0 {
		t.Errorf("output directory got %v, want nothing with --no-files", left)
	}
	if strings.Contains(run.stderr, "Detailed results saved to") {
		t.Errorf("stderr announces report files that were not written:\n%s", run.stderr)
	}
	if !strings.Contains(run.stdout, "Tag Compliance Report") {
		t.Errorf("stdout lacks the table:\n%s", run.stdout)
	}
	if _, err := os.Stat(sarif); err != nil {
		t.Errorf("--sarif report not written with --no-files: %v", err)
	}
}

func TestOutputDirFlag_UsageErrors(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", demoPolicy)

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "scan --no-files with --output-dir", args: []string{"scan", "--mock", "--no-files", "--output-dir", dir}, wantErr: "none of the others can be"},
		{name: "scan with an empty directory", args: []string{"scan", "--mock", "--output-dir", ""}, wantErr: "--output-dir needs a directory"},
		{name: "plan with an empty directory", args: []string{"plan", "--output-dir", " "}, wantErr: "--output-dir needs a directory"},
		{name: "apply with an empty directory", args: []string{"apply", "--output-dir", ""}, wantErr: "--output-dir needs a directory"},
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
		})
	}
}

func TestPlan_OutputDirSuppliesTheScanAndReceivesThePlan(t *testing.T) {
	dir := t.TempDir()
	config := writeFixture(t, dir, "tagctl.yaml",
		"clouds:\n  aws:\n    - profile: default\n      regions: [us-east-1]\n"+demoPolicy+
			"rules:\n  defaults:\n    - resource: \"*\"\n      when:\n        \"tag:owner\": absent\n      set:\n        owner: platform@example.com\n")
	reports := filepath.Join(dir, "reports")
	if err := os.Mkdir(reports, 0o750); err != nil {
		t.Fatal(err)
	}
	writeScan(t, reports, "scan-20260101-100000.json", failedScan(0))

	run := execute(t, "-c", config, "plan", "--output-dir", reports, "-o", "json")
	if run.err != nil {
		t.Fatalf("plan error = %v", run.err)
	}

	plan, err := FindLatestPlanInDir(reports)
	if err != nil {
		t.Fatalf("no plan written to --output-dir: %v", err)
	}
	loaded, err := loadPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Changes) != 1 || loaded.Changes[0].NewValue != "platform@example.com" {
		t.Errorf("plan changes = %+v, want the default owner for the scan found in --output-dir", loaded.Changes)
	}
	if left := fileNames(t, OutputDir); len(left) != 0 {
		t.Errorf("default output directory got %v, want nothing", left)
	}
}

func TestPlan_OutputDirWithoutScansFails(t *testing.T) {
	dir := t.TempDir()
	config := writeFixture(t, dir, "tagctl.yaml", "clouds:\n  aws:\n    - profile: default\n      regions: [us-east-1]\n"+demoPolicy)

	run := execute(t, "-c", config, "plan", "--output-dir", filepath.Join(dir, "empty"))

	if run.err == nil || !strings.Contains(run.err.Error(), filepath.Join(dir, "empty")) {
		t.Fatalf("plan error = %v, want one naming the directory it searched", run.err)
	}
}

func TestApply_OutputDirSuppliesTheLatestPlan(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", demoPolicy)
	reports := filepath.Join(dir, "reports")
	if err := os.Mkdir(reports, 0o750); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(getMockPlan())
	if err != nil {
		t.Fatal(err)
	}
	plan := writeFixture(t, reports, "plan-20260101-100000.json", string(data))

	run := execute(t, "-c", policy, "apply", "--mock", "--auto-approve", "--output-dir", reports)
	if run.err != nil {
		t.Fatalf("apply error = %v", run.err)
	}
	if !strings.Contains(run.stdout+run.stderr, plan) {
		t.Errorf("apply did not load %s:\n%s\n%s", plan, run.stdout, run.stderr)
	}
}

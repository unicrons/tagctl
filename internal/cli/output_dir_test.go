package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/unicrons/tagctl/internal/demo"
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
		{name: "diff with an empty directory", args: []string{"diff", "--output-dir", ""}, wantErr: "--output-dir needs a directory"},
		{name: "normalize with an empty directory", args: []string{"normalize", "--output-dir", " "}, wantErr: "--output-dir needs a directory"},
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
	data, err := json.Marshal(demo.Plan())
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

// scansIn writes an older and a newer scan into a new reports directory under dir.
func scansIn(t *testing.T, dir string) (reports, older, newer string) {
	t.Helper()

	reports = filepath.Join(dir, "reports")
	if err := os.Mkdir(reports, 0o750); err != nil {
		t.Fatal(err)
	}
	older = writeScan(t, reports, scanJan, failedScan(0))
	newer = writeScan(t, reports, scanFeb, failedScan(0))

	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, path := range []string{older, newer} {
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		stamp = stamp.AddDate(0, 1, 0)
	}
	return reports, older, newer
}

func TestDiffAndNormalize_OutputDirSuppliesTheLatestScans(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", demoPolicy)
	reports, older, newer := scansIn(t, dir)
	explicit := writeScan(t, dir, "explicit.json", failedScan(0))
	other := writeScan(t, dir, "other.json", failedScan(0))
	missing := filepath.Join(dir, "missing")

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "diff compares the two most recent scans in the directory",
			args: []string{"diff", "--output-dir", reports},
			want: []string{"Baseline: " + older, "Current:  " + newer},
		},
		{
			name: "diff with a baseline takes the latest scan from the directory",
			args: []string{"diff", explicit, "--output-dir", reports},
			want: []string{"Baseline: " + explicit, "Current:  " + newer},
		},
		{
			name: "diff with two files never reads the directory",
			args: []string{"diff", explicit, other, "--output-dir", missing},
			want: []string{"Baseline: " + explicit, "Current:  " + other},
		},
		{
			name: "normalize reads the latest scan in the directory",
			args: []string{"normalize", "--output-dir", reports},
			want: []string{"Source: " + newer},
		},
		{
			name: "normalize --scan never reads the directory",
			args: []string{"normalize", "--scan", explicit, "--output-dir", missing},
			want: []string{"Source: " + explicit},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := execute(t, append([]string{"-c", policy}, tt.args...)...)
			if run.err != nil {
				t.Fatalf("tagctl %v error = %v", tt.args, run.err)
			}
			for _, want := range tt.want {
				if !strings.Contains(run.stdout, want) {
					t.Errorf("stdout lacks %q:\n%s", want, run.stdout)
				}
			}
		})
	}
}

func TestDiffAndNormalize_OutputDirWithoutScansFails(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", demoPolicy)
	missing := filepath.Join(dir, "missing")
	single := filepath.Join(dir, "single")
	if err := os.Mkdir(single, 0o750); err != nil {
		t.Fatal(err)
	}
	baseline := writeScan(t, single, scanJan, failedScan(0))

	tests := []struct {
		name    string
		args    []string
		wantErr []string
	}{
		{name: "diff in a missing directory", args: []string{"diff", "--output-dir", missing}, wantErr: []string{"output directory " + missing + " not found"}},
		{name: "diff with a baseline in a missing directory", args: []string{"diff", baseline, "--output-dir", missing}, wantErr: []string{"output directory " + missing + " not found"}},
		{name: "diff in a directory with one scan", args: []string{"diff", "--output-dir", single}, wantErr: []string{"need two scans to compare, found 1 in " + single}},
		{name: "normalize in a missing directory", args: []string{"normalize", "--output-dir", missing}, wantErr: []string{"output directory " + missing + " not found"}},
		{name: "normalize in a directory without scans", args: []string{"normalize", "--output-dir", dir}, wantErr: []string{"no scan files found in " + dir}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := execute(t, append([]string{"-c", policy}, tt.args...)...)

			for _, want := range tt.wantErr {
				if run.err == nil || !strings.Contains(run.err.Error(), want) {
					t.Fatalf("tagctl %v error = %v, want one containing %q", tt.args, run.err, want)
				}
			}
			if code := ExitCode(run.err); code != exitError {
				t.Errorf("tagctl %v exited %d, want %d", tt.args, code, exitError)
			}
		})
	}
}

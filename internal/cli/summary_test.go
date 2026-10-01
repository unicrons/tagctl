package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const summaryHeading = "## Tag compliance\n"

func TestSummary_EveryGateCommandWritesIt(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", demoPolicy)
	resources := writeFixture(t, dir, "resources.json", `[{"id": "i-1", "type": "aws_instance", "provider": "aws", "tags": {}}]`)
	tfPlan := writeFixture(t, dir, "plan.json", terraformPlanFixture)

	tests := []struct {
		name string
		args []string
	}{
		{name: "scan", args: []string{"scan", "--mock"}},
		{name: "evaluate", args: []string{"evaluate", "--resources", resources, "--policy", policy}},
		{name: "terraform", args: []string{"terraform", "--plan", tfPlan}},
	}

	for _, tt := range tests {
		t.Run(tt.name+" to a file", func(t *testing.T) {
			summary := filepath.Join(t.TempDir(), "summary.md")

			run := execute(t, append(append([]string{"-c", policy}, tt.args...), "--summary", summary)...)
			if run.err != nil {
				t.Fatalf("tagctl %v error = %v", tt.args, run.err)
			}

			data, err := os.ReadFile(summary) // #nosec G304 -- test temp dir
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(data), summaryHeading) || !strings.Contains(string(data), "### Failed findings") {
				t.Errorf("summary file is not the Markdown summary:\n%s", data)
			}
			if run.stdout == "" || strings.Contains(run.stdout, summaryHeading) {
				t.Errorf("stdout should keep the command's own output:\n%s", run.stdout)
			}
		})

		t.Run(tt.name+" to stdout", func(t *testing.T) {
			run := execute(t, append(append([]string{"-c", policy}, tt.args...), "--summary", "-")...)
			if run.err != nil {
				t.Fatalf("tagctl %v error = %v", tt.args, run.err)
			}
			if !strings.HasPrefix(run.stdout, summaryHeading) || !strings.HasSuffix(run.stdout, "</sub>\n") {
				t.Errorf("stdout is not the summary alone:\n%s", run.stdout)
			}
		})
	}
}

func TestSummary_SharesStdoutWithNothingElse(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", demoPolicy)

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "another report on stdout", args: []string{"--sarif", "-", "--summary", "-"}, wantErr: "--sarif and --summary both write to stdout"},
		{name: "an explicit -o", args: []string{"-o", "json", "--summary", "-"}, wantErr: "--summary - replaces the json output on stdout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := execute(t, append([]string{"-c", policy, "scan", "--mock"}, tt.args...)...)

			if run.err == nil || !strings.Contains(run.err.Error(), tt.wantErr) {
				t.Fatalf("scan error = %v, want one containing %q", run.err, tt.wantErr)
			}
			if code := ExitCode(run.err); code != exitError {
				t.Errorf("exit code = %d, want %d", code, exitError)
			}
			if run.stdout != "" {
				t.Errorf("stdout written before rejecting the flags:\n%s", run.stdout)
			}
		})
	}
}

func TestSummary_IsWrittenBeforeTheGateFails(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", demoPolicy)
	summary := filepath.Join(dir, "summary.md")

	run := execute(t, "-c", policy, "scan", "--mock", "--fail-under", "100", "--summary", summary)

	if code := ExitCode(run.err); code != exitGateFailed {
		t.Fatalf("exit code = %d (%v), want %d", code, run.err, exitGateFailed)
	}
	if _, err := os.Stat(summary); err != nil {
		t.Errorf("summary not written when the gate fails: %v", err)
	}
}

func TestSummary_StepSummaryEnvironmentIsNotUsedImplicitly(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", demoPolicy)
	stepSummary := filepath.Join(dir, "step-summary.md")
	t.Setenv("GITHUB_STEP_SUMMARY", stepSummary)

	run := execute(t, "-c", policy, "scan", "--mock")
	if run.err != nil {
		t.Fatalf("scan error = %v", run.err)
	}

	if _, err := os.Stat(stepSummary); !os.IsNotExist(err) {
		t.Errorf("scan wrote $GITHUB_STEP_SUMMARY without --summary (stat error = %v)", err)
	}
}

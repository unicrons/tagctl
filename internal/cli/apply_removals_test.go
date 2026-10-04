package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/unicrons/tagctl/internal/types"
)

func writeRenamePlan(t *testing.T, dir string, changes ...types.TagChange) string {
	t.Helper()
	data, err := json.Marshal(types.Plan{ID: "plan-1", Changes: changes})
	if err != nil {
		t.Fatal(err)
	}
	return writeFixture(t, dir, "plan.json", string(data))
}

func renameChanges() []types.TagChange {
	instance := types.Resource{ID: "i-1", ARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-1", Provider: "aws", Type: "aws_instance", Region: "us-east-1"}
	return []types.TagChange{
		{Resource: instance, Tag: "environment", Action: types.ActionAdd, NewValue: "prod", Reason: types.ReasonRenamed},
		{Resource: instance, Tag: "Env", Action: types.ActionRemove, OldValue: "prod", Reason: types.ReasonRenamed},
		{Resource: instance, Tag: "temp", Action: types.ActionRemove, OldValue: "1", Reason: types.ReasonForbiddenTag},
	}
}

func TestApply_SkipsRemovalsWithoutAllowRemovals(t *testing.T) {
	dir := t.TempDir()
	config := writeFixture(t, dir, "tagctl.yaml", "policy:\n  required:\n    - name: environment\n")
	plan := writeRenamePlan(t, dir, renameChanges()...)

	run := execute(t, "apply", "--config", config, "--plan", plan, "--mock", "--auto-approve")

	if run.err != nil || ExitCode(run.err) != 0 {
		t.Fatalf("apply error = %v, want success: skipped removals do not fail the run", run.err)
	}
	if !strings.Contains(run.stderr, "2 removals will be SKIPPED (pass --allow-removals to perform them)") {
		t.Errorf("stderr does not announce the skipped removals:\n%s", run.stderr)
	}
	for _, want := range []string{
		"[1/1] aws_instance.i-1 (environment: prod)",
		"Applied successfully: 1 changes",
		"Errors: 0",
		"Skipped: 2 removal(s), no tag was removed. Run again with --allow-removals to perform them.",
	} {
		if !strings.Contains(run.stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, run.stdout)
		}
	}
	if strings.Contains(run.stdout, "remove Env") || strings.Contains(run.stdout, "remove temp") {
		t.Errorf("a removal was applied without --allow-removals:\n%s", run.stdout)
	}
}

func TestApply_PerformsRemovalsWithAllowRemovals(t *testing.T) {
	dir := t.TempDir()
	config := writeFixture(t, dir, "tagctl.yaml", "policy:\n  required:\n    - name: environment\n")
	plan := writeRenamePlan(t, dir, renameChanges()...)

	run := execute(t, "apply", "--config", config, "--plan", plan, "--mock", "--auto-approve", "--allow-removals")

	if run.err != nil {
		t.Fatalf("apply error = %v", run.err)
	}
	if !strings.Contains(run.stderr, "2 tags will be REMOVED:") {
		t.Errorf("stderr does not list the removals:\n%s", run.stderr)
	}
	for _, want := range []string{"(remove Env)", "(remove temp)", "Applied successfully: 3 changes"} {
		if !strings.Contains(run.stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, run.stdout)
		}
	}
	if strings.Contains(run.stdout, "Skipped:") {
		t.Errorf("stdout reports skipped removals with --allow-removals:\n%s", run.stdout)
	}
}

func TestApply_PlanWithOnlyRemovalsDoesNothingWithoutAllowRemovals(t *testing.T) {
	dir := t.TempDir()
	config := writeFixture(t, dir, "tagctl.yaml", "policy:\n  required:\n    - name: environment\n")
	plan := writeRenamePlan(t, dir, renameChanges()[1:]...)

	run := execute(t, "apply", "--config", config, "--plan", plan, "--mock")

	if run.err != nil {
		t.Fatalf("apply error = %v, want success without a prompt", run.err)
	}
	if strings.Contains(run.stderr, "Do you want to apply") || strings.Contains(run.stdout, "Applied successfully") {
		t.Errorf("apply prompted or ran with nothing to apply:\nstdout: %s\nstderr: %s", run.stdout, run.stderr)
	}
	if !strings.Contains(run.stdout, "Skipped: 2 removal(s)") {
		t.Errorf("stdout does not report the skipped removals:\n%s", run.stdout)
	}
}

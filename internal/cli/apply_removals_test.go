package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

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
	for _, want := range []string{
		"  • 1 resources will be modified\n",
		"  • 1 tags will be added\n",
		"  • 0 tags will be updated\n  • 2 removals will be SKIPPED (pass --allow-removals to perform them)\n\n",
	} {
		if !strings.Contains(run.stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, run.stderr)
		}
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
	if !strings.Contains(run.stderr, "  • 0 tags will be updated\n  • 2 tags will be REMOVED:\n") {
		t.Errorf("stderr does not list the removals after the counts:\n%s", run.stderr)
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

func TestReviewChanges_ShowsARemovalAsARemoval(t *testing.T) {
	var out bytes.Buffer

	if _, err := reviewChanges(context.Background(), &out, strings.NewReader("y\n"), &types.Plan{Changes: renameChanges()}); err != nil {
		t.Fatal(err)
	}

	if want := "  + environment: \"prod\"\n  - Env: \"prod\"\n  - temp: \"1\"\n"; !strings.Contains(out.String(), want) {
		t.Errorf("review output lacks %q:\n%s", want, out.String())
	}
	if strings.Contains(out.String(), "+ Env") || strings.Contains(out.String(), "+ temp") {
		t.Errorf("review shows a removal as an addition:\n%s", out.String())
	}
}

func TestApplyInteractive_ReviewsRemovalsOnlyWithAllowRemovals(t *testing.T) {
	dir := t.TempDir()
	config := writeFixture(t, dir, "tagctl.yaml", "policy:\n  required:\n    - name: environment\n")
	plan := writeRenamePlan(t, dir, renameChanges()...)

	t.Run("without the flag", func(t *testing.T) {
		answerApplyPrompts(t, "y\n", true)

		run := execute(t, "apply", "--config", config, "--plan", plan, "--mock", "--interactive")

		if run.err != nil {
			t.Fatalf("apply error = %v\n%s", run.err, run.stderr)
		}
		if strings.Contains(run.stderr, "Env") || strings.Contains(run.stderr, "temp") {
			t.Errorf("the review offers a removal without --allow-removals:\n%s", run.stderr)
		}
		if strings.Contains(run.stdout, "remove ") || !strings.Contains(run.stdout, "Applied successfully: 1 changes") {
			t.Errorf("stdout = %q, want only the addition applied", run.stdout)
		}
		if !strings.Contains(run.stdout, "Skipped: 2 removal(s)") {
			t.Errorf("stdout does not report the skipped removals:\n%s", run.stdout)
		}
	})

	t.Run("with the flag", func(t *testing.T) {
		answerApplyPrompts(t, "y\n", true)

		run := execute(t, "apply", "--config", config, "--plan", plan, "--mock", "--interactive", "--allow-removals")

		if run.err != nil {
			t.Fatalf("apply error = %v\n%s", run.err, run.stderr)
		}
		if want := "  + environment: \"prod\"\n  - Env: \"prod\"\n  - temp: \"1\"\nApply these changes?"; !strings.Contains(run.stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, run.stderr)
		}
		for _, want := range []string{"(remove Env)", "(remove temp)", "Applied successfully: 3 changes"} {
			if !strings.Contains(run.stdout, want) {
				t.Errorf("stdout lacks %q:\n%s", want, run.stdout)
			}
		}
	})
}

const kubernetesRenameConfig = `clouds:
  kubernetes:
    - name: prod
      context: prod
      namespaces: [app]
      resource_types: [k8s_deployment]
policy:
  required:
    - name: environment
  forbidden:
    - name: temp
rules:
  rename:
    - from: Env
      to: environment
`

func labelsAfterKubernetesApply(t *testing.T, flags map[string]string) map[string]string {
	t.Helper()
	prod := fake.NewSimpleClientset(appDeployment("api", map[string]string{"Env": "prod", "temp": "1"}))
	useFakeClusters(t, map[string]*fake.Clientset{"prod": prod})
	useKubernetesConfig(t, kubernetesRenameConfig)

	if err := runScan(scanCmd, nil); err != nil {
		t.Fatalf("runScan() error = %v", err)
	}
	if err := runPlan(planCmd, nil); err != nil {
		t.Fatalf("runPlan() error = %v", err)
	}
	setFlags(t, applyCmd, flags)
	if err := runApply(applyCmd, nil); err != nil {
		t.Fatalf("runApply() error = %v", err)
	}

	deployment, err := prod.AppsV1().Deployments("app").Get(context.Background(), "api", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return deployment.Labels
}

func TestScanPlanApply_KubernetesKeepsRemovedLabelsWithoutAllowRemovals(t *testing.T) {
	got := labelsAfterKubernetesApply(t, map[string]string{"auto-approve": "true"})

	if want := map[string]string{"Env": "prod", "temp": "1", "environment": "prod"}; !maps.Equal(got, want) {
		t.Errorf("labels = %v, want %v", got, want)
	}
}

func TestScanPlanApply_KubernetesRemovesLabelsWithAllowRemovals(t *testing.T) {
	got := labelsAfterKubernetesApply(t, map[string]string{"auto-approve": "true", "allow-removals": "true"})

	if want := map[string]string{"environment": "prod"}; !maps.Equal(got, want) {
		t.Errorf("labels = %v, want %v", got, want)
	}
}

func TestApply_ReportsSkippedRemovalsWhenAChangeFails(t *testing.T) {
	prod := fake.NewSimpleClientset(appDeployment("api", map[string]string{"temp": "1"}))
	useFakeClusters(t, map[string]*fake.Clientset{"prod": prod})
	useKubernetesConfig(t, `clouds:
  kubernetes:
    - name: prod
      context: prod
      namespaces: [app]
      resource_types: [k8s_deployment]
policy:
  required:
    - name: owner
  forbidden:
    - name: temp
rules:
  defaults:
    - resource: "k8s_*"
      when:
        "tag:owner": absent
      set:
        owner: platform@company.com
`)
	if err := runScan(scanCmd, nil); err != nil {
		t.Fatalf("runScan() error = %v", err)
	}
	if err := runPlan(planCmd, nil); err != nil {
		t.Fatalf("runPlan() error = %v", err)
	}
	setFlags(t, applyCmd, map[string]string{"auto-approve": "true"})

	var err error
	stdout := captureStdout(t, func() { err = runApply(applyCmd, nil) })

	if err == nil || !strings.Contains(err.Error(), "1 of 1 changes failed") || ExitCode(err) != exitError {
		t.Errorf("runApply() error = %v, want the invalid label to fail the run", err)
	}
	if !strings.Contains(stdout, "Skipped: 1 removal(s)") {
		t.Errorf("stdout does not report the skipped removal:\n%s", stdout)
	}
}

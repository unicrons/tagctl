package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

const (
	// injected colours the terminal and returns the cursor when printed raw.
	injected    = "\x1b[31m\r"
	neutralised = "?[31m?"

	applyNormal      = "normal"
	applyInteractive = "interactive"
	applyMock        = "mock"
)

// failingCluster is the provider of cluster "prod"; every write fails with err.
type failingCluster struct{ err error }

func (p failingCluster) Name() string      { return providerKubernetes }
func (p failingCluster) AccountID() string { return "prod" }
func (p failingCluster) ListResources(context.Context) ([]types.Resource, error) {
	return nil, nil
}
func (p failingCluster) ApplyTags(context.Context, string, map[string]string) error { return p.err }
func (p failingCluster) RemoveTags(context.Context, types.Resource, []string) error { return p.err }

func useFailingCluster(t *testing.T, message string) {
	t.Helper()
	original := newKubernetesProvider
	newKubernetesProvider = func(context.Context, config.KubernetesCluster) (provider.Provider, error) {
		return failingCluster{err: errors.New(message)}, nil
	}
	t.Cleanup(func() { newKubernetesProvider = original })
}

func writePlan(t *testing.T, dir, name string, changes []types.TagChange) string {
	t.Helper()
	data, err := json.Marshal(types.Plan{ID: "plan-1", Changes: changes})
	if err != nil {
		t.Fatal(err)
	}
	return writeFixture(t, dir, name, string(data))
}

func TestApply_NeutralisesControlCharactersFromThePlan(t *testing.T) {
	everyMode := []string{applyNormal, applyInteractive, applyMock}
	tests := []struct {
		name        string
		inject      func(*types.TagChange)
		providerErr string
		printedIn   []string
	}{
		{name: "resource type", inject: func(c *types.TagChange) { c.Resource.Type += injected }, printedIn: everyMode},
		{name: "resource ID", inject: func(c *types.TagChange) { c.Resource.ID += injected }, printedIn: everyMode},
		{name: "resource name", inject: func(c *types.TagChange) { c.Resource.Name += injected }, printedIn: []string{applyInteractive}},
		{name: "resource ARN", inject: func(c *types.TagChange) { c.Resource.ARN = "arn:" + injected }, printedIn: []string{applyInteractive}},
		{name: "provider", inject: func(c *types.TagChange) { c.Resource.Provider += injected }, printedIn: []string{applyNormal, applyInteractive}},
		{name: "account", inject: func(c *types.TagChange) { c.Resource.Account += injected }, printedIn: []string{applyInteractive}},
		{name: "region", inject: func(c *types.TagChange) { c.Resource.Region += injected }, printedIn: []string{applyInteractive}},
		{name: "tag key", inject: func(c *types.TagChange) { c.Tag += injected }, printedIn: everyMode},
		{name: "new value", inject: func(c *types.TagChange) { c.NewValue += injected }, printedIn: everyMode},
		{name: "old value", inject: func(c *types.TagChange) { c.OldValue += injected }, printedIn: everyMode},
		{name: "reason", inject: func(c *types.TagChange) { c.Reason += injected }, printedIn: everyMode},
		{name: "provider error", inject: func(*types.TagChange) {}, providerErr: "denied" + injected, printedIn: []string{applyNormal, applyInteractive}},
	}
	modes := []struct {
		name string
		args []string
	}{
		{applyNormal, []string{"--auto-approve"}},
		{applyInteractive, []string{"--interactive"}},
		{applyMock, []string{"--mock", "--auto-approve"}},
	}

	for _, tt := range tests {
		for _, mode := range modes {
			t.Run(tt.name+"/"+mode.name, func(t *testing.T) {
				resource := types.Resource{ID: "k8s_deployment/app/web", Name: "web", Type: "k8s_deployment", Provider: providerKubernetes, Account: "prod", Region: "app"}
				changes := []types.TagChange{
					{Resource: resource, Tag: "owner", Action: types.ActionUpdate, OldValue: "old", NewValue: "new", Reason: types.ReasonDefault},
					{Resource: resource, Tag: "Env", Action: types.ActionRemove, OldValue: "prod", Reason: types.ReasonRenamed},
				}
				for i := range changes {
					tt.inject(&changes[i])
				}

				dir := t.TempDir()
				cfg := writeFixture(t, dir, "tagctl.yaml", "clouds:\n  kubernetes:\n    - name: prod\n")
				plan := writePlan(t, dir, "plan.json", changes)
				useFailingCluster(t, cmp.Or(tt.providerErr, "denied"))
				answerApplyPrompts(t, "y\n", true)

				run := execute(t, append([]string{"apply", "--config", cfg, "--plan", plan, "--allow-removals"}, mode.args...)...)

				for stream, out := range map[string]string{"stdout": run.stdout, "stderr": run.stderr} {
					if strings.ContainsAny(out, "\x1b\r") {
						t.Errorf("%s carries the injected control characters: %q", stream, out)
					}
				}
				if !strings.Contains(run.stdout, "[2/2]") {
					t.Fatalf("apply did not run both changes (err %v):\n%s\n%s", run.err, run.stdout, run.stderr)
				}
				if slices.Contains(tt.printedIn, mode.name) && !strings.Contains(run.stdout+run.stderr, neutralised) {
					t.Errorf("the %s was not printed, so nothing was checked:\n%s\n%s", tt.name, run.stdout, run.stderr)
				}
			})
		}
	}
}

func TestApply_NeutralisesControlCharactersInThePlanPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("control characters are not valid in Windows file names")
	}

	dir := t.TempDir()
	cfg := writeFixture(t, dir, "tagctl.yaml", demoPolicy)
	changes := make([]types.TagChange, 0, maxListedRemovals+1)
	for i := range maxListedRemovals + 1 {
		changes = append(changes, removal(fmt.Sprintf("i-%d", i), "temp", "1", types.ReasonForbiddenTag))
	}
	plan := writePlan(t, dir, "plan-"+injected+".json", changes)

	run := execute(t, "apply", "--config", cfg, "--plan", plan, "--mock", "--auto-approve", "--allow-removals")
	if run.err != nil {
		t.Fatalf("apply error = %v", run.err)
	}

	if strings.ContainsAny(run.stdout+run.stderr, "\x1b\r") {
		t.Errorf("output carries the injected control characters:\n%q\n%q", run.stdout, run.stderr)
	}
	name := filepath.Join(dir, "plan-"+neutralised+".json")
	for _, want := range []string{"Applying plan from " + name, "... and 1 more, see " + name} {
		if !strings.Contains(run.stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, run.stderr)
		}
	}
}

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unicrons/tagctl/internal/types"
)

func TestExecute_DemoPlanIsPrintedButNeverSaved(t *testing.T) {
	dir := t.TempDir()
	policy := writeFixture(t, dir, "tagctl.yaml", "policy:\n  required:\n    - name: owner\n")
	explicit := filepath.Join(dir, "explicit", "plan.json")
	if err := os.Mkdir(filepath.Dir(explicit), 0o750); err != nil {
		t.Fatal(err)
	}
	reports := filepath.Join(dir, "reports")

	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "default path", args: []string{"-c", policy, "plan", "-o", "json"}},
		{name: "explicit --out", args: []string{"-c", policy, "plan", "-o", "json", "--out", explicit}},
		{name: "--output-dir", args: []string{"-c", policy, "plan", "-o", "json", "--output-dir", reports}},
		{name: "--output-dir with a bare --out", args: []string{"-c", policy, "plan", "-o", "json", "--output-dir", reports, "--out", "plan-demo.json"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			run := execute(t, tt.args...)
			if run.err != nil {
				t.Fatalf("plan failed: %v", run.err)
			}

			var plan types.Plan
			if err := json.Unmarshal([]byte(run.stdout), &plan); err != nil || plan.IsEmpty() {
				t.Fatalf("stdout is not the demo plan (err %v): %q", err, run.stdout)
			}
			if strings.Contains(run.stderr, "Plan saved to") {
				t.Errorf("stderr claims the demo plan was saved: %q", run.stderr)
			}

			if entries, _ := os.ReadDir(OutputDir); len(entries) != 0 {
				t.Errorf("demo plan left %d file(s) in the output directory", len(entries))
			}
			if _, err := os.Stat(explicit); !os.IsNotExist(err) {
				t.Errorf("demo plan was written to --out (stat err %v)", err)
			}
			if _, err := os.Stat(reports); !os.IsNotExist(err) {
				t.Errorf("demo plan created --output-dir (stat err %v)", err)
			}
			for _, planDir := range []string{OutputDir, reports} {
				if _, err := FindLatestPlanInDir(planDir); err == nil {
					t.Errorf("apply would find a plan in %s after a demo run", planDir)
				}
			}
		})
	}
}

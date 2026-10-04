package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/terraform"
	"github.com/unicrons/tagctl/internal/types"
)

// hostile clears the screen and rings the bell on a terminal that obeys it.
const hostile = "x\x1b[2Jy\x07z"

func hostileResource() types.Resource {
	return types.Resource{
		ID: "id-" + hostile, Name: "name-" + hostile, Type: "type-" + hostile,
		Provider: "provider-" + hostile, Account: "account-" + hostile, Region: "region-" + hostile,
	}
}

func hostileFinding(status types.FindingStatus, reason types.ViolationReason) types.Finding {
	return types.Finding{
		Resource: hostileResource(), Tag: "tag-" + hostile, Status: status, Reason: reason,
		Actual: "actual-" + hostile, Expected: "expected-" + hostile,
	}
}

func TestCommandOutput_NeutralisesEscapeSequences(t *testing.T) {
	failed := hostileFinding(types.StatusFailed, types.ReasonInvalidValue)
	passing := hostileFinding(types.StatusPass, types.ReasonCompliant)

	scan := &types.ScanResult{
		ScannedAt: time.Now(), TotalResources: 1, ViolationCount: 1,
		Findings:  []types.Finding{failed, passing},
		ByAccount: map[string]*types.AccountStats{"a": {Provider: "provider-" + hostile, Account: "account-" + hostile, Total: 1}},
		ByTag:     map[string]*types.TagStats{"t": {Tag: "tag-" + hostile, Required: true, Missing: 1}},
	}

	tests := []struct {
		name  string
		print func()
	}{
		{name: "scan table", print: func() { _ = outputScanTable(scan, true) }},
		{name: "plan table", print: func() {
			outputPlanTable(&types.Plan{Changes: []types.TagChange{{
				Resource: hostileResource(), Tag: "tag-" + hostile, Action: types.ActionAdd,
				NewValue: "value-" + hostile, Reason: types.ReasonDefault, Source: "source-" + hostile,
			}}})
		}},
		{name: "diff table", print: func() {
			printDiffTable(&types.DiffResult{
				Regressions: []types.Finding{failed},
				Resolved:    []types.Finding{failed},
				ByTag: map[string]*types.TagDelta{
					"t": {Tag: "tag-" + hostile, CompliancePctBefore: 100},
				},
			}, "baseline.json", "current.json")
		}},
		{name: "terraform table", print: func() { printTerraformResult(scan, "plan.json", false) }},
		{name: "terraform unchecked tags", print: func() {
			var buf bytes.Buffer
			log.SetOutput(&buf)
			defer log.SetOutput(os.Stderr)
			resource := hostileResource()
			resource.UnknownTags = []string{"tag-" + hostile}
			logUncheckedTags(terraform.Result{
				Resources:  []types.Resource{resource},
				Unreadable: []string{"address-" + hostile},
			})
			_, _ = os.Stdout.Write(buf.Bytes())
		}},
		{name: "cost table", print: func() {
			printCostReport(os.Stdout, &types.CostReport{
				Start: time.Now().AddDate(0, 0, -7), End: time.Now(),
				Currency: "USD-" + hostile, Total: 10,
				Tags: map[string]*types.TagCost{"t": {
					Tag: "tag-" + hostile, Attributed: 4, Unattributed: 6,
					Values: []types.ValueCost{{Value: "value-" + hostile, Amount: 4}},
					Trend: &types.CostTrend{
						Granularity: types.CostDaily,
						Periods:     []types.CostPeriod{{Start: time.Now().AddDate(0, 0, -1), End: time.Now(), Attributed: 4, Unattributed: 6}},
						Change:      &types.CostChange{Unattributed: 1},
						Projection:  &types.CostProjection{Start: time.Now(), End: time.Now().AddDate(0, 0, 1), Unattributed: 7, Method: "linear"},
					},
				}},
			})
		}},
		{name: "normalize table", print: func() {
			printNormalizeTable(&types.NormalizeResult{
				ResourcesScanned: 2, TagsScanned: 1,
				Clusters: []types.ValueCluster{{
					Tag: "tag-" + hostile, Canonical: "canonical-" + hostile, CanonicalCount: 1,
					Variants: []types.ValueVariant{{
						Value: "variant-" + hostile, Count: 1, Match: types.MatchCasing,
						Resources: []types.Resource{hostileResource()},
					}},
				}},
			}, "scan.json")
		}},
		{name: "validate report", print: func() {
			report := &validationReport{warnings: []string{"warning-" + hostile}, errors: []string{"error-" + hostile}}
			_ = report.print()
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStdout(t, tt.print)

			if !strings.Contains(out, "x") || len(out) < len(hostile) {
				t.Fatalf("nothing was printed: %q", out)
			}
			if strings.Contains(out, "\x1b[2J") {
				t.Errorf("output carries the injected escape sequence: %q", out)
			}
			if strings.ContainsRune(out, '\x07') {
				t.Errorf("output carries the injected control character: %q", out)
			}
		})
	}
}

func TestLogUncheckedTags_KeepsEachEntryOnOneLine(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	logUncheckedTags(terraform.Result{
		Unreadable: []string{"aws_instance.a\nforged line"},
		Resources: []types.Resource{{
			ID:          "aws_instance.b\nforged line",
			UnknownTags: []string{"owner\nforged line"},
		}},
	})

	if got := strings.Count(buf.String(), "\n"); got != 2 {
		t.Errorf("2 entries logged %d lines:\n%s", got, buf.String())
	}
}

func assertNoControlCharacters(t *testing.T, out string, want ...string) {
	t.Helper()

	for _, text := range want {
		if !strings.Contains(out, text) {
			t.Errorf("output lacks %q:\n%q", text, out)
		}
	}
	if strings.ContainsAny(out, "\x1b\x07") {
		t.Errorf("output carries an injected control character: %q", out)
	}
}

func TestOutputDirNotices_NeutraliseEscapeSequences(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("control characters are not valid in Windows file names")
	}

	t.Run("scan report paths", func(t *testing.T) {
		dir := t.TempDir()
		policy := writeFixture(t, dir, "tagctl.yaml", demoPolicy)

		run := execute(t, "-c", policy, "scan", "--mock", "--output-dir", filepath.Join(dir, hostile))
		if run.err != nil {
			t.Fatalf("scan error = %v", run.err)
		}
		assertNoControlCharacters(t, run.stderr, "Detailed results saved to")
	})

	t.Run("plan scan and plan paths", func(t *testing.T) {
		dir := t.TempDir()
		config := writeFixture(t, dir, "tagctl.yaml", awsPolicy+
			"rules:\n  defaults:\n    - resource: \"*\"\n      when:\n        \"tag:owner\": absent\n      set:\n        owner: platform@example.com\n")
		reports := filepath.Join(dir, hostile)
		if err := os.Mkdir(reports, 0o750); err != nil {
			t.Fatal(err)
		}
		scan := failedScan(0)
		scan.Partial, scan.Errors = true, []string{"aws: throttled"}
		writeScan(t, reports, "scan-20260101-100000.json", scan)

		run := execute(t, "-c", config, "plan", "--output-dir", reports)
		if run.err != nil {
			t.Fatalf("plan error = %v", run.err)
		}
		assertNoControlCharacters(t, run.stderr, "Loaded scan from", "is a partial scan", "Plan saved to")
	})
}

func TestScan_ResourceTypeWarningNeutralisesEscapeSequences(t *testing.T) {
	policy := writeFixture(t, t.TempDir(), "tagctl.yaml", awsPolicy)
	stubScanProviders(t, stubResource("i-1", "aws_instance"))

	run := execute(t, "-c", policy, "scan", "--no-files", "--resource-type", "zz\x1bc\x07*")
	if run.err != nil {
		t.Fatalf("scan error = %v", run.err)
	}
	assertNoControlCharacters(t, run.stderr, "no discovered resource matches --resource-type zz?c?*")
}

func TestCommandNotices_NeutraliseEscapeSequencesInPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("control characters are not valid in Windows file names")
	}

	const shown = "evil?[31m?dir"
	dir := filepath.Join(t.TempDir(), "evil\x1b[31m\rdir")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	config := writeFixture(t, dir, "tagctl.yaml", awsPolicy)
	tfPlan := writeFixture(t, dir, "plan.json", terraformPlanFixture)
	resources := writeFixture(t, dir, "resources.json", `[
		{"id": "i-1", "type": "aws_instance", "provider": "aws", "tags": {"environment": "prod"}},
		{"id": "i-2", "type": "aws_instance", "provider": "aws", "tags": {"environment": "Prod"}}
	]`)
	unnamed := writeScan(t, dir, "unnamed.json", &types.ScanResult{TotalResources: 3})
	scans := filepath.Join(dir, "scans")
	if err := os.Mkdir(scans, 0o750); err != nil {
		t.Fatal(err)
	}
	scan := writeScan(t, scans, scanJan, failedScan(0))
	writeScan(t, scans, scanFeb, failedScan(0))

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "diff file arguments",
			args: []string{"diff", scan, scan},
			want: []string{"Baseline: ", "Current:  "},
		},
		{
			name: "diff scans found in --output-dir",
			args: []string{"diff", "--output-dir", scans},
			want: []string{"Baseline: ", "Current:  "},
		},
		{
			name: "diff scan without an inventory",
			args: []string{"diff", unnamed, unnamed},
			want: []string{"Warning: "},
		},
		{
			name: "normalize --scan",
			args: []string{"normalize", "--scan", scan},
			want: []string{"Source: "},
		},
		{
			name: "normalize --resources and --out",
			args: []string{"normalize", "--resources", resources, "--out", filepath.Join(dir, "normalize-plan.json")},
			want: []string{"Source: ", "Plan written to ", "tagctl apply --plan "},
		},
		{
			name: "terraform --plan",
			args: []string{"terraform", "--plan", tfPlan},
			want: []string{"Source: "},
		},
		{
			name: "validate config path",
			args: []string{"validate"},
			want: []string{"Validating "},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			run := execute(t, append([]string{"-c", config}, tt.args...)...)
			if run.err != nil {
				t.Fatalf("tagctl %v error = %v", tt.args, run.err)
			}

			out := run.stdout + run.stderr
			for _, want := range tt.want {
				found := false
				for _, line := range strings.Split(out, "\n") {
					if strings.Contains(line, want) && strings.Contains(line, shown) {
						found = true
					}
				}
				if !found {
					t.Errorf("no line carries %q with the neutralised path %q:\n%q", want, shown, out)
				}
			}
			if strings.ContainsAny(out, "\x1b\r") {
				t.Errorf("output carries the raw escape sequence or carriage return: %q", out)
			}
		})
	}
}

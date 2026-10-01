package cli

import (
	"bytes"
	"os"
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

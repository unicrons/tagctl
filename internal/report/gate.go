package report

import (
	"fmt"
	"strings"

	"github.com/unicrons/tagctl/internal/types"
)

// Gate decides whether a scan should fail a pipeline.
//
// The two checks answer different questions. FailUnder is an absolute bar:
// "this estate must be at least this compliant". FailOnNew is relative to a
// baseline: "whatever state we are in, do not make it worse". A team adopting
// tagctl on a large non-compliant estate uses the second, because the first
// would keep the build red for months.
type Gate struct {
	// FailUnder is the minimum compliance percentage. Zero disables the check.
	FailUnder float64

	// FailOnNew fails when a finding regressed against the baseline.
	FailOnNew bool
}

// GateResult is the outcome of evaluating a gate.
type GateResult struct {
	// Passed reports whether the scan cleared every enabled check.
	Passed bool

	// Reasons lists why the gate failed, empty when it passed.
	Reasons []string
}

// Error returns an error describing the failure, or nil when the gate passed.
func (r *GateResult) Error() error {
	if r.Passed {
		return nil
	}
	return fmt.Errorf("compliance gate failed: %s", strings.Join(r.Reasons, "; "))
}

// Evaluate applies the gate to a scan. The diff may be nil when no baseline
// was supplied, in which case the FailOnNew check is skipped rather than
// treated as a pass, since there is nothing to compare against.
func (g Gate) Evaluate(scan *types.ScanResult, diff *types.DiffResult) *GateResult {
	result := &GateResult{Passed: true}

	if g.FailUnder > 0 && scan.CompliancePct < g.FailUnder {
		result.Passed = false
		result.Reasons = append(result.Reasons,
			fmt.Sprintf("compliance is %.1f%%, below the required %.1f%%", scan.CompliancePct, g.FailUnder))
	}

	if g.FailOnNew && diff != nil && diff.HasRegressions() {
		result.Passed = false
		result.Reasons = append(result.Reasons,
			fmt.Sprintf("%d finding(s) regressed since the baseline", len(diff.Regressions)))
	}

	return result
}

// IsEnabled reports whether the gate performs any check at all.
func (g Gate) IsEnabled() bool {
	return g.FailUnder > 0 || g.FailOnNew
}

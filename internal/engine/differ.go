package engine

import (
	"sort"

	"github.com/unicrons/tagctl/internal/types"
)

// Diff compares a baseline scan against a current one and reports what moved.
//
// A finding is identified by the resource it belongs to plus the tag it checks,
// so the same resource failing two tags produces two independent findings. A
// finding that fails in the current scan but not in the baseline is a
// regression; one that failed in the baseline and no longer does is resolved.
// Resources that only exist on one side are reported separately, since a
// regression on a resource that did not exist before is still new work.
func Diff(baseline, current *types.ScanResult) *types.DiffResult {
	result := &types.DiffResult{
		BaselineScannedAt:   baseline.ScannedAt,
		CurrentScannedAt:    current.ScannedAt,
		CompliancePctBefore: baseline.CompliancePct,
		CompliancePctAfter:  current.CompliancePct,
		ByTag:               make(map[string]*types.TagDelta),
		ByAccount:           make(map[string]*types.AccountDelta),
	}

	baselineFailures := failedFindingsByKey(baseline)
	currentFailures := failedFindingsByKey(current)

	for key, finding := range currentFailures {
		if _, failedBefore := baselineFailures[key]; failedBefore {
			result.Unchanged++
			continue
		}
		result.Regressions = append(result.Regressions, finding)
	}

	for key, finding := range baselineFailures {
		if _, stillFailing := currentFailures[key]; !stillFailing {
			result.Resolved = append(result.Resolved, finding)
		}
	}

	sortFindings(result.Regressions)
	sortFindings(result.Resolved)

	result.NewResources = resourcesOnlyIn(current, baseline)
	result.RemovedResources = resourcesOnlyIn(baseline, current)

	diffTagStats(baseline, current, result)
	diffAccountStats(baseline, current, result)

	return result
}

// findingKey identifies a finding across scans: the same resource and tag.
type findingKey struct {
	provider string
	account  string
	resource string
	tag      string
}

// failedFindingsByKey indexes only the FAILED findings of a scan.
func failedFindingsByKey(scan *types.ScanResult) map[findingKey]types.Finding {
	failures := make(map[findingKey]types.Finding, len(scan.Findings))

	for _, finding := range scan.Findings {
		if finding.Status != types.StatusFailed {
			continue
		}
		failures[keyOf(finding)] = finding
	}

	// Older scan files carry findings under the deprecated Violations field.
	if len(failures) == 0 {
		for _, finding := range types.ViolationsToFindings(scan.Violations) {
			failures[keyOf(finding)] = finding
		}
	}

	return failures
}

func keyOf(finding types.Finding) findingKey {
	return findingKey{
		provider: finding.Resource.Provider,
		account:  finding.Resource.Account,
		resource: finding.Resource.Identity(),
		tag:      finding.Tag,
	}
}

// resourcesOnlyIn returns the resources that appear in a but not in b.
func resourcesOnlyIn(a, b *types.ScanResult) []types.Resource {
	inB := make(map[findingKey]bool)
	for _, finding := range allFindings(b) {
		inB[resourceKey(finding.Resource)] = true
	}

	findings := allFindings(a)
	seen := make(map[findingKey]bool, len(findings))
	only := make([]types.Resource, 0, len(findings))

	for _, finding := range findings {
		key := resourceKey(finding.Resource)
		if inB[key] || seen[key] {
			continue
		}
		seen[key] = true
		only = append(only, finding.Resource)
	}

	sort.Slice(only, func(i, j int) bool {
		if only[i].Account != only[j].Account {
			return only[i].Account < only[j].Account
		}
		return only[i].ID < only[j].ID
	})

	return only
}

// resourceKey identifies a resource independently of any tag.
func resourceKey(resource types.Resource) findingKey {
	return findingKey{
		provider: resource.Provider,
		account:  resource.Account,
		resource: resource.Identity(),
	}
}

// allFindings returns a scan's findings, falling back to the deprecated
// Violations field for scan files written by older versions.
func allFindings(scan *types.ScanResult) []types.Finding {
	if len(scan.Findings) > 0 {
		return scan.Findings
	}
	return types.ViolationsToFindings(scan.Violations)
}

func diffTagStats(baseline, current *types.ScanResult, result *types.DiffResult) {
	for tag, stats := range current.ByTag {
		delta := &types.TagDelta{
			Tag:                tag,
			CompliancePctAfter: stats.CompliancePct,
			FailedAfter:        stats.Missing + stats.Invalid,
		}
		if before, ok := baseline.ByTag[tag]; ok {
			delta.CompliancePctBefore = before.CompliancePct
			delta.FailedBefore = before.Missing + before.Invalid
		}
		result.ByTag[tag] = delta
	}

	// A tag that disappeared from the policy still deserves a row.
	for tag, stats := range baseline.ByTag {
		if _, ok := result.ByTag[tag]; ok {
			continue
		}
		result.ByTag[tag] = &types.TagDelta{
			Tag:                 tag,
			CompliancePctBefore: stats.CompliancePct,
			FailedBefore:        stats.Missing + stats.Invalid,
		}
	}
}

func diffAccountStats(baseline, current *types.ScanResult, result *types.DiffResult) {
	for account, stats := range current.ByAccount {
		delta := &types.AccountDelta{
			Account:            account,
			CompliancePctAfter: stats.CompliancePct,
			TotalAfter:         stats.Total,
		}
		if before, ok := baseline.ByAccount[account]; ok {
			delta.CompliancePctBefore = before.CompliancePct
			delta.TotalBefore = before.Total
		}
		result.ByAccount[account] = delta
	}

	for account, stats := range baseline.ByAccount {
		if _, ok := result.ByAccount[account]; ok {
			continue
		}
		result.ByAccount[account] = &types.AccountDelta{
			Account:             account,
			CompliancePctBefore: stats.CompliancePct,
			TotalBefore:         stats.Total,
		}
	}
}

// sortFindings orders findings so output is stable across runs.
func sortFindings(findings []types.Finding) {
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.Resource.Account != b.Resource.Account {
			return a.Resource.Account < b.Resource.Account
		}
		if a.Resource.ID != b.Resource.ID {
			return a.Resource.ID < b.Resource.ID
		}
		return a.Tag < b.Tag
	})
}

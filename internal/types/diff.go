package types

import "time"

// DiffResult describes how compliance changed between two scans.
type DiffResult struct {
	// BaselineScannedAt is when the older scan was taken.
	BaselineScannedAt time.Time `json:"baseline_scanned_at"`

	// CurrentScannedAt is when the newer scan was taken.
	CurrentScannedAt time.Time `json:"current_scanned_at"`

	// Regressions are findings that fail now but passed (or did not exist) before.
	Regressions []Finding `json:"regressions"`

	// Resolved are findings that failed before and no longer do.
	Resolved []Finding `json:"resolved"`

	// Unchanged is the number of findings that failed in both scans.
	Unchanged int `json:"unchanged"`

	// NewResources are resources present only in the current scan.
	NewResources []Resource `json:"new_resources"`

	// RemovedResources are resources present only in the baseline scan.
	RemovedResources []Resource `json:"removed_resources"`

	// CompliancePctBefore is the baseline compliance percentage.
	CompliancePctBefore float64 `json:"compliance_percent_before"`

	// CompliancePctAfter is the current compliance percentage.
	CompliancePctAfter float64 `json:"compliance_percent_after"`

	// ByTag holds the per-tag movement, keyed by tag name.
	ByTag map[string]*TagDelta `json:"by_tag"`

	// ByAccount holds the per-account movement, keyed by account.
	ByAccount map[string]*AccountDelta `json:"by_account"`
}

// CompliancePctDelta is the change in overall compliance, positive when
// compliance improved.
func (d *DiffResult) CompliancePctDelta() float64 {
	return d.CompliancePctAfter - d.CompliancePctBefore
}

// HasRegressions reports whether anything got worse.
func (d *DiffResult) HasRegressions() bool {
	return len(d.Regressions) > 0
}

// IsClean reports whether nothing changed at all between the two scans.
func (d *DiffResult) IsClean() bool {
	return len(d.Regressions) == 0 && len(d.Resolved) == 0 &&
		len(d.NewResources) == 0 && len(d.RemovedResources) == 0
}

// TagDelta is the movement in compliance for a single tag.
type TagDelta struct {
	// Tag is the tag name.
	Tag string `json:"tag"`

	// CompliancePctBefore is the tag's baseline compliance percentage.
	CompliancePctBefore float64 `json:"compliance_percent_before"`

	// CompliancePctAfter is the tag's current compliance percentage.
	CompliancePctAfter float64 `json:"compliance_percent_after"`

	// FailedBefore is how many resources failed this tag in the baseline.
	FailedBefore int `json:"failed_before"`

	// FailedAfter is how many resources fail this tag now.
	FailedAfter int `json:"failed_after"`
}

// Delta is the change in compliance percentage for this tag.
func (t *TagDelta) Delta() float64 {
	return t.CompliancePctAfter - t.CompliancePctBefore
}

// AccountDelta is the movement in compliance for a single account.
type AccountDelta struct {
	// Account is the account identifier.
	Account string `json:"account"`

	// CompliancePctBefore is the account's baseline compliance percentage.
	CompliancePctBefore float64 `json:"compliance_percent_before"`

	// CompliancePctAfter is the account's current compliance percentage.
	CompliancePctAfter float64 `json:"compliance_percent_after"`

	// TotalBefore is the resource count in the baseline.
	TotalBefore int `json:"total_before"`

	// TotalAfter is the resource count now.
	TotalAfter int `json:"total_after"`
}

// Delta is the change in compliance percentage for this account.
func (a *AccountDelta) Delta() float64 {
	return a.CompliancePctAfter - a.CompliancePctBefore
}

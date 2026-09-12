package types

import "time"

// ScanResult contains the results of a compliance scan.
type ScanResult struct {
	// ScannedAt is when the scan was performed.
	ScannedAt time.Time `json:"scanned_at"`

	// Partial is true when discovery failed for part of the estate, so
	// resources may be missing from every count and finding.
	Partial bool `json:"partial,omitempty"`

	// Errors lists the discovery failures of a partial scan.
	Errors []string `json:"errors,omitempty"`

	// TotalResources is the total number of resources scanned.
	TotalResources int `json:"total_resources"`

	// CompliantCount is the number of fully compliant resources.
	CompliantCount int `json:"compliant_count"`

	// ViolationCount is the number of FAILED findings.
	ViolationCount int `json:"violation_count"`

	// CompliancePct is the percentage of compliant resources.
	CompliancePct float64 `json:"compliance_percent"`

	// Violations repeats the FAILED findings, in order (DEPRECATED: use Findings).
	Violations []Violation `json:"violations"`

	// Findings is the list of all compliance findings (PASS and FAILED).
	Findings []Finding `json:"findings"`

	// ByAccount contains per-account statistics.
	ByAccount map[string]*AccountStats `json:"by_account"`

	// ByTag contains per-tag statistics.
	ByTag map[string]*TagStats `json:"by_tag"`
}

// NewScanResult creates an initialized ScanResult.
func NewScanResult() *ScanResult {
	return &ScanResult{
		ScannedAt: time.Now(),
		ByAccount: make(map[string]*AccountStats),
		ByTag:     make(map[string]*TagStats),
	}
}

// FailedFindings returns the FAILED findings, reading the deprecated
// Violations field when the scan was written by an older version.
func (s *ScanResult) FailedFindings() []Finding {
	failures := make([]Finding, 0, len(s.Findings))
	for _, finding := range s.Findings {
		if finding.Status == StatusFailed {
			failures = append(failures, finding)
		}
	}

	if len(failures) == 0 && len(s.Violations) > 0 {
		failures = ViolationsToFindings(s.Violations)
	}

	return failures
}

// AccountStats contains per-account statistics.
type AccountStats struct {
	// Account is the account identifier.
	Account string `json:"account"`

	// Provider is the cloud provider.
	Provider string `json:"provider"`

	// Total is the total number of resources in this account.
	Total int `json:"total"`

	// Compliant is the number of compliant resources.
	Compliant int `json:"compliant"`

	// CompliancePct is the compliance percentage.
	CompliancePct float64 `json:"compliance_percent"`
}

// TagStats contains per-tag statistics. An optional tag only counts the
// resources that carry it.
type TagStats struct {
	// Tag is the tag name.
	Tag string `json:"tag"`

	// Required indicates if this is a required tag.
	Required bool `json:"required"`

	// Present is the count of resources with this tag.
	Present int `json:"present"`

	// Missing is the count of resources missing this tag; always 0 for an optional tag.
	Missing int `json:"missing"`

	// Invalid is the count of resources with invalid values.
	Invalid int `json:"invalid"`

	// CompliancePct is the compliance percentage for this tag.
	CompliancePct float64 `json:"compliance_percent"`
}

// CalculateCompliance updates the compliance percentage based on counts.
func (s *ScanResult) CalculateCompliance() {
	if s.TotalResources > 0 {
		s.CompliancePct = float64(s.CompliantCount) / float64(s.TotalResources) * 100
	}

	for _, acc := range s.ByAccount {
		if acc.Total > 0 {
			acc.CompliancePct = float64(acc.Compliant) / float64(acc.Total) * 100
		}
	}

	for _, tag := range s.ByTag {
		switch total := tag.Present + tag.Missing; {
		case total > 0:
			tag.CompliancePct = float64(tag.Present-tag.Invalid) / float64(total) * 100
		case !tag.Required:
			tag.CompliancePct = 100
		}
	}
}

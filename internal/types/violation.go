package types

// FindingStatus represents the status of a compliance finding.
type FindingStatus string

const (
	// StatusPass indicates the resource is compliant for this tag.
	StatusPass FindingStatus = "PASS"

	// StatusFailed indicates the resource is non-compliant for this tag.
	StatusFailed FindingStatus = "FAILED"
)

// ViolationReason describes why a tag is non-compliant.
type ViolationReason string

const (
	// ReasonMissing indicates the required tag is not present.
	ReasonMissing ViolationReason = "missing"

	// ReasonInvalidValue indicates the tag value is not in the allowed list.
	ReasonInvalidValue ViolationReason = "invalid_value"

	// ReasonInvalidFormat indicates the tag value doesn't match the pattern.
	ReasonInvalidFormat ViolationReason = "invalid_format"

	// ReasonForbidden indicates the resource carries a tag the policy forbids.
	ReasonForbidden ViolationReason = "forbidden"

	// ReasonCompliant indicates the tag is present and valid.
	ReasonCompliant ViolationReason = "compliant"
)

// Violation is a FAILED Finding as written under the deprecated violations key.
type Violation Finding

// Message returns a human-readable description of the violation.
func (v *Violation) Message() string {
	f := v.ToFinding()
	return f.Message()
}

// Finding represents a compliance check result for a resource+tag combination.
type Finding struct {
	// Resource is the resource being checked.
	Resource Resource `json:"resource"`

	// Tag is the name of the tag being checked.
	Tag string `json:"tag"`

	// Status is PASS or FAILED.
	Status FindingStatus `json:"status"`

	// Reason describes why it passed or failed.
	Reason ViolationReason `json:"reason"`

	// Expected is what was expected (pattern or allowed values).
	Expected string `json:"expected,omitempty"`

	// Actual is the actual value found.
	Actual string `json:"actual,omitempty"`
}

// Message returns a human-readable description of the finding.
func (f *Finding) Message() string {
	if f.Status == StatusPass {
		if f.Actual != "" {
			return "tag '" + f.Tag + "' is present with value '" + f.Actual + "'"
		}
		return "tag '" + f.Tag + "' is compliant"
	}

	switch f.Reason {
	case ReasonMissing:
		return "required tag '" + f.Tag + "' is missing"
	case ReasonInvalidValue:
		return "tag '" + f.Tag + "' has invalid value '" + f.Actual + "'"
	case ReasonInvalidFormat:
		return "tag '" + f.Tag + "' value '" + f.Actual + "' doesn't match pattern"
	case ReasonForbidden:
		if f.Expected != "" {
			return "tag '" + f.Tag + "' has forbidden value '" + f.Actual + "'"
		}
		return "tag '" + f.Tag + "' is forbidden"
	default:
		return "tag '" + f.Tag + "' is non-compliant"
	}
}

// ToFinding converts a Violation to the equivalent Finding. Violations are
// always failures, so the resulting Finding carries StatusFailed.
func (v *Violation) ToFinding() Finding {
	f := Finding(*v)
	f.Status = StatusFailed
	return f
}

// ViolationsToFindings converts a slice of Violations, as written by older
// versions of tagctl, into Findings.
func ViolationsToFindings(violations []Violation) []Finding {
	findings := make([]Finding, 0, len(violations))
	for i := range violations {
		findings = append(findings, violations[i].ToFinding())
	}
	return findings
}

package types

import (
	"testing"
)

func TestViolation_Message(t *testing.T) {
	tests := []struct {
		name      string
		violation Violation
		want      string
	}{
		{
			name: "missing tag",
			violation: Violation{
				Tag:    "environment",
				Reason: ReasonMissing,
			},
			want: "required tag 'environment' is missing",
		},
		{
			name: "invalid value",
			violation: Violation{
				Tag:    "environment",
				Reason: ReasonInvalidValue,
				Actual: "production",
			},
			want: "tag 'environment' has invalid value 'production'",
		},
		{
			name: "invalid format",
			violation: Violation{
				Tag:    "owner",
				Reason: ReasonInvalidFormat,
				Actual: "john",
			},
			want: "tag 'owner' value 'john' doesn't match pattern",
		},
		{
			name: "unknown reason",
			violation: Violation{
				Tag:    "test",
				Reason: ViolationReason("unknown"),
			},
			want: "tag 'test' is non-compliant",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.violation.Message(); got != tt.want {
				t.Errorf("Message() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestViolationReason_Constants(t *testing.T) {
	// Ensure constants have expected values (for JSON serialization compatibility)
	tests := []struct {
		reason ViolationReason
		want   string
	}{
		{ReasonMissing, "missing"},
		{ReasonInvalidValue, "invalid_value"},
		{ReasonInvalidFormat, "invalid_format"},
	}

	for _, tt := range tests {
		t.Run(string(tt.reason), func(t *testing.T) {
			if string(tt.reason) != tt.want {
				t.Errorf("ViolationReason = %v, want %v", tt.reason, tt.want)
			}
		})
	}
}

func TestFindingStatus_Constants(t *testing.T) {
	// Ensure constants have expected values (for JSON serialization compatibility)
	tests := []struct {
		status FindingStatus
		want   string
	}{
		{StatusPass, "PASS"},
		{StatusFailed, "FAILED"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if string(tt.status) != tt.want {
				t.Errorf("FindingStatus = %v, want %v", tt.status, tt.want)
			}
		})
	}
}

func TestViolation_Status(t *testing.T) {
	// Violations should always have Status = FAILED
	violation := Violation{
		Tag:    "environment",
		Status: StatusFailed,
		Reason: ReasonMissing,
	}

	if violation.Tag != "environment" {
		t.Errorf("Violation.Tag = %v, want %v", violation.Tag, "environment")
	}

	if violation.Reason != ReasonMissing {
		t.Errorf("Violation.Reason = %v, want %v", violation.Reason, ReasonMissing)
	}

	if violation.Status != StatusFailed {
		t.Errorf("Violation.Status = %v, want %v", violation.Status, StatusFailed)
	}
}

func TestFinding_Message(t *testing.T) {
	tests := []struct {
		name    string
		finding Finding
		want    string
	}{
		{
			name: "pass with value",
			finding: Finding{
				Tag:    "environment",
				Status: StatusPass,
				Reason: ReasonCompliant,
				Actual: "prod",
			},
			want: "tag 'environment' is present with value 'prod'",
		},
		{
			name: "pass without value",
			finding: Finding{
				Tag:    "environment",
				Status: StatusPass,
				Reason: ReasonCompliant,
			},
			want: "tag 'environment' is compliant",
		},
		{
			name: "failed missing",
			finding: Finding{
				Tag:    "environment",
				Status: StatusFailed,
				Reason: ReasonMissing,
			},
			want: "required tag 'environment' is missing",
		},
		{
			name: "failed invalid value",
			finding: Finding{
				Tag:    "environment",
				Status: StatusFailed,
				Reason: ReasonInvalidValue,
				Actual: "production",
			},
			want: "tag 'environment' has invalid value 'production'",
		},
		{
			name: "failed invalid format",
			finding: Finding{
				Tag:    "owner",
				Status: StatusFailed,
				Reason: ReasonInvalidFormat,
				Actual: "john",
			},
			want: "tag 'owner' value 'john' doesn't match pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.finding.Message(); got != tt.want {
				t.Errorf("Finding.Message() = %v, want %v", got, tt.want)
			}
		})
	}
}

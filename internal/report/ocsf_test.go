package report

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/unicrons/tagctl/internal/types"
)

func decodeOCSF(t *testing.T, scan *types.ScanResult) []OCSFComplianceFinding {
	t.Helper()

	var buf bytes.Buffer
	if err := WriteOCSF(&buf, scan, OCSFOptions{Version: "1.2.3"}); err != nil {
		t.Fatalf("WriteOCSF() error = %v", err)
	}

	var events []OCSFComplianceFinding
	if err := json.Unmarshal(buf.Bytes(), &events); err != nil {
		t.Fatalf("output is not a JSON array of events: %v", err)
	}
	return events
}

func TestWriteOCSF_ClassIdentity(t *testing.T) {
	events := decodeOCSF(t, scanOf(
		failed("i-1", "aws_instance", "111", tagOwner, types.ReasonMissing),
		passed("i-2", "aws_s3_bucket", "111", tagOwner),
	))

	// Passing findings are emitted too, unlike SARIF.
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (pass and fail)", len(events))
	}

	for _, e := range events {
		if e.ClassUID != 2003 || e.CategoryUID != 2 || e.ActivityID != 1 || e.TypeUID != 200301 {
			t.Errorf("identity = class %d category %d activity %d type %d, want 2003/2/1/200301",
				e.ClassUID, e.CategoryUID, e.ActivityID, e.TypeUID)
		}
		if e.Metadata.Version != "1.4.0" {
			t.Errorf("metadata.version = %q, want 1.4.0", e.Metadata.Version)
		}
		if e.Metadata.Product.Name != "tagctl" || e.Metadata.Product.Version != "1.2.3" {
			t.Errorf("metadata.product = %+v, want tagctl 1.2.3", e.Metadata.Product)
		}
		if e.Time != scanOf().ScannedAt.UnixMilli() {
			t.Errorf("time = %d, want the scan time in epoch milliseconds", e.Time)
		}
	}
}

// osint is required by the schema even when there is nothing to report, so it
// must serialise as an empty array rather than be omitted.
func TestWriteOCSF_RequiredArraysArePresent(t *testing.T) {
	var buf bytes.Buffer
	scan := scanOf(passed("i-1", "aws_instance", "111", tagOwner))
	if err := WriteOCSF(&buf, scan, OCSFOptions{}); err != nil {
		t.Fatalf("WriteOCSF() error = %v", err)
	}

	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"osint", "observables", "resources", "cloud", "compliance", "finding_info", "metadata", "severity_id", "time"} {
		if _, ok := raw[0][key]; !ok {
			t.Errorf("event is missing required attribute %q", key)
		}
	}
	if string(raw[0]["osint"]) != "[]" {
		t.Errorf("osint = %s, want []", raw[0]["osint"])
	}
}

func TestWriteOCSF_ComplianceStatusAndSeverity(t *testing.T) {
	cases := []struct {
		name         string
		finding      types.Finding
		wantStatusID int
		wantStatus   string
		wantSeverity int
		wantFix      bool
	}{
		{"pass", passed("i-1", "aws_instance", "111", tagOwner), 1, "Pass", 1, false},
		{"missing", failed("i-1", "aws_instance", "111", tagOwner, types.ReasonMissing), 3, "Fail", 3, true},
		{"invalid value", failed("i-1", "aws_instance", "111", tagEnv, types.ReasonInvalidValue), 3, "Fail", 2, true},
		{"invalid format", failed("i-1", "aws_instance", "111", tagOwner, types.ReasonInvalidFormat), 3, "Fail", 2, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := decodeOCSF(t, scanOf(tc.finding))[0]

			if e.Compliance.StatusID != tc.wantStatusID || e.Compliance.Status != tc.wantStatus {
				t.Errorf("compliance status = %d %q, want %d %q", e.Compliance.StatusID, e.Compliance.Status, tc.wantStatusID, tc.wantStatus)
			}
			if e.SeverityID != tc.wantSeverity {
				t.Errorf("severity_id = %d, want %d", e.SeverityID, tc.wantSeverity)
			}
			if (e.Remediation != nil) != tc.wantFix {
				t.Errorf("remediation present = %v, want %v", e.Remediation != nil, tc.wantFix)
			}
			if e.Compliance.Control != tc.finding.Tag {
				t.Errorf("compliance.control = %q, want the tag %q", e.Compliance.Control, tc.finding.Tag)
			}
		})
	}
}

func TestWriteOCSF_RequirementsCarryThePolicyRule(t *testing.T) {
	f := failed("i-1", "aws_instance", "111", tagEnv, types.ReasonInvalidValue)
	f.Expected = "one of: dev, staging, prod"
	f.Actual = "production"

	e := decodeOCSF(t, scanOf(f))[0]

	if len(e.Compliance.Requirements) != 1 || e.Compliance.Requirements[0] != f.Expected {
		t.Errorf("compliance.requirements = %v, want [%q]", e.Compliance.Requirements, f.Expected)
	}
	if e.Unmapped.Expected != f.Expected || e.Unmapped.Actual != "production" || e.Unmapped.Reason != "invalid_value" {
		t.Errorf("unmapped = %+v, want expected/actual/reason preserved", e.Unmapped)
	}
	if e.Remediation == nil || !bytes.Contains([]byte(e.Remediation.Desc), []byte(f.Expected)) {
		t.Errorf("remediation = %+v, want it to quote the rule", e.Remediation)
	}
}

// The finding identity must match the SARIF fingerprint so both outputs point
// at the same finding.
func TestWriteOCSF_UIDMatchesSARIFFingerprint(t *testing.T) {
	f := failed("i-1", "aws_instance", "111", tagOwner, types.ReasonMissing)
	f.Resource.ARN = "arn:aws:ec2:us-east-1:111:instance/i-1"

	e := decodeOCSF(t, scanOf(f))[0]

	want := f.Resource.Identity() + "#" + tagOwner
	if e.FindingInfo.UID != want {
		t.Errorf("finding_info.uid = %q, want %q", e.FindingInfo.UID, want)
	}
	if e.FindingInfo.Analytic.Name != ruleID(tagOwner) {
		t.Errorf("analytic.name = %q, want the SARIF rule id %q", e.FindingInfo.Analytic.Name, ruleID(tagOwner))
	}
	if len(e.Observables) != 1 || e.Observables[0].TypeID != 10 || e.Observables[0].Value != f.Resource.ARN {
		t.Errorf("observables = %+v, want one Resource UID observable with the ARN", e.Observables)
	}
}

func TestWriteOCSF_CloudAndResource(t *testing.T) {
	f := passed("i-1", "aws_instance", "111", tagOwner)
	f.Resource.Name = "web-1"
	f.Resource.Tags = map[string]string{"owner": "a@b.c", "environment": "prod"}

	e := decodeOCSF(t, scanOf(f))[0]

	if e.Cloud.Provider != "AWS" || e.Cloud.Region != "us-east-1" {
		t.Errorf("cloud = %+v, want provider AWS in us-east-1", e.Cloud)
	}
	if e.Cloud.Account == nil || e.Cloud.Account.UID != "111" || e.Cloud.Account.TypeID != 10 {
		t.Errorf("cloud.account = %+v, want uid 111 of type AWS Account (10)", e.Cloud.Account)
	}

	r := e.Resources[0]
	if r.Name != "web-1" || r.Type != "aws_instance" || r.Region != "us-east-1" {
		t.Errorf("resource = %+v", r)
	}
	if len(r.Tags) != 2 || r.Tags[0].Name != "environment" || r.Tags[1].Name != "owner" {
		t.Errorf("resource.tags = %+v, want the tags sorted by name", r.Tags)
	}
}

// A Terraform plan has no account yet; the account object must be omitted
// rather than emitted empty, since the schema requires a name or uid on it.
func TestWriteOCSF_OmitsUnknownAccount(t *testing.T) {
	f := failed("aws_instance.web", "aws_instance", "", tagOwner, types.ReasonMissing)
	f.Resource.Region = ""

	var buf bytes.Buffer
	if err := WriteOCSF(&buf, scanOf(f), OCSFOptions{}); err != nil {
		t.Fatalf("WriteOCSF() error = %v", err)
	}

	var raw []struct {
		Cloud map[string]json.RawMessage `json:"cloud"`
	}
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := raw[0].Cloud["account"]; ok {
		t.Error("cloud.account was emitted for a resource with no account")
	}
	if _, ok := raw[0].Cloud["region"]; ok {
		t.Error("cloud.region was emitted for a resource with no region")
	}
}

func TestWriteOCSF_ReadsLegacyViolations(t *testing.T) {
	scan := scanOf()
	scan.Violations = []types.Violation{{
		Resource: resource("i-1", "aws_instance", "111"),
		Tag:      tagOwner,
		Status:   types.StatusFailed,
		Reason:   types.ReasonMissing,
	}}

	events := decodeOCSF(t, scan)

	if len(events) != 1 || events[0].Compliance.StatusID != 3 {
		t.Fatalf("events = %+v, want one failed event from the legacy violation", events)
	}
}

func TestWriteOCSF_EmptyScanIsAnEmptyArray(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteOCSF(&buf, scanOf(), OCSFOptions{}); err != nil {
		t.Fatalf("WriteOCSF() error = %v", err)
	}
	if got := bytes.TrimSpace(buf.Bytes()); string(got) != "[]" {
		t.Errorf("output = %s, want []", got)
	}
}

package report

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/unicrons/tagctl/internal/types"
)

const (
	tagOwner = "owner"
	tagEnv   = "environment"
)

func resource(id, resType, account string) types.Resource {
	return types.Resource{
		ID:       id,
		Type:     resType,
		Account:  account,
		Region:   "us-east-1",
		Provider: "aws",
	}
}

func failed(id, resType, account, tag string, reason types.ViolationReason) types.Finding {
	return types.Finding{
		Resource: resource(id, resType, account),
		Tag:      tag,
		Status:   types.StatusFailed,
		Reason:   reason,
	}
}

func passed(id, resType, account, tag string) types.Finding {
	return types.Finding{
		Resource: resource(id, resType, account),
		Tag:      tag,
		Status:   types.StatusPass,
		Reason:   types.ReasonCompliant,
	}
}

func scanOf(findings ...types.Finding) *types.ScanResult {
	return &types.ScanResult{
		ScannedAt: time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC),
		Findings:  findings,
	}
}

// --- SARIF ---

func TestWriteSARIF_Structure(t *testing.T) {
	scan := scanOf(
		failed("i-1", "aws_instance", "111", tagOwner, types.ReasonMissing),
		failed("i-2", "aws_instance", "111", tagEnv, types.ReasonInvalidValue),
		passed("i-3", "aws_s3_bucket", "111", tagOwner),
	)

	var buf bytes.Buffer
	if err := WriteSARIF(&buf, scan, SARIFOptions{Version: "1.2.3"}); err != nil {
		t.Fatalf("WriteSARIF() error = %v", err)
	}

	var log SARIFLog
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if log.Version != "2.1.0" {
		t.Errorf("version = %q, want 2.1.0", log.Version)
	}
	if log.Schema == "" {
		t.Error("$schema is empty; GitHub validates uploads against it")
	}
	if len(log.Runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(log.Runs))
	}

	run := log.Runs[0]
	if run.Tool.Driver.Name != "tagctl" {
		t.Errorf("driver name = %q, want tagctl", run.Tool.Driver.Name)
	}
	if run.Tool.Driver.Version != "1.2.3" {
		t.Errorf("driver version = %q, want 1.2.3", run.Tool.Driver.Version)
	}

	// Only failures become results; the passing finding must not appear.
	if len(run.Results) != 2 {
		t.Fatalf("got %d results, want 2 (failures only)", len(run.Results))
	}

	// One rule per failing tag.
	if len(run.Tool.Driver.Rules) != 2 {
		t.Fatalf("got %d rules, want 2", len(run.Tool.Driver.Rules))
	}
	if run.Tool.Driver.Rules[0].ID != ruleID(tagEnv) {
		t.Errorf("rules are not sorted: first = %q", run.Tool.Driver.Rules[0].ID)
	}
}

// Every result must reference a rule the driver declares, or GitHub rejects
// the upload.
func TestWriteSARIF_ResultsReferenceDeclaredRules(t *testing.T) {
	scan := scanOf(
		failed("i-1", "aws_instance", "111", tagOwner, types.ReasonMissing),
		failed("i-2", "aws_instance", "222", tagEnv, types.ReasonInvalidFormat),
		failed("i-3", "aws_lb", "111", tagOwner, types.ReasonMissing),
	)

	var buf bytes.Buffer
	if err := WriteSARIF(&buf, scan, SARIFOptions{}); err != nil {
		t.Fatalf("WriteSARIF() error = %v", err)
	}

	var log SARIFLog
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	declared := make(map[string]bool)
	for _, rule := range log.Runs[0].Tool.Driver.Rules {
		declared[rule.ID] = true
	}

	for _, result := range log.Runs[0].Results {
		if !declared[result.RuleID] {
			t.Errorf("result references undeclared rule %q", result.RuleID)
		}
		if len(result.Locations) == 0 {
			t.Errorf("result for rule %q has no location", result.RuleID)
		}
		if result.Locations[0].PhysicalLocation.Region.StartLine < 1 {
			t.Error("startLine must be >= 1 for GitHub to accept the result")
		}
	}
}

// A missing required tag is an error; a bad value is a warning.
func TestSARIFLevel(t *testing.T) {
	tests := []struct {
		reason types.ViolationReason
		want   string
	}{
		{types.ReasonMissing, "error"},
		{types.ReasonInvalidValue, "warning"},
		{types.ReasonInvalidFormat, "warning"},
	}

	for _, tt := range tests {
		t.Run(string(tt.reason), func(t *testing.T) {
			if got := sarifLevel(tt.reason); got != tt.want {
				t.Errorf("sarifLevel(%v) = %q, want %q", tt.reason, got, tt.want)
			}
		})
	}
}

// Fingerprints keep an alert attached to the same resource and tag, so GitHub
// does not close and reopen it on every scan.
func TestWriteSARIF_FingerprintsAreStableAndDistinct(t *testing.T) {
	scan := scanOf(
		failed("i-1", "aws_instance", "111", tagOwner, types.ReasonMissing),
		failed("i-1", "aws_instance", "111", tagEnv, types.ReasonMissing),
		failed("i-1", "aws_instance", "222", tagOwner, types.ReasonMissing),
	)

	render := func() []SARIFResult {
		var buf bytes.Buffer
		if err := WriteSARIF(&buf, scan, SARIFOptions{}); err != nil {
			t.Fatalf("WriteSARIF() error = %v", err)
		}
		var log SARIFLog
		if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		return log.Runs[0].Results
	}

	first, second := render(), render()

	seen := make(map[string]bool)
	for i, result := range first {
		fingerprint := result.PartialFingerprints["tagctl/resourceTag"]
		if fingerprint == "" {
			t.Fatalf("result %d has no fingerprint", i)
		}
		if seen[fingerprint] {
			t.Errorf("fingerprint %q is reused across distinct findings", fingerprint)
		}
		seen[fingerprint] = true

		if second[i].PartialFingerprints["tagctl/resourceTag"] != fingerprint {
			t.Errorf("fingerprint for result %d changed between runs", i)
		}
	}
}

func TestWriteSARIF_AnchorsToPolicyFile(t *testing.T) {
	scan := scanOf(failed("i-1", "aws_instance", "111", tagOwner, types.ReasonMissing))

	var buf bytes.Buffer
	if err := WriteSARIF(&buf, scan, SARIFOptions{PolicyFile: "config/prod.yaml"}); err != nil {
		t.Fatalf("WriteSARIF() error = %v", err)
	}

	var log SARIFLog
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	uri := log.Runs[0].Results[0].Locations[0].PhysicalLocation.ArtifactLocation.URI
	if uri != "config/prod.yaml" {
		t.Errorf("artifact URI = %q, want config/prod.yaml", uri)
	}
}

func TestWriteSARIF_DefaultsPolicyFile(t *testing.T) {
	scan := scanOf(failed("i-1", "aws_instance", "111", tagOwner, types.ReasonMissing))

	var buf bytes.Buffer
	if err := WriteSARIF(&buf, scan, SARIFOptions{}); err != nil {
		t.Fatalf("WriteSARIF() error = %v", err)
	}

	if !strings.Contains(buf.String(), `"uri": "tagctl.yaml"`) {
		t.Error("default policy file should be tagctl.yaml")
	}
}

func decodeSARIF(t *testing.T, scan *types.ScanResult, opts SARIFOptions) SARIFLog {
	t.Helper()

	var buf bytes.Buffer
	if err := WriteSARIF(&buf, scan, opts); err != nil {
		t.Fatalf("WriteSARIF() error = %v", err)
	}
	var log SARIFLog
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	return log
}

func TestWriteSARIF_AutomationDetailsNameTheCommand(t *testing.T) {
	scan := scanOf(failed("i-1", "aws_instance", "111", tagOwner, types.ReasonMissing))

	for _, command := range []string{"scan", "evaluate", "terraform"} {
		run := decodeSARIF(t, scan, SARIFOptions{Command: command}).Runs[0]
		if want := "tagctl/" + command + "/"; run.AutomationDetails == nil || run.AutomationDetails.ID != want {
			t.Errorf("automationDetails for %s = %+v, want id %q", command, run.AutomationDetails, want)
		}
	}

	var buf bytes.Buffer
	if err := WriteSARIF(&buf, scan, SARIFOptions{}); err != nil {
		t.Fatalf("WriteSARIF() error = %v", err)
	}
	if strings.Contains(buf.String(), "automationDetails") {
		t.Error("automationDetails written without a command")
	}
}

func TestWriteSARIF_InvocationReportsPartialScans(t *testing.T) {
	const discoveryErr = "provider aws: ec2 in eu-west-1: AccessDenied"

	complete := scanOf(failed("i-1", "aws_instance", "111", tagOwner, types.ReasonMissing))
	partial := scanOf(failed("i-1", "aws_instance", "111", tagOwner, types.ReasonMissing))
	partial.Partial = true
	partial.Errors = []string{discoveryErr}

	tests := []struct {
		name              string
		scan              *types.ScanResult
		wantSuccessful    bool
		wantNotifications []string
	}{
		{name: "complete scan", scan: complete, wantSuccessful: true},
		{name: "partial scan", scan: partial, wantSuccessful: false, wantNotifications: []string{discoveryErr}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invocations := decodeSARIF(t, tt.scan, SARIFOptions{}).Runs[0].Invocations
			if len(invocations) != 1 {
				t.Fatalf("got %d invocations, want 1", len(invocations))
			}
			invocation := invocations[0]
			if invocation.ExecutionSuccessful != tt.wantSuccessful {
				t.Errorf("executionSuccessful = %v, want %v", invocation.ExecutionSuccessful, tt.wantSuccessful)
			}
			if len(invocation.ToolExecutionNotifications) != len(tt.wantNotifications) {
				t.Fatalf("notifications = %+v, want %q", invocation.ToolExecutionNotifications, tt.wantNotifications)
			}
			for i, want := range tt.wantNotifications {
				got := invocation.ToolExecutionNotifications[i]
				if got.Level != "error" || got.Message.Text != want {
					t.Errorf("notification %d = %+v, want an error with %q", i, got, want)
				}
			}
		})
	}
}

// A clean scan must still produce a valid document, or CI uploads break.
func TestWriteSARIF_EmptyScanIsValid(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteSARIF(&buf, scanOf(), SARIFOptions{}); err != nil {
		t.Fatalf("WriteSARIF() error = %v", err)
	}

	var log SARIFLog
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatalf("invalid JSON for an empty scan: %v", err)
	}
	if len(log.Runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(log.Runs))
	}
	if len(log.Runs[0].Results) != 0 {
		t.Errorf("got %d results for a clean scan, want 0", len(log.Runs[0].Results))
	}
}

func TestWriteSARIF_ReadsLegacyViolations(t *testing.T) {
	scan := &types.ScanResult{
		Violations: []types.Violation{
			{
				Resource: resource("i-1", "aws_instance", "111"),
				Tag:      tagOwner,
				Status:   types.StatusFailed,
				Reason:   types.ReasonMissing,
			},
		},
	}

	var buf bytes.Buffer
	if err := WriteSARIF(&buf, scan, SARIFOptions{}); err != nil {
		t.Fatalf("WriteSARIF() error = %v", err)
	}

	var log SARIFLog
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(log.Runs[0].Results) != 1 {
		t.Errorf("got %d results from a legacy scan, want 1", len(log.Runs[0].Results))
	}
}

// --- JUnit ---

func TestWriteJUnit_Structure(t *testing.T) {
	scan := scanOf(
		failed("i-1", "aws_instance", "111", tagOwner, types.ReasonMissing),
		passed("i-2", "aws_instance", "111", tagOwner),
		failed("i-3", "aws_lb", "111", tagEnv, types.ReasonInvalidValue),
	)

	var buf bytes.Buffer
	if err := WriteJUnit(&buf, scan); err != nil {
		t.Fatalf("WriteJUnit() error = %v", err)
	}

	if !strings.HasPrefix(buf.String(), xml.Header) {
		t.Error("output must start with the XML header")
	}

	var suites JUnitTestSuites
	if err := xml.Unmarshal(buf.Bytes(), &suites); err != nil {
		t.Fatalf("output is not valid XML: %v", err)
	}

	// Passing findings are reported too, so the ratio is truthful.
	if suites.Tests != 3 {
		t.Errorf("tests = %d, want 3 (passes included)", suites.Tests)
	}
	if suites.Failures != 2 {
		t.Errorf("failures = %d, want 2", suites.Failures)
	}

	// One suite per tag, sorted.
	if len(suites.Suites) != 2 {
		t.Fatalf("got %d suites, want 2", len(suites.Suites))
	}
	if suites.Suites[0].Name != "tag: "+tagEnv {
		t.Errorf("first suite = %q, want the environment tag (sorted first)", suites.Suites[0].Name)
	}

	ownerSuite := suites.Suites[1]
	if ownerSuite.Tests != 2 || ownerSuite.Failures != 1 {
		t.Errorf("owner suite tests/failures = %d/%d, want 2/1", ownerSuite.Tests, ownerSuite.Failures)
	}
}

func TestWriteJUnit_FailureCarriesContext(t *testing.T) {
	finding := failed("i-1", "aws_instance", "111", tagEnv, types.ReasonInvalidValue)
	finding.Expected = "dev, staging, prod"
	finding.Actual = "Production"
	finding.Resource.ARN = "arn:aws:ec2:us-east-1:111:instance/i-1"

	var buf bytes.Buffer
	if err := WriteJUnit(&buf, scanOf(finding)); err != nil {
		t.Fatalf("WriteJUnit() error = %v", err)
	}

	var suites JUnitTestSuites
	if err := xml.Unmarshal(buf.Bytes(), &suites); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}

	failure := suites.Suites[0].Cases[0].Failure
	if failure == nil {
		t.Fatal("failing finding produced no <failure> element")
	}
	if failure.Type != string(types.ReasonInvalidValue) {
		t.Errorf("failure type = %q, want %q", failure.Type, types.ReasonInvalidValue)
	}
	for _, want := range []string{"expected: dev, staging, prod", "actual: Production", "arn:aws:ec2:"} {
		if !strings.Contains(failure.Content, want) {
			t.Errorf("failure detail is missing %q\ngot:\n%s", want, failure.Content)
		}
	}
}

func TestWriteJUnit_PassingCaseHasNoFailure(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJUnit(&buf, scanOf(passed("i-1", "aws_instance", "111", tagOwner))); err != nil {
		t.Fatalf("WriteJUnit() error = %v", err)
	}

	var suites JUnitTestSuites
	if err := xml.Unmarshal(buf.Bytes(), &suites); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}
	if suites.Suites[0].Cases[0].Failure != nil {
		t.Error("passing case must not carry a <failure> element")
	}
	if suites.Failures != 0 {
		t.Errorf("failures = %d, want 0", suites.Failures)
	}
}

func TestWriteJUnit_EmptyScanIsValid(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJUnit(&buf, scanOf()); err != nil {
		t.Fatalf("WriteJUnit() error = %v", err)
	}

	var suites JUnitTestSuites
	if err := xml.Unmarshal(buf.Bytes(), &suites); err != nil {
		t.Fatalf("invalid XML for an empty scan: %v", err)
	}
	if suites.Tests != 0 || suites.Failures != 0 {
		t.Errorf("empty scan reported %d tests / %d failures, want 0/0", suites.Tests, suites.Failures)
	}
}

func TestJUnitClassName(t *testing.T) {
	tests := []struct {
		name                      string
		provider, account, region string
		want                      string
	}{
		{name: "regional resource", provider: "aws", account: "111", region: "us-east-1", want: "aws.111.us-east-1"},
		{name: "global resource", provider: "aws", account: "111", want: "aws.111"},
		{name: "terraform resource", provider: "aws", want: "aws"},
		{name: "region without account", provider: "aws", region: "us-east-1", want: "aws.us-east-1"},
		{name: "nothing known", want: "tagctl"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finding := types.Finding{Resource: types.Resource{Provider: tt.provider, Account: tt.account, Region: tt.region}}
			if got := junitClassName(finding); got != tt.want {
				t.Errorf("junitClassName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- Gate ---

func TestGate_FailUnder(t *testing.T) {
	scan := &types.ScanResult{CompliancePct: 62.0}

	failing := Gate{FailUnder: 80}.Evaluate(scan, nil)
	if failing.Passed {
		t.Error("gate passed at 62% with a floor of 80%")
	}
	if failing.Error() == nil {
		t.Error("Error() = nil for a failed gate")
	}
	if !strings.Contains(failing.Reasons[0], "62.0%") {
		t.Errorf("reason should name the actual value, got %q", failing.Reasons[0])
	}

	passing := Gate{FailUnder: 60}.Evaluate(scan, nil)
	if !passing.Passed {
		t.Errorf("gate failed at 62%% with a floor of 60%%: %v", passing.Reasons)
	}
	if passing.Error() != nil {
		t.Errorf("Error() = %v for a passing gate", passing.Error())
	}
}

// A floor of exactly the current compliance passes: the bar is "at least".
func TestGate_FailUnderIsInclusive(t *testing.T) {
	result := Gate{FailUnder: 80}.Evaluate(&types.ScanResult{CompliancePct: 80}, nil)
	if !result.Passed {
		t.Error("gate failed when compliance exactly meets the floor")
	}
}

func TestGate_FailOnNew(t *testing.T) {
	scan := &types.ScanResult{CompliancePct: 50}
	regressed := &types.DiffResult{
		Regressions: []types.Finding{failed("i-1", "aws_instance", "111", tagOwner, types.ReasonMissing)},
	}
	clean := &types.DiffResult{}

	if result := (Gate{FailOnNew: true}).Evaluate(scan, regressed); result.Passed {
		t.Error("gate passed despite a regression")
	}
	if result := (Gate{FailOnNew: true}).Evaluate(scan, clean); !result.Passed {
		t.Errorf("gate failed with no regressions: %v", result.Reasons)
	}
}

func TestGate_FailOnNewWithoutBaselineFailsClosed(t *testing.T) {
	result := Gate{FailOnNew: true}.Evaluate(&types.ScanResult{CompliancePct: 100}, nil)
	if result.Passed {
		t.Fatal("gate passed with no baseline to compare against")
	}
	if err := result.Error(); err == nil || !strings.Contains(err.Error(), "no baseline") {
		t.Errorf("Error() = %v, want it to name the missing baseline", err)
	}
}

func TestGate_NilDiffIsIgnoredWithoutFailOnNew(t *testing.T) {
	if result := (Gate{FailUnder: 50}).Evaluate(&types.ScanResult{CompliancePct: 80}, nil); !result.Passed {
		t.Errorf("gate failed without FailOnNew: %v", result.Reasons)
	}
}

func TestGate_BothChecksReported(t *testing.T) {
	scan := &types.ScanResult{CompliancePct: 40}
	diff := &types.DiffResult{
		Regressions: []types.Finding{failed("i-1", "aws_instance", "111", tagOwner, types.ReasonMissing)},
	}

	result := Gate{FailUnder: 90, FailOnNew: true}.Evaluate(scan, diff)
	if result.Passed {
		t.Fatal("gate passed with both checks failing")
	}
	if len(result.Reasons) != 2 {
		t.Errorf("got %d reasons, want both checks reported: %v", len(result.Reasons), result.Reasons)
	}
	if err := result.Error(); err == nil || !strings.Contains(err.Error(), ";") {
		t.Errorf("Error() should join both reasons, got %v", err)
	}
}

func TestGate_IsEnabled(t *testing.T) {
	if (Gate{}).IsEnabled() {
		t.Error("an empty gate reports itself as enabled")
	}
	if !(Gate{FailUnder: 1}).IsEnabled() {
		t.Error("FailUnder should enable the gate")
	}
	if !(Gate{FailOnNew: true}).IsEnabled() {
		t.Error("FailOnNew should enable the gate")
	}
}

// A disabled gate must never fail, whatever the scan says.
func TestGate_DisabledAlwaysPasses(t *testing.T) {
	scan := &types.ScanResult{CompliancePct: 0}
	diff := &types.DiffResult{
		Regressions: []types.Finding{failed("i-1", "aws_instance", "111", tagOwner, types.ReasonMissing)},
	}

	if result := (Gate{}).Evaluate(scan, diff); !result.Passed {
		t.Errorf("disabled gate failed: %v", result.Reasons)
	}
}

func TestDescribeResource(t *testing.T) {
	tests := []struct {
		name     string
		resource types.Resource
		want     string
	}{
		{
			name:     "live resource names its type",
			resource: types.Resource{ID: "i-1", Type: "aws_instance"},
			want:     "aws_instance i-1",
		},
		{
			name:     "terraform address already starts with the type",
			resource: types.Resource{ID: "aws_s3_bucket.logs", Type: "aws_s3_bucket"},
			want:     "aws_s3_bucket.logs",
		},
		{
			name:     "no type",
			resource: types.Resource{ID: "i-1"},
			want:     "i-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := describeResource(tt.resource); got != tt.want {
				t.Errorf("describeResource() = %q, want %q", got, tt.want)
			}
		})
	}
}

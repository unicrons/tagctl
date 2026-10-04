package report

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/unicrons/tagctl/internal/types"
)

func forbiddenFinding() types.Finding {
	f := failed("i-1", "aws_instance", "111", "Env", types.ReasonForbidden)
	f.Actual = "prod"
	return f
}

func TestWriteSARIF_ForbiddenTagHasItsOwnRule(t *testing.T) {
	var buf bytes.Buffer
	scan := scanOf(forbiddenFinding(), failed("i-1", "aws_instance", "111", tagOwner, types.ReasonMissing))
	if err := WriteSARIF(&buf, scan, SARIFOptions{}); err != nil {
		t.Fatalf("WriteSARIF() error = %v", err)
	}
	var log SARIFLog
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatal(err)
	}

	run := log.Runs[0]
	if len(run.Tool.Driver.Rules) != 2 || len(run.Results) != 2 {
		t.Fatalf("rules = %d, results = %d, want 2 and 2", len(run.Tool.Driver.Rules), len(run.Results))
	}
	rule := run.Tool.Driver.Rules[0]
	if rule.ID != "tagctl/forbidden-tag/Env" || rule.Name != "ForbiddenTag" || strings.Contains(rule.FullDescription.Text, "requires") {
		t.Errorf("rule = %+v, want a forbidden-tag rule that does not say the tag is required", rule)
	}
	result := run.Results[0]
	if result.RuleID != rule.ID || result.Level != "warning" || !strings.Contains(result.Message.Text, "tag 'Env' is forbidden") {
		t.Errorf("result = %+v", result)
	}
}

func TestWriteOCSF_ForbiddenTag(t *testing.T) {
	e := decodeOCSF(t, scanOf(forbiddenFinding()))[0]

	if e.Compliance.StatusID != 3 || e.SeverityID != 2 {
		t.Errorf("compliance status = %d, severity = %d, want Fail and Low", e.Compliance.StatusID, e.SeverityID)
	}
	if e.FindingInfo.Analytic.UID != "tagctl/forbidden-tag/Env" {
		t.Errorf("analytic.uid = %q, want the SARIF forbidden rule id", e.FindingInfo.Analytic.UID)
	}
	if e.Remediation == nil || !strings.HasPrefix(e.Remediation.Desc, "Remove tag 'Env' from ") {
		t.Errorf("remediation = %+v, want one that removes the tag", e.Remediation)
	}
	if got := e.Compliance.StatusDetails; len(got) != 1 || got[0] != "tag 'Env' is forbidden" {
		t.Errorf("status_details = %v", got)
	}
}

func TestWriteJUnit_ForbiddenTag(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJUnit(&buf, scanOf(forbiddenFinding())); err != nil {
		t.Fatalf("WriteJUnit() error = %v", err)
	}
	if err := xml.Unmarshal(buf.Bytes(), new(struct{})); err != nil {
		t.Fatalf("output is not XML: %v", err)
	}
	for _, want := range []string{`type="forbidden"`, "is forbidden"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("JUnit output lacks %q:\n%s", want, buf.String())
		}
	}
}

func TestBuildHTMLReport_ForbiddenTag(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteHTML(&buf, scanOf(forbiddenFinding()), HTMLOptions{}); err != nil {
		t.Fatalf("WriteHTML() error = %v", err)
	}
	if failureLabel(types.ReasonForbidden) != "forbidden" || !strings.Contains(buf.String(), "forbidden") {
		t.Errorf("HTML report does not label the finding as forbidden")
	}
	if failureLabel("some_future_reason") != "failed" {
		t.Errorf("an unknown reason must still render as a failure")
	}
}

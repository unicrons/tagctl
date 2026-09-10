package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/unicrons/tagctl/internal/types"
)

func withValue(f types.Finding, value string) types.Finding {
	f.Actual = value
	return f
}

func TestWriteHTML_EscapesUntrustedValues(t *testing.T) {
	f := withValue(passed("i-1", "aws_instance", "123", tagEnv), `<script>alert(1)</script>`)
	f.Resource.Name = `<img src=x onerror=alert(2)>`
	f.Resource.ARN = `arn:aws:ec2:us-east-1:123:instance/i-1" onmouseover="alert(3)`
	scan := scanOf(f)
	scan.TotalResources, scan.CompliantCount = 1, 1

	var buf bytes.Buffer
	if err := WriteHTML(&buf, scan, HTMLOptions{Version: "1.2.3"}); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}
	out := buf.String()

	for _, raw := range []string{"<script>alert(1)", "<img src=x", `onmouseover="alert(3)"`} {
		if strings.Contains(out, raw) {
			t.Errorf("output contains unescaped %q", raw)
		}
	}
	for _, want := range []string{"&lt;script&gt;alert(1)", "&lt;img src=x", "v1.2.3"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

// The findings table ships its own filter and pager so a 1,000-row report
// stays usable without any external asset.
func TestWriteHTML_FindingsTableHasFilterAndPager(t *testing.T) {
	scan := scanOf(
		passed("i-1", "aws_instance", "123", tagEnv),
		failed("i-2", "aws_instance", "123", tagEnv, types.ReasonMissing),
	)

	var buf bytes.Buffer
	if err := WriteHTML(&buf, scan, HTMLOptions{}); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}
	out := buf.String()

	for _, want := range []string{`data-f="fail"`, `<div class="pager" data-pager>`, `data-size`, `data-prev`, `data-next`, `<option selected>50</option>`} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
	if got := strings.Count(out, `<tr data-s=`); got != 2 {
		t.Errorf("rendered %d finding rows, want 2 (every finding is in the DOM; paging is client-side)", got)
	}
}

func TestWriteHTML_NoFindingsHasNoPager(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteHTML(&buf, scanOf(), HTMLOptions{}); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}
	if strings.Contains(buf.String(), `<div class="pager" data-pager>`) {
		t.Error("pager rendered for a scan with no findings")
	}
}

func TestBuildHTMLReport_Matrix(t *testing.T) {
	scan := scanOf(
		passed("i-1", "aws_instance", "123", tagEnv),
		passed("i-1", "aws_instance", "123", tagOwner),
		passed("i-2", "aws_instance", "123", tagEnv),
		failed("i-2", "aws_instance", "123", tagOwner, types.ReasonMissing),
		failed("i-3", "aws_instance", "123", tagEnv, types.ReasonMissing),
		failed("i-3", "aws_instance", "123", tagOwner, types.ReasonMissing),
		passed("b-1", "aws_s3_bucket", "123", tagEnv),
		passed("b-1", "aws_s3_bucket", "123", tagOwner),
	)
	scan.TotalResources, scan.CompliantCount = 4, 2
	scan.ByTag = map[string]*types.TagStats{
		tagOwner: {Tag: tagOwner, Required: true, Present: 2, Missing: 2, CompliancePct: 50},
		tagEnv:   {Tag: tagEnv, Required: true, Present: 3, Missing: 1, CompliancePct: 75},
	}

	r := BuildHTMLReport(scan, HTMLOptions{})

	if got := []string{r.Tags[0].Name, r.Tags[1].Name}; got[0] != tagEnv || got[1] != tagOwner {
		t.Fatalf("tags not sorted by name: %v", got)
	}
	if len(r.Matrix) != 2 || r.Matrix[0].Type != "instance" || r.Matrix[1].Type != "s3_bucket" {
		t.Fatalf("matrix rows = %+v, want instance then s3_bucket", r.Matrix)
	}

	inst := r.Matrix[0]
	if inst.Resources != 3 || inst.Compliant != 1 {
		t.Errorf("instance row: resources=%d compliant=%d, want 3/1", inst.Resources, inst.Compliant)
	}
	env, owner := inst.Cells[0], inst.Cells[1]
	if env.Count != 2 || env.Pct != 66 || env.Level != 3 {
		t.Errorf("instance/environment cell = %+v, want count 2, pct 66, level 3", env)
	}
	if owner.Count != 1 || owner.Pct != 33 || owner.Level != 2 {
		t.Errorf("instance/owner cell = %+v, want count 1, pct 33, level 2", owner)
	}

	bucket := r.Matrix[1]
	if bucket.Compliant != 1 || bucket.Cells[0].Pct != 100 || bucket.Cells[0].Level != 4 {
		t.Errorf("s3_bucket row = %+v, want fully compliant", bucket)
	}
}

func TestBuildHTMLReport_Headline(t *testing.T) {
	cases := []struct {
		name         string
		scan         *types.ScanResult
		wantLead     string
		wantRest     string
		wantLedePart string
	}{
		{
			name:     "empty scan",
			scan:     scanOf(),
			wantRest: "No resources were scanned",
		},
		{
			name: "all compliant",
			scan: func() *types.ScanResult {
				s := scanOf(passed("i-1", "aws_instance", "123", tagEnv))
				s.TotalResources, s.CompliantCount = 1, 1
				return s
			}(),
			wantLead: "All 1 resources",
			wantRest: "carry every required tag",
		},
		{
			name: "only missing",
			scan: func() *types.ScanResult {
				s := scanOf(failed("i-1", "aws_instance", "123", tagEnv, types.ReasonMissing))
				s.TotalResources, s.CompliantCount = 2, 1
				return s
			}(),
			wantLead:     "1 of 2 resources",
			wantRest:     "are missing at least one required tag",
			wantLedePart: "the 1 gaps are tags that were never set",
		},
		{
			name: "only invalid",
			scan: func() *types.ScanResult {
				s := scanOf(failed("i-1", "aws_instance", "123", tagEnv, types.ReasonInvalidValue))
				s.TotalResources, s.CompliantCount = 2, 1
				return s
			}(),
			wantLead:     "1 of 2 resources",
			wantRest:     "carry a tag value the policy rejects",
			wantLedePart: "1 values do not match the policy",
		},
		{
			name: "missing and invalid",
			scan: func() *types.ScanResult {
				s := scanOf(
					failed("i-1", "aws_instance", "123", tagEnv, types.ReasonMissing),
					failed("i-2", "aws_instance", "123", tagEnv, types.ReasonInvalidFormat),
				)
				s.TotalResources, s.CompliantCount = 3, 1
				return s
			}(),
			wantLead:     "2 of 3 resources",
			wantRest:     "fail the tag policy",
			wantLedePart: "1 tags were never set and 1 values do not match",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := BuildHTMLReport(tc.scan, HTMLOptions{})
			if r.HeadlineLead != tc.wantLead || r.HeadlineRest != tc.wantRest {
				t.Errorf("headline = %q + %q, want %q + %q", r.HeadlineLead, r.HeadlineRest, tc.wantLead, tc.wantRest)
			}
			if !strings.Contains(r.Lede, tc.wantLedePart) {
				t.Errorf("lede = %q, want it to contain %q", r.Lede, tc.wantLedePart)
			}
		})
	}
}

func TestBuildHTMLReport_TagsFallBackToFindings(t *testing.T) {
	scan := scanOf(
		passed("i-1", "aws_instance", "123", tagEnv),
		failed("i-2", "aws_instance", "123", tagEnv, types.ReasonMissing),
		failed("i-1", "aws_instance", "123", tagOwner, types.ReasonMissing),
	)

	r := BuildHTMLReport(scan, HTMLOptions{})

	if len(r.Tags) != 2 {
		t.Fatalf("tags = %+v, want environment and owner", r.Tags)
	}
	env := r.Tags[0]
	if env.Name != tagEnv || env.Compliant != 1 || env.Total != 2 || env.Pct != 50 {
		t.Errorf("environment = %+v, want 1 of 2 (50%%)", env)
	}
}

func TestBuildHTMLReport_OptionalTagsAfterRequired(t *testing.T) {
	scan := scanOf()
	scan.ByTag = map[string]*types.TagStats{
		"a-optional": {Tag: "a-optional", Required: false},
		"z-required": {Tag: "z-required", Required: true},
	}

	r := BuildHTMLReport(scan, HTMLOptions{})

	if r.Tags[0].Name != "z-required" || r.Tags[1].Name != "a-optional" {
		t.Errorf("tags = %+v, want required first", r.Tags)
	}
}

func TestBuildHTMLReport_ReadsLegacyViolations(t *testing.T) {
	scan := scanOf()
	scan.TotalResources, scan.CompliantCount = 1, 0
	scan.Violations = []types.Violation{{
		Resource: resource("i-1", "aws_instance", "123"),
		Tag:      tagOwner,
		Status:   types.StatusFailed,
		Reason:   types.ReasonInvalidFormat,
		Actual:   "nobody",
	}}

	r := BuildHTMLReport(scan, HTMLOptions{})

	if len(r.Findings) != 1 || r.Findings[0].Status != "fail" || r.Findings[0].Label != "invalid format" {
		t.Fatalf("findings = %+v, want one invalid-format failure", r.Findings)
	}
	if r.Invalid != 1 || r.Missing != 0 {
		t.Errorf("invalid=%d missing=%d, want 1/0", r.Invalid, r.Missing)
	}
}

func TestCoverageLevel(t *testing.T) {
	cases := map[int]int{0: 0, 1: 1, 24: 1, 25: 2, 49: 2, 50: 3, 79: 3, 80: 4, 100: 4}
	for pct, want := range cases {
		if got := coverageLevel(pct); got != want {
			t.Errorf("coverageLevel(%d) = %d, want %d", pct, got, want)
		}
	}
}

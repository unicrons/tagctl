package report

import (
	"bytes"
	"strconv"
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

func TestWriteHTML_MarksPartialScan(t *testing.T) {
	const notice = `<div class="partial" role="alert">`
	scan := scanOf(passed("i-1", "aws_instance", "123", tagEnv))

	var complete bytes.Buffer
	if err := WriteHTML(&complete, scan, HTMLOptions{}); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}
	if strings.Contains(complete.String(), notice) {
		t.Error("partial notice rendered for a complete scan")
	}

	scan.Partial = true
	scan.Errors = []string{`provider aws: <b>AccessDenied</b>`}
	var partial bytes.Buffer
	if err := WriteHTML(&partial, scan, HTMLOptions{}); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}
	for _, want := range []string{notice, "<li>provider aws: &lt;b&gt;AccessDenied&lt;/b&gt;</li>"} {
		if !strings.Contains(partial.String(), want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestWriteHTML_OptionalTagNoResourceCarriesReadsNotUsed(t *testing.T) {
	scan := scanOf(failed("i-1", "aws_instance", "123", tagOwner, types.ReasonMissing))
	scan.TotalResources = 1
	scan.ByTag = map[string]*types.TagStats{
		tagOwner:  {Tag: tagOwner, Required: true, Missing: 1},
		"project": {Tag: "project", CompliancePct: 100},
	}

	var buf bytes.Buffer
	if err := WriteHTML(&buf, scan, HTMLOptions{}); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}
	out := buf.String()

	if got := strings.Count(out, `<div class="tag zero">`); got != 1 {
		t.Errorf("rendered %d zero-coverage tag chips, want 1 (the required tag only)", got)
	}
	if !strings.Contains(out, `<span>project</span><b>not used</b>`) {
		t.Error("optional tag no resource carries does not read as not used")
	}
	if strings.Contains(out, "0 of 0 tagged") {
		t.Error("optional tag no resource carries still renders a 0 of 0 count")
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

func tagged(f types.Finding, tags map[string]string) types.Finding {
	f.Resource.Tags = tags
	f.Actual = tags[f.Tag]
	return f
}

// ownerScan has four instances: two of alice (one failing environment), one of
// bob and one with no owner at all.
func ownerScan() *types.ScanResult {
	alice := map[string]string{tagOwner: "alice", tagEnv: "prod"}
	aliceNoEnv := map[string]string{tagOwner: "alice"}
	bob := map[string]string{tagOwner: "bob", tagEnv: "prod"}
	nobody := map[string]string{tagEnv: "dev"}

	scan := scanOf(
		tagged(passed("i-1", "aws_instance", "123", tagOwner), alice),
		tagged(passed("i-1", "aws_instance", "123", tagEnv), alice),
		tagged(passed("i-2", "aws_instance", "123", tagOwner), aliceNoEnv),
		tagged(failed("i-2", "aws_instance", "123", tagEnv, types.ReasonMissing), aliceNoEnv),
		tagged(passed("i-3", "aws_instance", "123", tagOwner), bob),
		tagged(passed("i-3", "aws_instance", "123", tagEnv), bob),
		tagged(failed("i-4", "aws_instance", "123", tagOwner, types.ReasonMissing), nobody),
		tagged(passed("i-4", "aws_instance", "123", tagEnv), nobody),
	)
	scan.TotalResources, scan.CompliantCount = 4, 2
	scan.ByTag = map[string]*types.TagStats{
		tagOwner: {Tag: tagOwner, Required: true, Present: 3, Missing: 1, CompliancePct: 75},
		tagEnv:   {Tag: tagEnv, Required: true, Present: 3, Missing: 1, CompliancePct: 75},
	}
	return scan
}

func ownerValues(t *testing.T, r HTMLReport) HTMLTagValues {
	t.Helper()
	for _, group := range r.ByValue {
		if group.Tag == tagOwner {
			return group
		}
	}
	t.Fatalf("no by-value group for owner in %+v", r.ByValue)
	return HTMLTagValues{}
}

func TestBuildHTMLReport_ByValueCountsResourcesAndCompliance(t *testing.T) {
	r := BuildHTMLReport(ownerScan(), HTMLOptions{})

	if len(r.ByValue) != len(r.Tags) {
		t.Fatalf("got %d by-value groups, want one per policy tag (%d)", len(r.ByValue), len(r.Tags))
	}

	want := []HTMLTagValue{
		{Label: "alice", Index: 0, Resources: 2, Compliant: 1, Pct: 50},
		{Label: "bob", Index: 1, Resources: 1, Compliant: 1, Pct: 100},
		{Label: "(untagged)", Untagged: true, Index: 2, Resources: 1, Compliant: 0, Pct: 0},
	}
	got := ownerValues(t, r).Values
	if len(got) != len(want) {
		t.Fatalf("owner values = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("owner value %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestBuildHTMLReport_ByValueOpensOnOwner(t *testing.T) {
	r := BuildHTMLReport(ownerScan(), HTMLOptions{})

	for _, group := range r.ByValue {
		if group.Selected != (group.Tag == tagOwner) {
			t.Errorf("tag %q selected = %v, want only owner selected", group.Tag, group.Selected)
		}
	}
}

func TestBuildHTMLReport_ByValueOpensOnFirstTagWithoutOwnerOrTeam(t *testing.T) {
	scan := scanOf(
		passed("i-1", "aws_instance", "123", tagEnv),
		passed("i-1", "aws_instance", "123", "project"),
	)

	r := BuildHTMLReport(scan, HTMLOptions{})

	if !r.ByValue[0].Selected || r.ByValue[1].Selected {
		t.Errorf("by-value = %+v, want only the first tag selected", r.ByValue)
	}
}

func TestBuildHTMLReport_FindingGroupsPointAtTheValueOfTheirResource(t *testing.T) {
	r := BuildHTMLReport(ownerScan(), HTMLOptions{})

	ownerAt := -1
	for i, group := range r.ByValue {
		if group.Tag == tagOwner {
			ownerAt = i
		}
	}
	owners := ownerValues(t, r).Values

	// Findings keep the scan order: two per instance, i-1 to i-4.
	wantOwner := []string{"alice", "alice", "alice", "alice", "bob", "bob", "(untagged)", "(untagged)"}
	for i, f := range r.Findings {
		parts := strings.Fields(f.Groups)
		if len(parts) != len(r.ByValue) {
			t.Fatalf("finding %d groups = %q, want one index per policy tag", i, f.Groups)
		}
		index, err := strconv.Atoi(parts[ownerAt])
		if err != nil {
			t.Fatalf("finding %d groups = %q: %v", i, f.Groups, err)
		}
		if got := owners[index].Label; got != wantOwner[i] {
			t.Errorf("finding %d filters under owner %q, want %q", i, got, wantOwner[i])
		}
	}
}

func TestBuildHTMLReport_ByValueGroupsByIdentityNotID(t *testing.T) {
	tags := map[string]string{tagOwner: "alice"}
	west := tagged(passed("log-group", "aws_cloudwatch_log_group", "123", tagOwner), tags)
	west.Resource.Region = "us-west-2"
	scan := scanOf(
		tagged(passed("log-group", "aws_cloudwatch_log_group", "123", tagOwner), tags),
		west,
	)

	r := BuildHTMLReport(scan, HTMLOptions{})

	if alice := ownerValues(t, r).Values[0]; alice.Resources != 2 || alice.Compliant != 2 {
		t.Errorf("alice = %+v, want the two same-named resources counted apart", alice)
	}
}

func TestBuildHTMLReport_ByValueReadsValueFromFindingWithoutResourceTags(t *testing.T) {
	scan := scanOf(
		withValue(passed("i-1", "aws_instance", "123", tagOwner), "alice"),
		withValue(failed("i-2", "aws_instance", "123", tagOwner, types.ReasonInvalidFormat), "nobody"),
	)

	values := ownerValues(t, BuildHTMLReport(scan, HTMLOptions{})).Values

	if len(values) != 3 || values[0].Label != "alice" || values[1].Label != "nobody" {
		t.Fatalf("values = %+v, want alice, nobody and the untagged row", values)
	}
	if values[1].Compliant != 0 {
		t.Errorf("nobody = %+v, want its failing resource not compliant", values[1])
	}
}

func TestBuildHTMLReport_ByValueKeepsAnEmptyUntaggedRow(t *testing.T) {
	scan := scanOf(tagged(passed("i-1", "aws_instance", "123", tagOwner), map[string]string{tagOwner: "alice"}))

	values := ownerValues(t, BuildHTMLReport(scan, HTMLOptions{})).Values

	last := values[len(values)-1]
	if !last.Untagged || last.Resources != 0 || last.Index != len(values)-1 {
		t.Errorf("last row = %+v, want an untagged row with no resources", last)
	}
}

func TestBuildHTMLReport_ByValueLargestGroupFirstThenByName(t *testing.T) {
	var findings []types.Finding
	for id, owner := range map[string]string{"i-1": "zoe", "i-2": "bob", "i-3": "amy", "i-4": "bob"} {
		findings = append(findings, tagged(passed(id, "aws_instance", "123", tagOwner), map[string]string{tagOwner: owner}))
	}

	values := ownerValues(t, BuildHTMLReport(scanOf(findings...), HTMLOptions{})).Values

	got := []string{values[0].Label, values[1].Label, values[2].Label}
	if got[0] != "bob" || got[1] != "amy" || got[2] != "zoe" {
		t.Errorf("order = %v, want bob (2 resources), then amy and zoe by name", got)
	}
}

func TestBuildHTMLReport_NoByValueWithoutFindings(t *testing.T) {
	if r := BuildHTMLReport(scanOf(), HTMLOptions{}); r.ByValue != nil {
		t.Errorf("by-value = %+v for a scan with no findings, want none", r.ByValue)
	}
}

func TestWriteHTML_ByValueSectionRendersSelectorAndFilterHooks(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteHTML(&buf, ownerScan(), HTMLOptions{}); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}
	out := buf.String()

	for _, want := range []string{
		`<h2 id="by-value">`,
		`<select id="by-value-tag">`,
		`<option value="1" selected>owner</option>`,
		`<table id="by-value-table">`,
		`<tbody data-tag="1" data-name="owner">`,
		`<tbody data-tag="0" data-name="environment" hidden>`,
		`<button type="button" class="val" data-v="0" aria-pressed="false">alice</button></td><td class="num">2</td><td class="num">1</td><td class="num">50.0%</td>`,
		`<button type="button" class="val none" data-v="2" aria-pressed="false">(untagged)</button></td><td class="num">1</td><td class="num">0</td><td class="num">0.0%</td>`,
		`<p class="active" id="value-filter" hidden>`,
		`<h2 id="findings">`,
		`<tr data-s="fail" data-g="1 2">`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
	for _, external := range []string{"<script src", "<link ", "http://", "https://"} {
		if strings.Contains(out, external) {
			t.Errorf("output references an external asset: %q", external)
		}
	}
}

func TestWriteHTML_ByValueEscapesTagValuesAndNames(t *testing.T) {
	const value = `<script>alert(1)</script>`
	const name = `team"><script>alert(2)</script>`
	scan := scanOf(tagged(passed("i-1", "aws_instance", "123", name), map[string]string{name: value}))

	var buf bytes.Buffer
	if err := WriteHTML(&buf, scan, HTMLOptions{}); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}
	out := buf.String()

	for _, raw := range []string{"<script>alert(1)", "<script>alert(2)"} {
		if strings.Contains(out, raw) {
			t.Errorf("output contains unescaped %q", raw)
		}
	}
	for _, want := range []string{
		`aria-pressed="false">&lt;script&gt;alert(1)&lt;/script&gt;</button>`,
		`data-name="team&#34;&gt;&lt;script&gt;alert(2)&lt;/script&gt;"`,
		`selected>team&#34;&gt;&lt;script&gt;alert(2)&lt;/script&gt;</option>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing escaped %q", want)
		}
	}
}

func TestWriteHTML_NoByValueSectionWithoutFindings(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteHTML(&buf, scanOf(), HTMLOptions{}); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}
	for _, id := range []string{`id="by-value"`, `id="by-value-table"`, `id="value-filter"`} {
		if strings.Contains(buf.String(), id) {
			t.Errorf("output has %s for a scan with no findings", id)
		}
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

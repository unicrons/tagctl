package report

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/unicrons/tagctl/internal/types"
)

func renderMarkdown(t *testing.T, scan *types.ScanResult, opts MarkdownOptions) string {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteMarkdown(&buf, scan, opts); err != nil {
		t.Fatalf("WriteMarkdown() error = %v", err)
	}
	return buf.String()
}

// tableRows returns the body rows of the table under heading, split into trimmed cells.
func tableRows(t *testing.T, markdown, heading string) [][]string {
	t.Helper()
	_, section, found := strings.Cut(markdown, "### "+heading+"\n")
	if !found {
		t.Fatalf("no %q section in:\n%s", heading, markdown)
	}

	var rows [][]string
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "<sub>") {
			break
		}
		if !strings.HasPrefix(line, "| ") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), " | ")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		rows = append(rows, cells)
	}
	if len(rows) < 2 {
		t.Fatalf("%q has no table:\n%s", heading, markdown)
	}
	return rows[2:]
}

func TestWriteMarkdown_Summary(t *testing.T) {
	invalid := failed("b-1", "aws_s3_bucket", "111", tagEnv, types.ReasonInvalidValue)
	invalid.Actual = "Prod"
	scan := scanOf(
		failed("i-1", "aws_instance", "111", tagOwner, types.ReasonMissing),
		failed("i-1", "aws_instance", "111", tagEnv, types.ReasonMissing),
		failed("i-2", "aws_instance", "111", tagOwner, types.ReasonMissing),
		invalid,
		passed("b-2", "aws_s3_bucket", "111", tagOwner),
	)
	scan.TotalResources, scan.CompliantCount, scan.CompliancePct = 4, 1, 25
	scan.ByTag = map[string]*types.TagStats{
		tagOwner: {Tag: tagOwner, Required: true, Present: 2, Missing: 2, CompliancePct: 50},
		tagEnv:   {Tag: tagEnv, Present: 1, Invalid: 1, CompliancePct: 0},
	}

	got := renderMarkdown(t, scan, MarkdownOptions{Version: "1.2.3"})

	for _, want := range []string{
		"## Tag compliance\n",
		"**25.0% compliant**: 1 of 4 resources pass every tag check, 4 failed findings.",
		"<sub>tagctl 1.2.3, scanned 2026-02-03T10:00:00Z</sub>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Partial scan") || strings.Contains(got, "Showing") {
		t.Errorf("summary of a complete, short scan mentions a partial scan or a cut:\n%s", got)
	}

	wantTags := [][]string{
		{tagEnv, "optional", "0.0%", "1", "0", "1"},
		{tagOwner, "required", "50.0%", "2", "2", "0"},
	}
	if rows := tableRows(t, got, "Tags"); fmt.Sprint(rows) != fmt.Sprint(wantTags) {
		t.Errorf("tag rows = %v, want %v (worst first)", rows, wantTags)
	}

	wantTypes := [][]string{
		{`aws\_instance`, "2", "3"},
		{`aws\_s3\_bucket`, "1", "1"},
	}
	if rows := tableRows(t, got, "Top failing resource types"); fmt.Sprint(rows) != fmt.Sprint(wantTypes) {
		t.Errorf("type rows = %v, want %v", rows, wantTypes)
	}

	findings := tableRows(t, got, "Failed findings")
	if len(findings) != 4 {
		t.Fatalf("got %d failed finding rows, want 4:\n%s", len(findings), got)
	}
	wantLast := []string{"aws/111/us-east-1/b-1", `aws\_s3\_bucket`, tagEnv, `invalid\_value: Prod`}
	if fmt.Sprint(findings[3]) != fmt.Sprint(wantLast) {
		t.Errorf("last finding row = %v, want %v", findings[3], wantLast)
	}
}

func TestWriteMarkdown_CountsFailingResourcesByIdentity(t *testing.T) {
	east := failed("/aws/lambda/shared", "aws_cloudwatch_log_group", "111", tagOwner, types.ReasonMissing)
	west := east
	west.Resource.Region = "us-west-2"
	eastAgain := east
	eastAgain.Tag = tagEnv

	got := renderMarkdown(t, scanOf(east, west, eastAgain), MarkdownOptions{})

	want := [][]string{{`aws\_cloudwatch\_log\_group`, "2", "3"}}
	if rows := tableRows(t, got, "Top failing resource types"); fmt.Sprint(rows) != fmt.Sprint(want) {
		t.Errorf("type rows = %v, want %v: the same name in two regions is two resources", rows, want)
	}
}

func TestWriteMarkdown_LimitsTheTables(t *testing.T) {
	findings := make([]types.Finding, 0, 30)
	for i := range 30 {
		findings = append(findings, failed(fmt.Sprintf("r-%02d", i), fmt.Sprintf("aws_type_%02d", i%12), "111", tagOwner, types.ReasonMissing))
	}
	scan := scanOf(findings...)

	t.Run("defaults", func(t *testing.T) {
		got := renderMarkdown(t, scan, MarkdownOptions{})

		if rows := tableRows(t, got, "Failed findings"); len(rows) != 25 || rows[0][0] != "aws/111/us-east-1/r-00" {
			t.Errorf("got %d finding rows starting at %v, want the first 25", len(rows), rows[0])
		}
		if rows := tableRows(t, got, "Top failing resource types"); len(rows) != 10 {
			t.Errorf("got %d type rows, want 10", len(rows))
		}
		for _, want := range []string{"Showing the first 25 of 30 failed findings.", "Showing 10 of 12 resource types with failures."} {
			if !strings.Contains(got, want) {
				t.Errorf("summary lacks %q", want)
			}
		}
	})

	t.Run("options", func(t *testing.T) {
		got := renderMarkdown(t, scan, MarkdownOptions{MaxFindings: 3, MaxTypes: 2})

		if rows := tableRows(t, got, "Failed findings"); len(rows) != 3 {
			t.Errorf("got %d finding rows, want 3", len(rows))
		}
		if rows := tableRows(t, got, "Top failing resource types"); len(rows) != 2 {
			t.Errorf("got %d type rows, want 2", len(rows))
		}
	})
}

func TestWriteMarkdown_CompliantScanHasNoFailureTables(t *testing.T) {
	scan := scanOf(passed("i-1", "aws_instance", "111", tagOwner))
	scan.TotalResources, scan.CompliantCount, scan.CompliancePct = 1, 1, 100

	got := renderMarkdown(t, scan, MarkdownOptions{})

	if !strings.Contains(got, "**100.0% compliant**: 1 of 1 resources pass every tag check, 0 failed findings.") {
		t.Errorf("headline missing:\n%s", got)
	}
	if strings.Contains(got, "###") {
		t.Errorf("a compliant scan without tag stats has sections:\n%s", got)
	}
}

func TestWriteMarkdown_EmptyAndPartialScans(t *testing.T) {
	scan := scanOf()
	scan.Partial, scan.Errors = true, []string{"provider aws: AccessDenied | <b>x</b>"}

	got := renderMarkdown(t, scan, MarkdownOptions{})

	if !strings.Contains(got, "No resources were evaluated.") {
		t.Errorf("empty scan headline missing:\n%s", got)
	}
	if !strings.Contains(got, "> **Partial scan**: discovery failed for part of the estate (1 error)") {
		t.Errorf("partial scan notice missing:\n%s", got)
	}
	if strings.Contains(got, "AccessDenied") {
		t.Errorf("summary repeats raw discovery errors:\n%s", got)
	}
}

func TestWriteMarkdown_ReadsFailuresFromLegacyViolations(t *testing.T) {
	scan := scanOf()
	scan.Violations = []types.Violation{{Resource: resource("i-1", "aws_instance", "111"), Tag: tagOwner, Reason: types.ReasonMissing}}

	got := renderMarkdown(t, scan, MarkdownOptions{})

	if rows := tableRows(t, got, "Failed findings"); len(rows) != 1 || rows[0][3] != "missing" {
		t.Errorf("finding rows = %v, want the legacy violation", rows)
	}
}

func TestWriteMarkdown_HostileValuesCannotBreakTheTable(t *testing.T) {
	hostile := failed("id|with`pipes`\nand\r\nnewlines", "aws_instance", "111", "ow|ner", types.ReasonInvalidValue)
	hostile.Resource.Region = ""
	hostile.Actual = "<img src=x> | [link](http://x) **bold** \x1b[31mred\x00\u202e &amp; ~~s~~ back\\slash"
	scan := scanOf(hostile)
	scan.ByTag = map[string]*types.TagStats{"ow|ner": {Tag: "ow|ner\n| injected | row |", Required: true}}

	got := renderMarkdown(t, scan, MarkdownOptions{Version: "1|0`"})

	for _, r := range got {
		if r != '\n' && (r < 0x20 || r == 0x7f || r == 0x202e) {
			t.Fatalf("summary contains control character %U:\n%q", r, got)
		}
	}
	separators := 0
	for _, line := range strings.Split(got, "\n") {
		if !strings.HasPrefix(line, "|") {
			separators = 0
			continue
		}
		count := strings.Count(strings.ReplaceAll(strings.ReplaceAll(line, `\\`, ""), `\|`, ""), "|")
		if separators == 0 {
			separators = count
		}
		if count != separators {
			t.Errorf("row has %d cell separators, its header has %d: %s", count, separators, line)
		}
	}

	findings := tableRows(t, got, "Failed findings")
	if len(findings) != 1 {
		t.Fatalf("hostile finding rendered as %d rows:\n%s", len(findings), got)
	}
	wantReason := `invalid\_value: \<img src=x\> \| \[link\](http://x) \*\*bold\*\* \[31mred &amp;amp; \~\~s\~\~ back\\slash`
	row := strings.Join(findings[0], " | ")
	if !strings.HasSuffix(row, wantReason) {
		t.Errorf("reason cell = %q, want suffix %q", row, wantReason)
	}
	if !strings.HasPrefix(row, "aws/111/id\\|with\\`pipes\\` and  newlines | ") {
		t.Errorf("resource cell not escaped: %q", row)
	}
	if !strings.Contains(got, "<sub>tagctl 1\\|0\\`") {
		t.Errorf("version not escaped:\n%s", got)
	}
}

func TestMarkdownCell(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "plain", value: "arn:aws:s3:::bucket", want: "arn:aws:s3:::bucket"},
		{name: "pipe", value: "a|b", want: `a\|b`},
		{name: "backtick", value: "a`b`", want: "a\\`b\\`"},
		{name: "backslash before pipe", value: `a\|b`, want: `a\\\|b`},
		{name: "newlines and tabs become spaces", value: "a\nb\r\nc\td", want: "a b  c d"},
		{name: "control characters are stripped", value: "a\x00b\x1bc\x7fd", want: "abcd"},
		{name: "bidi and zero-width characters are stripped", value: "a\u202eb\u200bc", want: "abc"},
		{name: "html", value: "<b>&", want: `\<b\>&amp;`},
		{name: "emphasis and links", value: "*a* _b_ [c] ~d~", want: `\*a\* \_b\_ \[c\] \~d\~`},
		{name: "surrounding space is trimmed", value: "  a  ", want: "a"},
		{name: "non-ASCII is kept", value: "equipo-ñ-日本", want: "equipo-ñ-日本"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := markdownCell(tt.value); got != tt.want {
				t.Errorf("markdownCell(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWriteMarkdown_ReturnsWriteErrors(t *testing.T) {
	if err := WriteMarkdown(failingWriter{}, scanOf(), MarkdownOptions{}); err == nil {
		t.Error("WriteMarkdown() error = nil on a failing writer")
	}
}

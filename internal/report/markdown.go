package report

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/unicrons/tagctl/internal/types"
)

const (
	defaultMarkdownFindings = 25
	defaultMarkdownTypes    = 10
)

// MarkdownOptions configures the Markdown summary.
type MarkdownOptions struct {
	// Version is the tagctl version printed in the summary.
	Version string

	// MaxFindings caps the failed findings table; 0 means 25.
	MaxFindings int

	// MaxTypes caps the failing resource types table; 0 means 10.
	MaxTypes int
}

// WriteMarkdown renders a scan as a GitHub-flavoured Markdown summary.
func WriteMarkdown(w io.Writer, scan *types.ScanResult, opts MarkdownOptions) error {
	if opts.MaxFindings <= 0 {
		opts.MaxFindings = defaultMarkdownFindings
	}
	if opts.MaxTypes <= 0 {
		opts.MaxTypes = defaultMarkdownTypes
	}
	failed := scan.FailedFindings()

	out := bufio.NewWriter(w)
	writeMarkdownHeadline(out, scan, len(failed))
	writeMarkdownTags(out, scan)
	writeMarkdownTypes(out, failed, opts.MaxTypes)
	writeMarkdownFindings(out, failed, opts.MaxFindings)
	writeMarkdownFooter(out, scan, opts.Version)
	return out.Flush()
}

func writeMarkdownHeadline(out *bufio.Writer, scan *types.ScanResult, failed int) {
	fmt.Fprint(out, "## Tag compliance\n\n")

	if scan.TotalResources == 0 {
		fmt.Fprint(out, "No resources were evaluated.\n")
	} else {
		fmt.Fprintf(out, "**%s compliant**: %d of %d resources pass every tag check, %d failed %s.\n",
			markdownPct(scan.CompliancePct), scan.CompliantCount, scan.TotalResources, failed, plural(failed, "finding"))
	}

	if scan.Partial {
		fmt.Fprintf(out, "\n> **Partial scan**: discovery failed for part of the estate (%d %s), so resources may be missing from these numbers.\n",
			len(scan.Errors), plural(len(scan.Errors), "error"))
	}
}

func writeMarkdownTags(out *bufio.Writer, scan *types.ScanResult) {
	if len(scan.ByTag) == 0 {
		return
	}

	tags := make([]*types.TagStats, 0, len(scan.ByTag))
	for _, stats := range scan.ByTag {
		tags = append(tags, stats)
	}
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].CompliancePct != tags[j].CompliancePct {
			return tags[i].CompliancePct < tags[j].CompliancePct
		}
		return tags[i].Tag < tags[j].Tag
	})

	fmt.Fprint(out, "\n### Tags\n\n")
	fmt.Fprint(out, "| Tag | Policy | Compliance | Present | Missing | Invalid |\n")
	fmt.Fprint(out, "| --- | --- | ---: | ---: | ---: | ---: |\n")
	for _, stats := range tags {
		policy := "optional"
		if stats.Required {
			policy = "required"
		}
		fmt.Fprintf(out, "| %s | %s | %s | %d | %d | %d |\n",
			markdownCell(stats.Tag), policy, markdownPct(stats.CompliancePct), stats.Present, stats.Missing, stats.Invalid)
	}
}

// markdownTypeRow counts the failures of one resource type.
type markdownTypeRow struct {
	name      string
	resources int
	findings  int
}

// failingTypes ranks resource types by distinct failing resources, then by failed findings.
func failingTypes(failed []types.Finding) []markdownTypeRow {
	resources := make(map[string]map[string]struct{})
	findings := make(map[string]int)
	for i := range failed {
		resource := &failed[i].Resource
		if resources[resource.Type] == nil {
			resources[resource.Type] = make(map[string]struct{})
		}
		resources[resource.Type][resource.Identity()] = struct{}{}
		findings[resource.Type]++
	}

	rows := make([]markdownTypeRow, 0, len(resources))
	for name, identities := range resources {
		rows = append(rows, markdownTypeRow{name: name, resources: len(identities), findings: findings[name]})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].resources != rows[j].resources {
			return rows[i].resources > rows[j].resources
		}
		if rows[i].findings != rows[j].findings {
			return rows[i].findings > rows[j].findings
		}
		return rows[i].name < rows[j].name
	})
	return rows
}

func writeMarkdownTypes(out *bufio.Writer, failed []types.Finding, limit int) {
	rows := failingTypes(failed)
	if len(rows) == 0 {
		return
	}

	fmt.Fprint(out, "\n### Top failing resource types\n\n")
	if len(rows) > limit {
		fmt.Fprintf(out, "Showing %d of %d resource types with failures.\n\n", limit, len(rows))
		rows = rows[:limit]
	}
	fmt.Fprint(out, "| Resource type | Failing resources | Failed findings |\n")
	fmt.Fprint(out, "| --- | ---: | ---: |\n")
	for _, row := range rows {
		fmt.Fprintf(out, "| %s | %d | %d |\n", markdownCell(row.name), row.resources, row.findings)
	}
}

func writeMarkdownFindings(out *bufio.Writer, failed []types.Finding, limit int) {
	if len(failed) == 0 {
		return
	}

	fmt.Fprint(out, "\n### Failed findings\n\n")
	if len(failed) > limit {
		fmt.Fprintf(out, "Showing the first %d of %d failed findings.\n\n", limit, len(failed))
		failed = failed[:limit]
	}
	fmt.Fprint(out, "| Resource | Type | Tag | Reason |\n")
	fmt.Fprint(out, "| --- | --- | --- | --- |\n")
	for i := range failed {
		finding := &failed[i]
		fmt.Fprintf(out, "| %s | %s | %s | %s |\n",
			markdownCell(finding.Resource.Identity()), markdownCell(finding.Resource.Type),
			markdownCell(finding.Tag), markdownCell(markdownReason(finding)))
	}
}

func writeMarkdownFooter(out *bufio.Writer, scan *types.ScanResult, version string) {
	parts := []string{toolName}
	if version != "" {
		parts[0] += " " + markdownCell(version)
	}
	if !scan.ScannedAt.IsZero() {
		parts = append(parts, "scanned "+markdownCell(scan.ScannedAt.UTC().Format(time.RFC3339)))
	}
	fmt.Fprintf(out, "\n<sub>%s</sub>\n", strings.Join(parts, ", "))
}

// markdownReason is the failure reason, with the offending value when there is one.
func markdownReason(finding *types.Finding) string {
	if finding.Actual == "" {
		return string(finding.Reason)
	}
	return fmt.Sprintf("%s: %s", finding.Reason, finding.Actual)
}

func markdownPct(pct float64) string {
	return fmt.Sprintf("%.1f%%", pct)
}

func plural(n int, noun string) string {
	if n == 1 {
		return noun
	}
	return noun + "s"
}

// markdownSpecials are the characters that would end a table cell or start
// inline Markdown or HTML inside one.
const markdownSpecials = "\\|`*_[]<>~"

// markdownCell makes a value safe inside a GitHub-flavoured Markdown table cell.
func markdownCell(value string) string {
	var cell strings.Builder
	cell.Grow(len(value))
	for _, r := range value {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			cell.WriteByte(' ')
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			continue
		case r == '&':
			cell.WriteString("&amp;")
		case strings.ContainsRune(markdownSpecials, r):
			cell.WriteByte('\\')
			cell.WriteRune(r)
		default:
			cell.WriteRune(r)
		}
	}
	return strings.TrimSpace(cell.String())
}

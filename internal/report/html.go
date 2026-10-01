package report

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/unicrons/tagctl/internal/types"
)

//go:embed templates/scan.html
var templateFS embed.FS

var scanTemplate = template.Must(template.ParseFS(templateFS, "templates/scan.html"))

// HTMLOptions configures the HTML report.
type HTMLOptions struct {
	// Version is the tagctl version printed in the report.
	Version string
}

// HTMLReport is the view rendered by the scan template.
type HTMLReport struct {
	Version       string
	ScannedAt     string
	Total         int
	Compliant     int
	NonCompliant  int
	CompliancePct float64
	Partial       bool
	Errors        []string
	Missing       int
	Invalid       int
	HeadlineLead  string
	HeadlineRest  string
	Lede          string
	Regions       string
	Accounts      []HTMLAccount
	Tags          []HTMLTag
	Matrix        []HTMLMatrixRow
	ByValue       []HTMLTagValues
	Findings      []HTMLFinding
}

// HTMLTagValues groups the scanned resources by the value of one policy tag.
type HTMLTagValues struct {
	Tag      string
	Selected bool
	Values   []HTMLTagValue
}

// HTMLTagValue is the resources sharing one value of a tag. Index is its
// position in HTMLTagValues.Values, which HTMLFinding.Groups refers to.
type HTMLTagValue struct {
	Label     string
	Untagged  bool
	Index     int
	Resources int
	Compliant int
	Pct       float64
}

// HTMLAccount is one row of the accounts section.
type HTMLAccount struct {
	Provider  string
	Account   string
	Total     int
	Compliant int
	Pct       float64
}

// HTMLTag is one policy tag with its coverage.
type HTMLTag struct {
	Name      string
	Required  bool
	Compliant int
	Total     int
	Pct       float64
}

// HTMLMatrixRow is one resource type in the type-by-tag matrix.
type HTMLMatrixRow struct {
	Type      string
	Resources int
	Compliant int
	Cells     []HTMLMatrixCell
}

// HTMLMatrixCell is the coverage of one tag on one resource type.
type HTMLMatrixCell struct {
	Count int
	Total int
	Pct   int
	Level int
}

// HTMLFinding is one row of the findings table.
type HTMLFinding struct {
	Resource string
	Type     string
	Region   string
	Tag      string
	Value    string
	ARN      string
	Status   string
	Label    string
	// Groups holds, per entry of HTMLReport.ByValue, the Index of the value
	// this finding's resource carries, space-separated.
	Groups string
}

// WriteHTML renders the scan as a standalone HTML report.
func WriteHTML(w io.Writer, scan *types.ScanResult, opts HTMLOptions) error {
	if err := scanTemplate.Execute(w, BuildHTMLReport(scan, opts)); err != nil {
		return fmt.Errorf("failed to render HTML report: %w", err)
	}
	return nil
}

// BuildHTMLReport derives the view the template renders from a scan.
func BuildHTMLReport(scan *types.ScanResult, opts HTMLOptions) HTMLReport {
	findings := scan.Findings
	if len(findings) == 0 && len(scan.Violations) > 0 {
		findings = types.ViolationsToFindings(scan.Violations)
	}

	r := HTMLReport{
		Version:       opts.Version,
		ScannedAt:     scan.ScannedAt.Format("2006-01-02 15:04:05 MST"),
		Total:         scan.TotalResources,
		Compliant:     scan.CompliantCount,
		NonCompliant:  scan.TotalResources - scan.CompliantCount,
		CompliancePct: scan.CompliancePct,
		Partial:       scan.Partial,
		Errors:        scan.Errors,
		Accounts:      htmlAccounts(scan),
		Tags:          htmlTags(scan, findings),
	}
	r.Regions = strings.Join(uniqueRegions(findings), ", ")
	r.Matrix = htmlMatrix(findings, r.Tags)

	var groups map[string]string
	r.ByValue, groups = htmlByValue(findings, r.Tags)
	r.Findings = htmlFindings(findings, groups)

	for _, f := range findings {
		if f.Status != types.StatusFailed {
			continue
		}
		if f.Reason == types.ReasonMissing {
			r.Missing++
		} else {
			r.Invalid++
		}
	}
	r.HeadlineLead, r.HeadlineRest, r.Lede = headline(r)

	return r
}

func headline(r HTMLReport) (lead, rest, lede string) {
	switch {
	case r.Total == 0:
		return "", "No resources were scanned",
			"Check the configured accounts, regions and credentials, then run the scan again."
	case r.NonCompliant == 0:
		return fmt.Sprintf("All %d resources", r.Total), "carry every required tag",
			"Nothing to fix. Keep it that way with --fail-under in CI."
	}

	lead = fmt.Sprintf("%d of %d resources", r.NonCompliant, r.Total)
	pass := fmt.Sprintf("%d of %d resources pass the whole policy.", r.Compliant, r.Total)

	switch {
	case r.Invalid == 0:
		rest = "are missing at least one required tag"
		lede = fmt.Sprintf("%s Nothing carries an invalid value — the %d gaps are tags that were never set.", pass, r.Missing)
	case r.Missing == 0:
		rest = "carry a tag value the policy rejects"
		lede = fmt.Sprintf("%s Every required tag is present, but %d values do not match the policy.", pass, r.Invalid)
	default:
		rest = "fail the tag policy"
		lede = fmt.Sprintf("%s %d tags were never set and %d values do not match the policy.", pass, r.Missing, r.Invalid)
	}
	return lead, rest, lede
}

func htmlAccounts(scan *types.ScanResult) []HTMLAccount {
	accounts := make([]HTMLAccount, 0, len(scan.ByAccount))
	for key, acc := range scan.ByAccount {
		name := acc.Account
		if name == "" {
			name = key
		}
		accounts = append(accounts, HTMLAccount{
			Provider:  acc.Provider,
			Account:   name,
			Total:     acc.Total,
			Compliant: acc.Compliant,
			Pct:       acc.CompliancePct,
		})
	}
	sort.Slice(accounts, func(i, j int) bool {
		if accounts[i].Provider != accounts[j].Provider {
			return accounts[i].Provider < accounts[j].Provider
		}
		return accounts[i].Account < accounts[j].Account
	})
	return accounts
}

// htmlTags lists the policy tags, required ones first. Scans written before
// ByTag existed fall back to the tags seen in the findings.
func htmlTags(scan *types.ScanResult, findings []types.Finding) []HTMLTag {
	tags := make([]HTMLTag, 0, len(scan.ByTag))
	for name, stats := range scan.ByTag {
		total := stats.Present + stats.Missing
		tags = append(tags, HTMLTag{
			Name:      name,
			Required:  stats.Required,
			Compliant: stats.Present - stats.Invalid,
			Total:     total,
			Pct:       stats.CompliancePct,
		})
	}

	if len(tags) == 0 {
		seen := map[string]*HTMLTag{}
		for _, f := range findings {
			t := seen[f.Tag]
			if t == nil {
				t = &HTMLTag{Name: f.Tag, Required: true}
				seen[f.Tag] = t
			}
			t.Total++
			if f.Status == types.StatusPass {
				t.Compliant++
			}
		}
		for _, t := range seen {
			if t.Total > 0 {
				t.Pct = float64(t.Compliant) / float64(t.Total) * 100
			}
			tags = append(tags, *t)
		}
	}

	sort.Slice(tags, func(i, j int) bool {
		if tags[i].Required != tags[j].Required {
			return tags[i].Required
		}
		return tags[i].Name < tags[j].Name
	})
	return tags
}

// htmlMatrix computes, per resource type, how many resources carry each tag
// and how many carry all of them. Rows are ordered by resource count.
func htmlMatrix(findings []types.Finding, tags []HTMLTag) []HTMLMatrixRow {
	type typeStats struct {
		resources map[string]bool
		failed    map[string]bool
		byTag     map[string]map[string]bool
	}
	stats := map[string]*typeStats{}

	for _, f := range findings {
		ts := stats[f.Resource.Type]
		if ts == nil {
			ts = &typeStats{
				resources: map[string]bool{},
				failed:    map[string]bool{},
				byTag:     map[string]map[string]bool{},
			}
			stats[f.Resource.Type] = ts
		}
		key := f.Resource.Identity()
		ts.resources[key] = true
		if f.Status == types.StatusFailed {
			ts.failed[key] = true
			continue
		}
		if ts.byTag[f.Tag] == nil {
			ts.byTag[f.Tag] = map[string]bool{}
		}
		ts.byTag[f.Tag][key] = true
	}

	rows := make([]HTMLMatrixRow, 0, len(stats))
	for resType, ts := range stats {
		row := HTMLMatrixRow{
			Type:      strings.TrimPrefix(resType, "aws_"),
			Resources: len(ts.resources),
			Compliant: len(ts.resources) - len(ts.failed),
			Cells:     make([]HTMLMatrixCell, 0, len(tags)),
		}
		for _, t := range tags {
			count := len(ts.byTag[t.Name])
			pct := 0
			if row.Resources > 0 {
				pct = count * 100 / row.Resources
			}
			row.Cells = append(row.Cells, HTMLMatrixCell{
				Count: count,
				Total: row.Resources,
				Pct:   pct,
				Level: coverageLevel(pct),
			})
		}
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Resources != rows[j].Resources {
			return rows[i].Resources > rows[j].Resources
		}
		return rows[i].Type < rows[j].Type
	})
	return rows
}

// coverageLevel buckets a percentage into the five matrix shades.
func coverageLevel(pct int) int {
	switch {
	case pct == 0:
		return 0
	case pct < 25:
		return 1
	case pct < 50:
		return 2
	case pct < 80:
		return 3
	default:
		return 4
	}
}

func uniqueRegions(findings []types.Finding) []string {
	seen := map[string]bool{}
	for _, f := range findings {
		if f.Resource.Region != "" {
			seen[f.Resource.Region] = true
		}
	}
	regions := make([]string, 0, len(seen))
	for r := range seen {
		regions = append(regions, r)
	}
	sort.Strings(regions)
	return regions
}

// untaggedLabel names the resources that carry no value for a tag.
const untaggedLabel = "(untagged)"

// defaultValueTags are the tags the by-value section opens on when the policy
// has one, in order of preference.
var defaultValueTags = []string{"owner", "team"}

// htmlByValue groups resources by the value they carry for each policy tag
// and counts how many pass the whole policy. It also returns, per resource
// identity, the HTMLFinding.Groups string of its findings.
func htmlByValue(findings []types.Finding, tags []HTMLTag) ([]HTMLTagValues, map[string]string) {
	if len(findings) == 0 || len(tags) == 0 {
		return nil, nil
	}

	tagIndex := make(map[string]int, len(tags))
	for i, t := range tags {
		tagIndex[t.Name] = i
	}

	type resourceState struct {
		values []string
		failed bool
	}
	resources := map[string]*resourceState{}

	for i := range findings {
		f := &findings[i]
		key := f.Resource.Identity()
		state := resources[key]
		if state == nil {
			state = &resourceState{values: make([]string, len(tags))}
			for name, idx := range tagIndex {
				state.values[idx] = f.Resource.Tags[name]
			}
			resources[key] = state
		}
		if f.Status == types.StatusFailed {
			state.failed = true
		}
		// Scan files written without resource tags still carry the value here.
		if idx, ok := tagIndex[f.Tag]; ok && state.values[idx] == "" {
			state.values[idx] = f.Actual
		}
	}

	byValue := make([]HTMLTagValues, len(tags))
	positions := make([]map[string]int, len(tags))
	for idx, t := range tags {
		counts := map[string]*HTMLTagValue{}
		for _, state := range resources {
			value := state.values[idx]
			row := counts[value]
			if row == nil {
				row = &HTMLTagValue{Label: value}
				counts[value] = row
			}
			row.Resources++
			if !state.failed {
				row.Compliant++
			}
		}
		byValue[idx], positions[idx] = tagValues(t.Name, counts)
	}
	selectDefaultValueTag(byValue)

	groups := make(map[string]string, len(resources))
	for key, state := range resources {
		parts := make([]string, len(tags))
		for idx, value := range state.values {
			parts[idx] = strconv.Itoa(positions[idx][value])
		}
		groups[key] = strings.Join(parts, " ")
	}
	return byValue, groups
}

// tagValues orders the values of one tag by resource count, the untagged row
// last, and returns the position of each value.
func tagValues(tag string, counts map[string]*HTMLTagValue) (HTMLTagValues, map[string]int) {
	untagged := &HTMLTagValue{}
	if row, ok := counts[""]; ok {
		untagged = row
	}
	untagged.Label, untagged.Untagged = untaggedLabel, true

	values := make([]HTMLTagValue, 0, len(counts)+1)
	for value, row := range counts {
		if value != "" {
			values = append(values, *row)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Resources != values[j].Resources {
			return values[i].Resources > values[j].Resources
		}
		return values[i].Label < values[j].Label
	})
	values = append(values, *untagged)

	positions := make(map[string]int, len(values))
	for i := range values {
		v := &values[i]
		v.Index = i
		if v.Resources > 0 {
			v.Pct = float64(v.Compliant) / float64(v.Resources) * 100
		}
		if v.Untagged {
			positions[""] = i
		} else {
			positions[v.Label] = i
		}
	}
	return HTMLTagValues{Tag: tag, Values: values}, positions
}

func selectDefaultValueTag(byValue []HTMLTagValues) {
	for _, preferred := range defaultValueTags {
		for i := range byValue {
			if strings.EqualFold(byValue[i].Tag, preferred) {
				byValue[i].Selected = true
				return
			}
		}
	}
	byValue[0].Selected = true
}

func htmlFindings(findings []types.Finding, groups map[string]string) []HTMLFinding {
	rows := make([]HTMLFinding, 0, len(findings))
	for i := range findings {
		f := &findings[i]
		row := HTMLFinding{
			Groups:   groups[f.Resource.Identity()],
			Resource: f.Resource.DisplayName(),
			Type:     f.Resource.Type,
			Region:   f.Resource.Region,
			Tag:      f.Tag,
			Value:    f.Actual,
			ARN:      f.Resource.ARN,
			Status:   "pass",
			Label:    "pass",
		}
		if f.Status == types.StatusFailed {
			row.Status = "fail"
			row.Label = failureLabel(f.Reason)
		}
		rows = append(rows, row)
	}
	return rows
}

func failureLabel(reason types.ViolationReason) string {
	switch reason {
	case types.ReasonMissing:
		return "missing"
	case types.ReasonInvalidValue:
		return "invalid value"
	case types.ReasonInvalidFormat:
		return "invalid format"
	case types.ReasonForbidden:
		return "forbidden"
	default:
		return "failed"
	}
}

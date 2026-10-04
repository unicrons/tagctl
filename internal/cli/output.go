package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"unicode"
	"unicode/utf8"

	"github.com/unicrons/tagctl/internal/report"
	"github.com/unicrons/tagctl/internal/types"
)

// renderProgressBar creates a visual progress bar.
func renderProgressBar(percent float64, width int) string {
	filled := int(percent / 100 * float64(width))
	if filled > width {
		filled = width
	}
	bar := ""
	for i := 0; i < width; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += "░"
		}
	}
	return bar
}

// outputScanTable prints scan results in table format.
func outputScanTable(result *types.ScanResult, verbose bool) error {
	// Header
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("                  Tag Compliance Report")
	fmt.Printf("                  %s\n", result.ScannedAt.Format("2006-01-02 15:04:05"))
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	// Overall compliance
	complianceBar := renderProgressBar(result.CompliancePct, 20)
	fmt.Printf("Overall: %.0f%% compliant %s (%d/%d resources)\n",
		result.CompliancePct, complianceBar, result.CompliantCount, result.TotalResources)
	fmt.Println()

	// By account table
	if len(result.ByAccount) > 0 {
		fmt.Println("By Account:")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  ACCOUNT\tTOTAL\tCOMPLIANT\tCOMPLIANCE")
		fmt.Fprintln(w, "  ───────\t─────\t─────────\t──────────")
		for _, key := range slices.Sorted(maps.Keys(result.ByAccount)) {
			acc := result.ByAccount[key]
			bar := renderProgressBar(acc.CompliancePct, 10)
			fmt.Fprintf(w, "  %s/%s\t%d\t%d\t%.0f%% %s\n",
				printable(acc.Provider), printable(acc.Account), acc.Total, acc.Compliant, acc.CompliancePct, bar)
		}
		_ = w.Flush()
		fmt.Println()
	}

	// By tag table
	if len(result.ByTag) > 0 {
		fmt.Println("By Required Tag:")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  TAG\tPRESENT\tMISSING\tINVALID\tCOMPLIANCE")
		fmt.Fprintln(w, "  ───\t───────\t───────\t───────\t──────────")
		for _, name := range slices.Sorted(maps.Keys(result.ByTag)) {
			tag := result.ByTag[name]
			if tag.Required {
				bar := renderProgressBar(tag.CompliancePct, 10)
				fmt.Fprintf(w, "  %s\t%d\t%d\t%d\t%.0f%% %s\n",
					printable(tag.Tag), tag.Present, tag.Missing, tag.Invalid, tag.CompliancePct, bar)
			}
		}
		_ = w.Flush()
		fmt.Println()
	}

	// Findings table
	if len(result.Findings) > 0 {
		fmt.Println("Findings:")
		limit := 10
		if verbose {
			limit = len(result.Findings)
		}
		shown := min(limit, len(result.Findings))
		printFindingsTable(os.Stdout, result.Findings[:shown])

		if shown < len(result.Findings) {
			remaining := len(result.Findings) - shown
			fmt.Printf("  ... and %d more findings (use --verbose to see all)\n", remaining)
		}
		fmt.Println()

		// Count PASS findings
		passFindings := 0
		failFindings := 0
		for _, f := range result.Findings {
			if f.Status == types.StatusPass {
				passFindings++
			} else {
				failFindings++
			}
		}

		// Summary
		c := paletteFor(os.Stdout)
		fmt.Printf("Summary: %s%d PASS%s | %s%d FAILED%s\n",
			c.green, passFindings, c.reset, c.red, failFindings, c.reset)
		fmt.Println()
	}

	// Footer
	if result.ViolationCount > 0 {
		fmt.Println("Run 'tagctl plan' to see suggested fixes.")
	} else {
		fmt.Println("All resources are compliant!")
	}

	return nil
}

// outputScanJSON prints scan results in JSON format to stdout.
func outputScanJSON(result *types.ScanResult) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// outputScanCSV prints scan results in CSV format to stdout.
func outputScanCSV(result *types.ScanResult) error {
	return writeFindingsCSV(os.Stdout, result)
}

var csvHeader = []string{"resource_id", "resource_name", "resource_type", "provider", "account", "region", "arn", "tag", "status", "reason", "actual", "expected"}

// writeFindingsCSV writes the findings with RFC 4180 quoting. Cells that a
// spreadsheet would evaluate as a formula are prefixed with a quote, since
// tag values are written by whoever can tag the resource.
func writeFindingsCSV(w io.Writer, result *types.ScanResult) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(csvHeader); err != nil {
		return err
	}
	for _, f := range result.Findings {
		row := []string{
			f.Resource.ID, f.Resource.Name, f.Resource.Type, f.Resource.Provider,
			f.Resource.Account, f.Resource.Region, f.Resource.ARN,
			f.Tag, string(f.Status), string(f.Reason), f.Actual, f.Expected,
		}
		for i := range row {
			row[i] = csvSafe(row[i])
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// csvSafe quotes a cell a spreadsheet would run as a formula. Spreadsheets
// skip leading whitespace before the trigger character, so it is skipped here too.
func csvSafe(cell string) string {
	if cell == "" {
		return cell
	}
	body := strings.TrimLeftFunc(cell, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	})
	if strings.ContainsRune("\t\r\n", rune(cell[0])) || (body != "" && strings.ContainsRune("=+-@", rune(body[0]))) {
		return "'" + cell
	}
	return cell
}

// Findings table cells longer than these are cut: one ARN-sized name would
// otherwise wrap every row. The report files keep the full values.
const (
	maxResourceWidth = 48
	maxValueWidth    = 32
)

// printFindingsTable writes the findings as aligned columns. It pads by hand
// because tabwriter counts the colour codes as cell width.
func printFindingsTable(out io.Writer, findings []types.Finding) {
	rows := make([][]string, 0, len(findings)+2)
	rows = append(rows,
		[]string{"STATUS", "RESOURCE", "TYPE", "TAG", "VALUE"},
		[]string{"──────", "────────", "────", "───", "─────"},
	)
	for _, f := range findings {
		status := "FAILED"
		if f.Status == types.StatusPass {
			status = "PASS"
		}
		value := f.Actual
		if value == "" && f.Status == types.StatusFailed {
			value = "(missing)"
		} else if value == "" {
			value = "-"
		}
		rows = append(rows, []string{
			status,
			truncate(printable(f.Resource.DisplayName()), maxResourceWidth),
			printable(f.Resource.Type),
			printable(f.Tag),
			truncate(printable(value), maxValueWidth),
		})
	}

	c := paletteFor(out)
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], utf8.RuneCountInString(cell))
		}
	}
	for n, row := range rows {
		var line strings.Builder
		line.WriteString("  ")
		for i, cell := range row {
			padded := cell
			if i < len(row)-1 {
				padded += strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell)+2)
			}
			if i == 0 && n >= 2 {
				colour := c.red
				if cell == "PASS" {
					colour = c.green
				}
				padded = colour + cell + c.reset + padded[len(cell):]
			}
			line.WriteString(padded)
		}
		fmt.Fprintln(out, line.String())
	}
}

// printable replaces control characters so a tag value cannot drive the
// terminal through escape sequences.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '?'
		}
		return r
	}, s)
}

// createReport opens an output file readable only by the current user: scan
// reports carry every tag value and ARN of the account.
func createReport(path string) (*os.File, error) {
	// #nosec G304 -- the output path is derived from the user's own output directory.
	return os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
}

// writeScanJSON writes scan results to a JSON file.
func writeScanJSON(result *types.ScanResult, path string) error {
	file, err := createReport(path)
	if err != nil {
		return fmt.Errorf("failed to create JSON file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// writeScanCSV writes scan results to a CSV file.
func writeScanCSV(result *types.ScanResult, path string) error {
	file, err := createReport(path)
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %w", err)
	}
	defer file.Close()

	return writeFindingsCSV(file, result)
}

// writeScanHTML writes scan results to an HTML file.
func writeScanHTML(result *types.ScanResult, path string) error {
	file, err := createReport(path)
	if err != nil {
		return fmt.Errorf("failed to create HTML file: %w", err)
	}
	defer file.Close()

	return report.WriteHTML(file, result, report.HTMLOptions{Version: Version})
}

// outputPlanTable prints plan in table format.
func outputPlanTable(plan *types.Plan) {
	if plan.IsEmpty() {
		if len(plan.Conflicts) == 0 {
			fmt.Println("No changes needed. All resources are compliant!")
		} else {
			fmt.Println("No changes planned.")
			printPlanConflicts(os.Stdout, plan)
		}
		return
	}

	fmt.Println("Planned changes:")
	fmt.Println()

	// Group changes by resource, in plan order.
	resourceChanges := make(map[string][]types.TagChange)
	var order []string
	for _, change := range plan.Changes {
		key := change.Resource.Identity()
		if _, seen := resourceChanges[key]; !seen {
			order = append(order, key)
		}
		resourceChanges[key] = append(resourceChanges[key], change)
	}

	for _, key := range order {
		changes := resourceChanges[key]
		res := changes[0].Resource
		fmt.Printf("%s (%s)\n", printable(res.Type), printable(res.DisplayName()))

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, c := range changes {
			actionSymbol := "+"
			switch c.Action {
			case types.ActionUpdate:
				actionSymbol = "~"
			case types.ActionRemove:
				actionSymbol = "-"
			}

			source := ""
			if c.Source != "" {
				source = fmt.Sprintf("(%s: %s)", c.Reason, c.Source)
			} else {
				source = fmt.Sprintf("(%s)", c.Reason)
			}

			value := c.NewValue
			if c.Action == types.ActionRemove {
				value = c.OldValue
			}
			fmt.Fprintf(w, "  %s %s:\t\"%s\"\t%s\n", actionSymbol, printable(c.Tag), printable(value), printable(source))
		}
		_ = w.Flush()
		fmt.Println()
	}

	// Summary
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Summary: %d resources will be modified\n", plan.Summary.TotalResources)
	fmt.Printf("         %d tags will be added\n", plan.Summary.TagsAdded)
	fmt.Printf("         %d tags will be updated\n", plan.Summary.TagsUpdated)
	fmt.Printf("         %d tags will be removed\n", plan.Summary.TagsRemoved)
	printPlanConflicts(os.Stdout, plan)
}

// printPlanConflicts lists the renames the planner skipped.
func printPlanConflicts(w io.Writer, plan *types.Plan) {
	if len(plan.Conflicts) == 0 {
		return
	}
	fmt.Fprintf(w, "\nConflicts: %d rename(s) skipped, the target tag holds another value\n", len(plan.Conflicts))
	for i := range plan.Conflicts {
		c := &plan.Conflicts[i]
		fmt.Fprintf(w, "  ! %s (%s): %s\n", printable(c.Resource.Type), printable(c.Resource.DisplayName()), printable(c.Message()))
	}
}

// outputPlanJSON prints plan in JSON format.
func outputPlanJSON(plan *types.Plan) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(plan)
}

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

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
		for _, acc := range result.ByAccount {
			bar := renderProgressBar(acc.CompliancePct, 10)
			fmt.Fprintf(w, "  %s/%s\t%d\t%d\t%.0f%% %s\n",
				acc.Provider, acc.Account, acc.Total, acc.Compliant, acc.CompliancePct, bar)
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
		for _, tag := range result.ByTag {
			if tag.Required {
				bar := renderProgressBar(tag.CompliancePct, 10)
				fmt.Fprintf(w, "  %s\t%d\t%d\t%d\t%.0f%% %s\n",
					tag.Tag, tag.Present, tag.Missing, tag.Invalid, tag.CompliancePct, bar)
			}
		}
		_ = w.Flush()
		fmt.Println()
	}

	// Findings table
	if len(result.Findings) > 0 {
		fmt.Println("Findings:")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  STATUS\tRESOURCE\tTYPE\tTAG\tVALUE")
		fmt.Fprintln(w, "  ──────\t────────\t────\t───\t─────")

		limit := 10
		if verbose {
			limit = len(result.Findings)
		}

		shown := 0
		for _, f := range result.Findings {
			if shown >= limit && !verbose {
				break
			}

			// Color codes
			var statusStr string
			if f.Status == types.StatusPass {
				statusStr = "\033[32mPASS\033[0m"
			} else {
				statusStr = "\033[31mFAILED\033[0m"
			}

			value := f.Actual
			if value == "" && f.Status == types.StatusFailed {
				value = "(missing)"
			} else if value == "" {
				value = "-"
			}

			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\n",
				statusStr, f.Resource.DisplayName(), f.Resource.Type, f.Tag, value)
			shown++
		}
		_ = w.Flush()

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
		fmt.Printf("Summary: \033[32m%d PASS\033[0m | \033[31m%d FAILED\033[0m\n",
			passFindings, failFindings)
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
	fmt.Println("resource_id,resource_name,resource_type,provider,account,region,arn,tag,status,reason,actual,expected")
	for _, f := range result.Findings {
		fmt.Printf("%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			f.Resource.ID, f.Resource.Name, f.Resource.Type, f.Resource.Provider,
			f.Resource.Account, f.Resource.Region, f.Resource.ARN,
			f.Tag, f.Status, f.Reason, f.Actual, f.Expected)
	}
	return nil
}

// writeScanJSON writes scan results to a JSON file.
func writeScanJSON(result *types.ScanResult, path string) error {
	// #nosec G304 -- the output path is derived from the user's own output directory.
	file, err := os.Create(filepath.Clean(path))
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
	// #nosec G304 -- the output path is derived from the user's own output directory.
	file, err := os.Create(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %w", err)
	}
	defer file.Close()

	fmt.Fprintln(file, "resource_id,resource_name,resource_type,provider,account,region,arn,tag,status,reason,actual,expected")
	for _, f := range result.Findings {
		fmt.Fprintf(file, "%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			f.Resource.ID, f.Resource.Name, f.Resource.Type, f.Resource.Provider,
			f.Resource.Account, f.Resource.Region, f.Resource.ARN,
			f.Tag, f.Status, f.Reason, f.Actual, f.Expected)
	}
	return nil
}

// writeScanHTML writes scan results to an HTML file.
func writeScanHTML(result *types.ScanResult, path string) error {
	// #nosec G304 -- the output path is derived from the user's own output directory.
	file, err := os.Create(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("failed to create HTML file: %w", err)
	}
	defer file.Close()

	return report.WriteHTML(file, result, report.HTMLOptions{Version: appVersion})
}

// outputPlanTable prints plan in table format.
func outputPlanTable(plan *types.Plan, planFile string) error {
	if plan.IsEmpty() {
		fmt.Println("No changes needed. All resources are compliant!")
		return nil
	}

	fmt.Println("Planned changes:")
	fmt.Println()

	// Group changes by resource
	resourceChanges := make(map[string][]types.TagChange)
	for _, change := range plan.Changes {
		key := change.Resource.Identity()
		resourceChanges[key] = append(resourceChanges[key], change)
	}

	for resourceID, changes := range resourceChanges {
		res := changes[0].Resource
		fmt.Printf("%s (%s)\n", res.Type, res.DisplayName())

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, c := range changes {
			actionSymbol := "+"
			if c.Action == types.ActionUpdate {
				actionSymbol = "~"
			} else if c.Action == types.ActionRemove {
				actionSymbol = "-"
			}

			source := ""
			if c.Source != "" {
				source = fmt.Sprintf("(%s: %s)", c.Reason, c.Source)
			} else {
				source = fmt.Sprintf("(%s)", c.Reason)
			}

			fmt.Fprintf(w, "  %s %s:\t\"%s\"\t%s\n", actionSymbol, c.Tag, c.NewValue, source)
		}
		_ = w.Flush()
		fmt.Println()
		_ = resourceID // used as map key
	}

	// Summary
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Summary: %d resources will be modified\n", plan.Summary.TotalResources)
	fmt.Printf("         %d tags will be added\n", plan.Summary.TagsAdded)
	fmt.Printf("         %d tags will be updated\n", plan.Summary.TagsUpdated)
	fmt.Printf("         %d tags will be removed\n", plan.Summary.TagsRemoved)
	fmt.Println()
	fmt.Printf("Plan saved to: %s\n", planFile)
	fmt.Println("Run 'tagctl apply' to execute this plan.")

	return nil
}

// outputPlanJSON prints plan in JSON format.
func outputPlanJSON(plan *types.Plan) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(plan)
}

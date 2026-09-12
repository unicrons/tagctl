package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/unicrons/tagctl/internal/engine"
	"github.com/unicrons/tagctl/internal/types"
)

var diffCmd = &cobra.Command{
	Use:   "diff [baseline.json] [current.json]",
	Short: "Compare two scans and report compliance drift",
	Long: `Diff compares a baseline scan against a newer one and reports what moved:
findings that regressed, findings that were resolved, resources that appeared or
disappeared, and the change in compliance per tag and per account.

With no arguments it compares the two most recent scans in the output directory.

A compliance percentage on its own says little. Drift is what teams act on, and
what a pipeline can be gated on with --fail-on-regression.

Examples:
  # Compare the two most recent scans
  tagctl diff

  # Compare two specific scans
  tagctl diff output/scan-20260101-120000.json output/scan-20260201-120000.json

  # Fail the build if anything regressed
  tagctl diff --fail-on-regression

  # Machine-readable output
  tagctl diff -o json`,
	Args: cobra.MaximumNArgs(2),
	RunE: runDiff,
}

func init() {
	diffCmd.Flags().Bool("fail-on-regression", false, "exit with a non-zero status if any finding regressed")
}

func runDiff(cmd *cobra.Command, args []string) error {
	failOnRegression, _ := cmd.Flags().GetBool("fail-on-regression")

	baselinePath, currentPath, err := resolveDiffInputs(args)
	if err != nil {
		return err
	}

	baseline, err := LoadScanFile(baselinePath)
	if err != nil {
		return err
	}

	current, err := LoadScanFile(currentPath)
	if err != nil {
		return err
	}

	const diffConsequence = "new, removed and resolved counts may be wrong"
	warnPartialScan(os.Stderr, baselinePath, baseline, diffConsequence)
	warnPartialScan(os.Stderr, currentPath, current, diffConsequence)

	result := engine.Diff(baseline, current)

	if strings.ToLower(outputFormat) == formatJSON {
		if err := printJSON(result); err != nil {
			return err
		}
	} else {
		printDiffTable(result, baselinePath, currentPath)
	}

	if failOnRegression && result.HasRegressions() {
		return fmt.Errorf("%d finding(s) regressed since the baseline", len(result.Regressions))
	}

	return nil
}

// resolveDiffInputs works out which two scan files to compare. With no
// arguments it picks the two most recent scans; with one, it treats it as the
// baseline and compares it against the most recent scan.
func resolveDiffInputs(args []string) (baseline, current string, err error) {
	switch len(args) {
	case 2:
		return args[0], args[1], nil // #nosec G602 -- guarded by the switch on len(args)
	case 1:
		latest, findErr := findLatestScan()
		if findErr != nil {
			return "", "", findErr
		}
		return args[0], latest, nil
	default:
		scans, findErr := findRecentScans(2)
		if findErr != nil {
			return "", "", findErr
		}
		if len(scans) < 2 {
			return "", "", fmt.Errorf("need two scans to compare, found %d in %s. Run 'tagctl scan' again to create a second one", len(scans), OutputDir)
		}
		// findRecentScans returns newest first.
		return scans[1], scans[0], nil
	}
}

// findRecentScans returns up to limit scan files from the output directory,
// most recent first.
func findRecentScans(limit int) ([]string, error) {
	entries, err := os.ReadDir(OutputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no output directory found. Run 'tagctl scan' first")
		}
		return nil, fmt.Errorf("failed to read output directory: %w", err)
	}

	type scanFile struct {
		name    string
		modTime int64
	}

	files := make([]scanFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isScanFileName(entry.Name()) {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		files = append(files, scanFile{name: entry.Name(), modTime: info.ModTime().UnixNano()})
	}

	sort.Slice(files, func(i, j int) bool { return files[i].modTime > files[j].modTime })

	paths := make([]string, 0, limit)
	for i := 0; i < len(files) && i < limit; i++ {
		paths = append(paths, filepath.Join(OutputDir, files[i].name))
	}

	return paths, nil
}

// isScanFileName reports whether a file name looks like a scan result.
func isScanFileName(name string) bool {
	return strings.HasPrefix(name, "scan-") && strings.HasSuffix(name, ".json")
}

// LoadScanFile reads and parses a scan result from disk.
func LoadScanFile(path string) (*types.ScanResult, error) {
	// #nosec G304 -- the scan path is supplied by the user running the CLI.
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("failed to read scan file %s: %w", path, err)
	}

	var result types.ScanResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse scan file %s: %w", path, err)
	}

	return &result, nil
}

// printJSON writes any value as indented JSON to stdout.
func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printDiffTable(result *types.DiffResult, baselinePath, currentPath string) {
	fmt.Printf("Baseline: %s (%s)\n", baselinePath, result.BaselineScannedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Current:  %s (%s)\n", currentPath, result.CurrentScannedAt.Format("2006-01-02 15:04:05"))
	fmt.Println()

	delta := result.CompliancePctDelta()
	fmt.Printf("Compliance: %.1f%% → %.1f%% (%s)\n",
		result.CompliancePctBefore, result.CompliancePctAfter, signedPct(delta))
	fmt.Println()

	if result.IsClean() {
		fmt.Println("No drift: the two scans report the same findings.")
		return
	}

	fmt.Printf("Regressed: %d    Resolved: %d    Still failing: %d\n",
		len(result.Regressions), len(result.Resolved), result.Unchanged)
	if len(result.NewResources) > 0 || len(result.RemovedResources) > 0 {
		fmt.Printf("New resources: %d    Removed resources: %d\n",
			len(result.NewResources), len(result.RemovedResources))
	}
	fmt.Println()

	if len(result.Regressions) > 0 {
		fmt.Println("Regressions:")
		for _, f := range result.Regressions {
			fmt.Printf("  ✗ %s %s (%s) — %s\n",
				f.Resource.Type, f.Resource.ID, f.Resource.Account, f.Message())
		}
		fmt.Println()
	}

	if len(result.Resolved) > 0 {
		fmt.Println("Resolved:")
		for _, f := range result.Resolved {
			fmt.Printf("  ✓ %s %s (%s) — tag '%s' is now compliant\n",
				f.Resource.Type, f.Resource.ID, f.Resource.Account, f.Tag)
		}
		fmt.Println()
	}

	printTagDeltas(result)
}

// printTagDeltas lists the tags whose compliance moved, worst first.
func printTagDeltas(result *types.DiffResult) {
	moved := make([]*types.TagDelta, 0, len(result.ByTag))
	for _, delta := range result.ByTag {
		if delta.Delta() != 0 {
			moved = append(moved, delta)
		}
	}
	if len(moved) == 0 {
		return
	}

	sort.Slice(moved, func(i, j int) bool {
		if moved[i].Delta() != moved[j].Delta() {
			return moved[i].Delta() < moved[j].Delta()
		}
		return moved[i].Tag < moved[j].Tag
	})

	fmt.Println("By tag:")
	for _, delta := range moved {
		fmt.Printf("  %-24s %5.1f%% → %5.1f%%  (%s)\n",
			delta.Tag, delta.CompliancePctBefore, delta.CompliancePctAfter, signedPct(delta.Delta()))
	}
	fmt.Println()
}

// signedPct formats a percentage change with an explicit sign.
func signedPct(delta float64) string {
	switch {
	case delta > 0:
		return fmt.Sprintf("+%.1f%%", delta)
	case delta < 0:
		return fmt.Sprintf("%.1f%%", delta)
	default:
		return "no change"
	}
}

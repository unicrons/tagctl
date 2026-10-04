package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/unicrons/tagctl/internal/demo"
	"github.com/unicrons/tagctl/internal/engine"
	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/types"
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Generate a plan to fix tag violations",
	Long: `Plan analyzes resources and generates smart fixes using
rename, inference, inheritance and default rules.

The plan will:
  • Analyze current tag violations
  • Apply rename rules (move a value to the right key, remove the old key)
  • Apply inference rules (from other tags and naming conventions)
  • Apply inheritance rules (from the parent resources the scan recorded)
  • Apply default values for untagged resources
  • Remove the tags policy.forbidden forbids
  • Generate a detailed plan file

A rename whose target key already holds another value is reported as a
conflict and left out of the plan.

Examples:
  # Generate a plan
  tagctl plan

  # Output as JSON
  tagctl plan --output json

  # Save plan to specific file
  tagctl plan --out my-plan.json

  # Read the latest scan from, and write the plan to, another directory
  tagctl plan --output-dir reports`,
	RunE: runPlan,
}

func init() {
	planCmd.Flags().String("out", "", "save plan to specific file")
	planCmd.Flags().String("scan", "", "path to scan results file (default: latest in --output-dir)")
	addOutputDirFlag(planCmd, "directory to read the latest scan from and write the plan to")
}

// planContext builds the context a plan runs under; tests replace it.
var planContext = signalContext

func runPlan(cmd *cobra.Command, args []string) error {
	outFile, _ := cmd.Flags().GetString("out")
	scanFile, _ := cmd.Flags().GetString("scan")

	format, err := outputFormatFor(cmd, formatTable, formatJSON)
	if err != nil {
		return err
	}

	outputDir, err := outputDirFor(cmd)
	if err != nil {
		return err
	}

	ctx, stop := planContext()
	defer stop()

	printBanner()

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if !hasConfiguredProviders(cfg) {
		printDemoModeWarning()
		fmt.Fprint(os.Stderr, "Analyzing resources for auto-fix opportunities (demo mode)...\n\n")

		// A demo plan on disk would be picked up by apply as a real one.
		if err = printPlan(demo.Plan(), format); err != nil {
			return err
		}
		fmt.Fprint(os.Stderr, "\nDemo plan: example data only, not saved.\n")
		return nil
	}

	// Load scan results from file with spinner
	spinner := NewSpinner("Loading scan results...")
	spinner.Start()

	scanResult, scanPath, err := loadScanResults(outputDir, scanFile)
	if err != nil {
		spinner.Fail("Failed to load scan results")
		return fmt.Errorf("failed to load scan results: %w", err)
	}
	spinner.Success(fmt.Sprintf("Loaded scan from %s", printable(scanPath)))
	warnPartialScan(os.Stderr, scanPath, scanResult, "resources it missed get no changes in this plan")

	log.Info("Using scan results from: %s", scanPath)
	log.Info("Found %d failed findings to analyze", len(scanResult.FailedFindings()))

	// Create planner and generate plan with spinner
	spinner = NewSpinner("Analyzing resources for auto-fix opportunities...")
	spinner.Start()

	planner, err := engine.NewPlanner(cfg.Rules)
	if err != nil {
		spinner.Fail("Failed to create planner")
		return fmt.Errorf("failed to create planner: %w", err)
	}

	plan, err := planner.Plan(ctx, scanResult)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		spinner.Fail("Failed to generate plan")
		return fmt.Errorf("failed to generate plan: %w", err)
	}
	spinner.Success(fmt.Sprintf("Generated plan with %d changes", len(plan.Changes)))
	printPlanWarnings(os.Stderr, plan)

	return savePlanAndOutput(plan, outputDir, outFile, format)
}

// printPlanWarnings reports the rules the planner could not apply.
func printPlanWarnings(w io.Writer, plan *types.Plan) {
	for _, warning := range plan.Warnings {
		fmt.Fprintf(w, "Warning: %s\n", warning)
	}
}

// loadScanResults loads scan results from a file.
// If scanFile is empty, it loads the latest scan in dir.
func loadScanResults(dir, scanFile string) (*types.ScanResult, string, error) {
	var scanPath string

	if scanFile != "" {
		scanPath = scanFile
	} else {
		// Find the latest scan file
		latest, err := findLatestScanIn(dir)
		if err != nil {
			return nil, "", fmt.Errorf("no scan results found. Run 'tagctl scan' first: %w", err)
		}
		scanPath = latest
	}

	// Read and parse the scan file
	// #nosec G304 -- the scan path is supplied by the user running the CLI.
	data, err := os.ReadFile(filepath.Clean(scanPath))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read scan file %s: %w", scanPath, err)
	}

	var result types.ScanResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, "", fmt.Errorf("failed to parse scan file %s: %w", scanPath, err)
	}

	return &result, scanPath, nil
}

// findLatestScan finds the most recent scan JSON file in the default output directory.
func findLatestScan() (string, error) {
	return findLatestScanIn(OutputDir)
}

// findLatestScanIn finds the most recent scan JSON file in dir.
func findLatestScanIn(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("output directory %s not found. Run 'tagctl scan' first", dir)
		}
		return "", fmt.Errorf("failed to read output directory: %w", err)
	}

	var latestFile string
	var latestTime time.Time

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Match scan-YYYYMMDD-HHMMSS.json pattern
		if len(name) > 5 && name[:5] == "scan-" && name[len(name)-5:] == ".json" {
			info, err := entry.Info()
			if err != nil {
				continue
			}

			if info.ModTime().After(latestTime) {
				latestTime = info.ModTime()
				latestFile = name
			}
		}
	}

	if latestFile == "" {
		return "", fmt.Errorf("no scan files found in %s. Run 'tagctl scan' first", dir)
	}

	return filepath.Join(dir, latestFile), nil
}

func savePlanAndOutput(plan *types.Plan, outputDir, outFile, format string) error {
	// Save plan to file
	planFile, err := GetPlanPath(outputDir, outFile)
	if err != nil {
		return err
	}

	planJSON, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal plan: %w", err)
	}

	if err := os.WriteFile(planFile, planJSON, 0o600); err != nil {
		return fmt.Errorf("failed to write plan file: %w", err)
	}

	if err := printPlan(plan, format); err != nil {
		return err
	}

	if !plan.IsEmpty() {
		fmt.Fprintf(os.Stderr, "\nPlan saved to: %s\nRun 'tagctl apply' to execute this plan.\n", printable(planFile))
	}
	return nil
}

// printPlan renders a plan to stdout in format.
func printPlan(plan *types.Plan, format string) error {
	if format == formatJSON {
		return outputPlanJSON(plan)
	}
	outputPlanTable(plan)
	return nil
}

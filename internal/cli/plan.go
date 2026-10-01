package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/unicrons/tagctl/internal/engine"
	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/types"
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Generate a plan to fix tag violations",
	Long: `Plan analyzes resources and generates smart fixes using
inference rules, inheritance, and defaults.

The plan will:
  • Analyze current tag violations
  • Apply inference rules (from naming conventions)
  • Apply inheritance rules (from parent resources)
  • Apply default values for untagged resources
  • Generate a detailed plan file

Examples:
  # Generate a plan
  tagctl plan

  # Output as JSON
  tagctl plan --output json

  # Save plan to specific file
  tagctl plan --out my-plan.json`,
	RunE: runPlan,
}

func init() {
	planCmd.Flags().String("out", "", "save plan to specific file")
	planCmd.Flags().String("scan", "", "path to scan results file (default: latest in output/)")
}

func runPlan(cmd *cobra.Command, args []string) error {
	outFile, _ := cmd.Flags().GetString("out")
	scanFile, _ := cmd.Flags().GetString("scan")
	ctx := context.Background()

	format, err := outputFormatFor(cmd, formatTable, formatJSON)
	if err != nil {
		return err
	}

	printBanner()

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Check if we should use mock mode
	if !hasConfiguredProviders(cfg) {
		printDemoModeWarning()
		fmt.Fprint(os.Stderr, "Analyzing resources for auto-fix opportunities (demo mode)...\n\n")

		plan := getMockPlan()
		return savePlanAndOutput(plan, outFile, format)
	}

	// Load scan results from file with spinner
	spinner := NewSpinner("Loading scan results...")
	spinner.Start()

	scanResult, scanPath, err := loadScanResults(scanFile)
	if err != nil {
		spinner.Fail("Failed to load scan results")
		return fmt.Errorf("failed to load scan results: %w", err)
	}
	spinner.Success(fmt.Sprintf("Loaded scan from %s", scanPath))
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
	if err != nil {
		spinner.Fail("Failed to generate plan")
		return fmt.Errorf("failed to generate plan: %w", err)
	}
	spinner.Success(fmt.Sprintf("Generated plan with %d changes", len(plan.Changes)))

	return savePlanAndOutput(plan, outFile, format)
}

// loadScanResults loads scan results from a file.
// If scanFile is empty, it loads the latest scan from the output directory.
func loadScanResults(scanFile string) (*types.ScanResult, string, error) {
	var scanPath string

	if scanFile != "" {
		scanPath = scanFile
	} else {
		// Find the latest scan file
		latest, err := findLatestScan()
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

// findLatestScan finds the most recent scan JSON file in the output directory.
func findLatestScan() (string, error) {
	entries, err := os.ReadDir(OutputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no output directory found. Run 'tagctl scan' first")
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
		return "", fmt.Errorf("no scan files found in %s. Run 'tagctl scan' first", OutputDir)
	}

	return OutputDir + "/" + latestFile, nil
}

func savePlanAndOutput(plan *types.Plan, outFile, format string) error {
	// Save plan to file
	planFile, err := GetPlanPath(outFile)
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

	if format == formatJSON {
		if err := outputPlanJSON(plan); err != nil {
			return err
		}
	} else {
		outputPlanTable(plan)
	}

	if !plan.IsEmpty() {
		fmt.Fprintf(os.Stderr, "\nPlan saved to: %s\nRun 'tagctl apply' to execute this plan.\n", planFile)
	}
	return nil
}

// getMockPlan returns a mock plan for demonstration purposes.
// TODO: Remove this when real planner is implemented.
func getMockPlan() *types.Plan {
	plan := &types.Plan{
		ID:        fmt.Sprintf("plan-%s", time.Now().Format("20060102-150405")),
		CreatedAt: time.Now(),
		Changes: []types.TagChange{
			{
				Resource: types.Resource{
					ID:       "i-0abc123",
					Name:     "web-prod-api-1",
					Type:     "aws_instance",
					Account:  "production",
					Provider: "aws",
				},
				Tag:      "environment",
				Action:   types.ActionAdd,
				NewValue: valueProd,
				Reason:   types.ReasonInferred,
				Source:   "name contains '-prod-'",
			},
			{
				Resource: types.Resource{
					ID:       "i-0abc123",
					Name:     "web-prod-api-1",
					Type:     "aws_instance",
					Account:  "production",
					Provider: "aws",
				},
				Tag:      "team",
				Action:   types.ActionAdd,
				NewValue: "backend",
				Reason:   types.ReasonInherited,
				Source:   "from ASG 'backend-asg'",
			},
			{
				Resource: types.Resource{
					ID:       "vol-xyz789",
					Name:     "",
					Type:     "aws_ebs_volume",
					Account:  "production",
					Provider: "aws",
				},
				Tag:      "environment",
				Action:   types.ActionAdd,
				NewValue: valueProd,
				Reason:   types.ReasonInherited,
				Source:   "from attached instance i-0abc123",
			},
			{
				Resource: types.Resource{
					ID:       "legacy-bucket",
					Name:     "legacy-data-2019",
					Type:     "aws_s3_bucket",
					Account:  "production",
					Provider: "aws",
				},
				Tag:      "owner",
				Action:   types.ActionAdd,
				NewValue: "platform-team@company.com",
				Reason:   types.ReasonDefault,
				Source:   "default for untagged S3 buckets",
			},
			{
				Resource: types.Resource{
					ID:       "legacy-bucket",
					Name:     "legacy-data-2019",
					Type:     "aws_s3_bucket",
					Account:  "production",
					Provider: "aws",
				},
				Tag:      "needs-review",
				Action:   types.ActionAdd,
				NewValue: "true",
				Reason:   types.ReasonDefault,
				Source:   "default for untagged S3 buckets",
			},
		},
		Summary: types.PlanSummary{
			TotalResources: 3,
			TotalChanges:   5,
			TagsAdded:      5,
			TagsUpdated:    0,
			TagsRemoved:    0,
		},
	}

	return plan
}

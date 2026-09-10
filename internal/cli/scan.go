package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/unicrons/tagctl/internal/engine"
	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/types"
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan cloud resources and report tag compliance",
	Long: `Scan connects to configured cloud accounts and evaluates
resources against your tagging policy.

The scan will:
  • Discover all taggable resources
  • Evaluate each resource against required tags
  • Generate a compliance report

Examples:
  # Scan all regions (default)
  tagctl scan

  # Scan specific region(s)
  tagctl scan --region eu-west-1
  tagctl scan --region eu-west-1 --region us-east-1

  # Scan with specific config file
  tagctl scan --config production.yaml

  # Pick the AWS profile or assume a role without touching the config
  tagctl scan --profile production
  tagctl scan --role arn:aws:iam::123456789012:role/TagAudit --external-id 1234

  # Output as JSON
  tagctl scan --output json`,
	RunE: runScan,
}

func init() {
	scanCmd.Flags().Bool("verbose", false, "show detailed violation information")
	scanCmd.Flags().Bool("mock", false, "use mock data for demonstration")
	scanCmd.Flags().StringSlice("region", nil, "AWS region(s) to scan (default: all available regions)")
	addAWSAuthFlags(scanCmd)
	addGateFlags(scanCmd)
}

func runScan(cmd *cobra.Command, args []string) error {
	verbose, _ := cmd.Flags().GetBool("verbose")
	useMock, _ := cmd.Flags().GetBool("mock")
	regions, _ := cmd.Flags().GetStringSlice("region")
	gateOpts := readGateFlags(cmd)

	ctx, stop := signalContext()
	defer stop()

	// Always print the banner
	printBanner()

	// Load configuration
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if err = applyAWSAuthFlags(cfg, readAWSAuthFlags(cmd)); err != nil {
		return err
	}

	var result *types.ScanResult

	// Use mock scanner if requested or if no providers are configured
	if useMock || !hasConfiguredProviders(cfg) {
		if !useMock && !hasConfiguredProviders(cfg) {
			printDemoModeWarning()
		}

		fmt.Println("Scanning cloud resources (demo mode)...")
		fmt.Println()

		scanner := engine.NewMockScanner()
		result, err = scanner.Scan(ctx)
		if err != nil {
			return fmt.Errorf("scan failed: %w", err)
		}
	} else {
		// Initialize providers with spinner
		spinner := NewSpinner("Initializing cloud providers...")
		spinner.Start()

		providers, provErr := initProviders(ctx, cfg, regions)
		if provErr != nil {
			spinner.Fail("Failed to initialize providers")
			return provErr
		}
		spinner.Success(fmt.Sprintf("Initialized %d provider(s)", len(providers)))

		// Create scanner
		scanner, scannerErr := engine.NewScanner(providers, cfg.Policy, cfg.Ignore)
		if scannerErr != nil {
			return fmt.Errorf("failed to create scanner: %w", scannerErr)
		}

		// Run scan with spinner
		spinner = NewSpinner("Discovering resources...")
		spinner.Start()

		result, err = scanner.Scan(ctx)
		if err != nil {
			spinner.Fail(fmt.Sprintf("Scan completed with errors: %v", err))
		} else {
			spinner.Success(fmt.Sprintf("Discovered %d resources", result.TotalResources))
		}
	}

	outputPaths := writeScanReports(result)

	if err := printScanResult(result, verbose); err != nil {
		return err
	}

	if outputPaths != nil {
		printOutputFilesBanner(outputPaths)
	}

	// The CI reports are written before the gate runs, so a failing build
	// still uploads its findings.
	if reportErr := gateOpts.writeReports(result, cfgFile); reportErr != nil {
		return reportErr
	}

	gateResult, gateErr := gateOpts.evaluate(result)
	if gateErr != nil {
		return gateErr
	}
	if !gateResult.Passed {
		return gateResult.Error()
	}

	return nil
}

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorCyan   = "\033[36m"
	colorYellow = "\033[33m"
	colorGreen  = "\033[32m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

// printBanner prints the tagctl banner with colors and version.
func printBanner() {
	fmt.Printf(`
%s%s  ████████╗ █████╗  ██████╗  ██████╗████████╗██╗     %s
%s  ╚══██╔══╝██╔══██╗██╔════╝ ██╔════╝╚══██╔══╝██║     %s
%s     ██║   ███████║██║  ███╗██║        ██║   ██║     %s
%s     ██║   ██╔══██║██║   ██║██║        ██║   ██║     %s
%s     ██║   ██║  ██║╚██████╔╝╚██████╗   ██║   ███████╗%s
%s     ╚═╝   ╚═╝  ╚═╝ ╚═════╝  ╚═════╝   ╚═╝   ╚══════╝%s
%s        Audit, fix, and enforce cloud resource tags%s
%s                                            v%s%s

`,
		colorBold, colorCyan, colorReset,
		colorCyan, colorReset,
		colorCyan, colorReset,
		colorCyan, colorReset,
		colorCyan, colorReset,
		colorCyan, colorReset,
		colorDim, colorReset,
		colorDim, appVersion, colorReset,
	)
}

// printDemoModeWarning prints a warning explaining that mock data is being used.
func printDemoModeWarning() {
	fmt.Printf(`%s%s┌─────────────────────────────────────────────────────────────────┐%s
%s│                         ⚠  DEMO MODE                           │%s
%s├─────────────────────────────────────────────────────────────────┤%s
%s│  No cloud providers configured. Showing example data.          │%s
%s│                                                                 │%s
%s│  To scan real resources:                                        │%s
%s│    1. Run '%stagctl init%s' to create a configuration file          │%s
%s│    2. Edit tagctl.yaml to add your cloud accounts               │%s
%s│    3. Run '%stagctl scan%s' again                                    │%s
%s│                                                                 │%s
%s│  More info: %shttps://github.com/unicrons/tagctl%s                   │%s
%s└─────────────────────────────────────────────────────────────────┘%s

`,
		colorBold, colorYellow, colorReset,
		colorYellow, colorReset,
		colorYellow, colorReset,
		colorYellow, colorReset,
		colorYellow, colorReset,
		colorYellow, colorReset,
		colorYellow, colorGreen, colorYellow, colorReset,
		colorYellow, colorReset,
		colorYellow, colorGreen, colorYellow, colorReset,
		colorYellow, colorReset,
		colorYellow, colorCyan, colorYellow, colorReset,
		colorYellow, colorReset,
	)
}

// printOutputFilesBanner prints the location of generated output files.
func printOutputFilesBanner(paths *ScanOutputPaths) {
	// Get absolute paths for clearer output
	absJSON := getAbsolutePath(paths.JSON)
	absCSV := getAbsolutePath(paths.CSV)
	absHTML := getAbsolutePath(paths.HTML)

	fmt.Println()
	fmt.Printf("%s%sDetailed results saved to:%s\n", colorBold, colorCyan, colorReset)
	fmt.Printf("  %s•%s JSON: %s%s%s\n", colorGreen, colorReset, colorDim, absJSON, colorReset)
	fmt.Printf("  %s•%s CSV:  %s%s%s\n", colorGreen, colorReset, colorDim, absCSV, colorReset)
	fmt.Printf("  %s•%s HTML: %s%s%s\n", colorGreen, colorReset, colorDim, absHTML, colorReset)
	fmt.Println()
}

// getAbsolutePath returns the absolute path, or the original if it fails.
func getAbsolutePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// writeScanReports writes the JSON, CSV and HTML reports for a scan. A report
// that cannot be written is logged and skipped rather than failing the scan,
// since the results are already in hand. Returns nil when the output directory
// could not be resolved.
func writeScanReports(result *types.ScanResult) *ScanOutputPaths {
	outputPaths, err := GetScanOutputPaths()
	if err != nil {
		log.Error("Failed to get output paths: %v", err)
		return nil
	}

	log.Debug("Writing output files...")

	if writeErr := writeScanJSON(result, outputPaths.JSON); writeErr != nil {
		log.Error("Failed to write JSON: %v", writeErr)
	}
	if writeErr := writeScanCSV(result, outputPaths.CSV); writeErr != nil {
		log.Error("Failed to write CSV: %v", writeErr)
	}
	if writeErr := writeScanHTML(result, outputPaths.HTML); writeErr != nil {
		log.Error("Failed to write HTML: %v", writeErr)
	}

	return outputPaths
}

// printScanResult renders a scan to stdout in the requested format.
func printScanResult(result *types.ScanResult, verbose bool) error {
	switch outputFormat {
	case formatJSON:
		return outputScanJSON(result)
	case formatCSV:
		return outputScanCSV(result)
	default:
		return outputScanTable(result, verbose)
	}
}

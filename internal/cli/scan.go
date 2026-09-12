package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/unicrons/tagctl/internal/config"
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

When discovery fails for part of the estate (a region or a service the role
cannot list), the reports are still written and marked partial, then the scan
fails. Pass --allow-partial to accept a partial scan with a warning.

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
  tagctl scan --role arn:aws:iam::123456789012:role/TagctlScan --external-id 1234

  # Output as JSON
  tagctl scan --output json

  # Accept a scan where some regions or services could not be listed
  tagctl scan --allow-partial`,
	RunE: runScan,
}

func init() {
	scanCmd.Flags().Bool("verbose", false, "show detailed violation information")
	scanCmd.Flags().Bool("mock", false, "use mock data for demonstration")
	scanCmd.Flags().Bool("allow-partial", false, "succeed with a warning when discovery failed for part of the estate")
	scanCmd.Flags().StringSlice("region", nil, "AWS region(s) to scan (default: all available regions)")
	addAWSAuthFlags(scanCmd)
	addGateFlags(scanCmd)
}

// scanProviders builds the providers a real scan runs against; tests replace it.
var scanProviders = initProviders

func runScan(cmd *cobra.Command, args []string) error {
	verbose, _ := cmd.Flags().GetBool("verbose")
	useMock, _ := cmd.Flags().GetBool("mock")
	allowPartial, _ := cmd.Flags().GetBool("allow-partial")
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
	var discoveryErr error

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
		result, discoveryErr = discoverResources(ctx, cfg, regions)
		if result == nil {
			return discoveryErr
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

	if discoveryErr != nil {
		if !allowPartial {
			return fmt.Errorf("scan is partial, pass --allow-partial to accept it: %w", discoveryErr)
		}
		fmt.Fprintf(os.Stderr, "Warning: accepting a partial scan (%d provider(s) failed discovery); resources may be missing from the results\n", len(result.Errors))
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

// discoverResources scans the configured providers. A partial scan returns
// both the result and the discovery error; any other failure returns no result.
func discoverResources(ctx context.Context, cfg *config.Config, regions []string) (*types.ScanResult, error) {
	spinner := NewSpinner("Initializing cloud providers...")
	spinner.Start()

	providers, err := scanProviders(ctx, cfg, regions)
	if err != nil {
		spinner.Fail("Failed to initialize providers")
		return nil, err
	}
	spinner.Success(fmt.Sprintf("Initialized %d provider(s)", len(providers)))

	scanner, err := engine.NewScanner(providers, cfg.Policy, cfg.Ignore)
	if err != nil {
		return nil, fmt.Errorf("failed to create scanner: %w", err)
	}

	spinner = NewSpinner("Discovering resources...")
	spinner.Start()

	result, err := scanner.Scan(ctx)
	switch {
	case result == nil:
		spinner.Fail("Scan failed")
	case err != nil:
		spinner.Fail(fmt.Sprintf("Discovered %d resources, but discovery failed for part of the estate", result.TotalResources))
	default:
		spinner.Success(fmt.Sprintf("Discovered %d resources", result.TotalResources))
	}
	return result, err
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

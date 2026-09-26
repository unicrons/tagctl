package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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

  # Only evaluate some resource types (path.Match globs, quoted for the shell)
  tagctl scan --resource-type 'aws_s3_*' --resource-type aws_instance

  # Scan with specific config file
  tagctl scan --config production.yaml

  # Pick the AWS profile or assume a role without touching the config
  tagctl scan --profile production
  tagctl scan --role arn:aws:iam::123456789012:role/TagctlScan --external-id 1234

  # Output as JSON
  tagctl scan --output json

  # Accept a scan where some regions or services could not be listed
  tagctl scan --allow-partial

  # Write the reports somewhere else, or not at all
  tagctl scan --output-dir reports
  tagctl scan --no-files --sarif tagctl.sarif`,
	RunE: runScan,
}

func init() {
	scanCmd.Flags().Bool("verbose", false, "show detailed violation information")
	scanCmd.Flags().Bool("mock", false, "use mock data for demonstration")
	scanCmd.Flags().Bool("allow-partial", false, "succeed with a warning when discovery failed for part of the estate")
	scanCmd.Flags().StringSlice("region", nil, "AWS region(s) to scan (default: all available regions)")
	scanCmd.Flags().StringArray(flagResourceType, nil, "only evaluate and report resource types matching this glob, e.g. 'aws_s3_*' (repeatable; default: every type)")
	addOutputDirFlag(scanCmd, "directory to write the JSON, CSV and HTML reports to")
	scanCmd.Flags().Bool(flagNoFiles, false, "write no JSON, CSV or HTML report files")
	scanCmd.MarkFlagsMutuallyExclusive(flagOutputDir, flagNoFiles)
	addAWSAuthFlags(scanCmd)
	addGateFlags(scanCmd)
}

const (
	flagNoFiles      = "no-files"
	flagResourceType = "resource-type"
)

// scanProviders builds the providers a real scan runs against; tests replace it.
var scanProviders = initProviders

// scanOptions holds the scan flags that are not gate or auth flags.
type scanOptions struct {
	verbose       bool
	mock          bool
	allowPartial  bool
	noFiles       bool
	regions       []string
	resourceTypes []string
	outputDir     string
}

// readScanFlags parses and validates the scan flags.
func readScanFlags(cmd *cobra.Command) (scanOptions, error) {
	var opts scanOptions
	opts.verbose, _ = cmd.Flags().GetBool("verbose")
	opts.mock, _ = cmd.Flags().GetBool("mock")
	opts.allowPartial, _ = cmd.Flags().GetBool("allow-partial")
	opts.noFiles, _ = cmd.Flags().GetBool(flagNoFiles)
	opts.regions, _ = cmd.Flags().GetStringSlice("region")

	opts.resourceTypes, _ = cmd.Flags().GetStringArray(flagResourceType)
	// pflag reads a lone "" back as no value at all.
	if cmd.Flags().Changed(flagResourceType) && len(opts.resourceTypes) == 0 {
		opts.resourceTypes = []string{""}
	}
	if err := engine.ValidateTypePatterns(opts.resourceTypes); err != nil {
		return opts, fmt.Errorf("--%s: %w", flagResourceType, err)
	}

	var err error
	opts.outputDir, err = outputDirFor(cmd)
	return opts, err
}

func runScan(cmd *cobra.Command, args []string) error {
	opts, err := readScanFlags(cmd)
	if err != nil {
		return err
	}
	gateOpts := readGateFlags(cmd)
	format, err := gateOpts.stdoutFormat(cmd, formatTable, formatJSON, formatCSV)
	if err != nil {
		return err
	}

	ctx, stop := signalContext()
	defer stop()

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
	if opts.mock || !hasConfiguredProviders(cfg) {
		if !opts.mock {
			printDemoModeWarning()
		}
		result, err = mockScan(ctx, opts)
		if err != nil {
			return err
		}
	} else {
		result, discoveryErr = discoverResources(ctx, cfg, opts.regions, opts.resourceTypes)
		if result == nil {
			return discoveryErr
		}
	}

	var outputPaths *ScanOutputPaths
	if !opts.noFiles {
		outputPaths = writeScanReports(result, opts.outputDir)
	}

	if err := printScanResult(result, format, opts.verbose); err != nil {
		return err
	}

	if outputPaths != nil {
		printOutputFilesBanner(outputPaths)
	}

	// The CI reports are written before the gate runs, so a failing build
	// still uploads its findings.
	if reportErr := gateOpts.writeReports(result, viper.ConfigFileUsed()); reportErr != nil {
		return reportErr
	}

	if discoveryErr != nil {
		if !opts.allowPartial {
			return fmt.Errorf("scan is partial, pass --allow-partial to accept it: %w", discoveryErr)
		}
		fmt.Fprintf(os.Stderr, "Warning: accepting a partial scan (%d provider(s) failed discovery); resources may be missing from the results\n", len(result.Errors))
	}

	return gateOpts.check(result)
}

// mockScan returns the fixed demo scan.
func mockScan(ctx context.Context, opts scanOptions) (*types.ScanResult, error) {
	if len(opts.resourceTypes) > 0 {
		fmt.Fprintf(os.Stderr, "Warning: --%s is ignored in demo mode, the mock data is fixed\n", flagResourceType)
	}

	fmt.Fprint(os.Stderr, "Scanning cloud resources (demo mode)...\n\n")

	result, err := engine.NewMockScanner().Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}
	return result, nil
}

// discoverResources scans the configured providers. A partial scan returns
// both the result and the discovery error; any other failure returns no result.
// resourceTypes, when set, keeps only the resources whose type matches a glob.
func discoverResources(ctx context.Context, cfg *config.Config, regions, resourceTypes []string) (*types.ScanResult, error) {
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
	if err = scanner.OnlyTypes(resourceTypes); err != nil {
		return nil, fmt.Errorf("--%s: %w", flagResourceType, err)
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
	if result != nil && len(resourceTypes) > 0 && result.TotalResources == 0 {
		fmt.Fprintf(os.Stderr, "Warning: no discovered resource matches --%s %s\n", flagResourceType, printable(strings.Join(resourceTypes, ", ")))
	}
	return result, err
}

// printBanner prints the tagctl banner with colors and version to stderr.
func printBanner() {
	c := paletteFor(os.Stderr)
	fmt.Fprintf(os.Stderr, `
%s%s  ████████╗ █████╗  ██████╗  ██████╗████████╗██╗     %s
%s  ╚══██╔══╝██╔══██╗██╔════╝ ██╔════╝╚══██╔══╝██║     %s
%s     ██║   ███████║██║  ███╗██║        ██║   ██║     %s
%s     ██║   ██╔══██║██║   ██║██║        ██║   ██║     %s
%s     ██║   ██║  ██║╚██████╔╝╚██████╗   ██║   ███████╗%s
%s     ╚═╝   ╚═╝  ╚═╝ ╚═════╝  ╚═════╝   ╚═╝   ╚══════╝%s
%s        Audit, fix, and enforce cloud resource tags%s
%s                                            v%s%s

`,
		c.bold, c.cyan, c.reset,
		c.cyan, c.reset,
		c.cyan, c.reset,
		c.cyan, c.reset,
		c.cyan, c.reset,
		c.cyan, c.reset,
		c.dim, c.reset,
		c.dim, Version, c.reset,
	)
}

// printDemoModeWarning warns on stderr that mock data is being used.
func printDemoModeWarning() {
	c := paletteFor(os.Stderr)
	fmt.Fprintf(os.Stderr, `%s%s┌─────────────────────────────────────────────────────────────────┐%s
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
		c.bold, c.yellow, c.reset,
		c.yellow, c.reset,
		c.yellow, c.reset,
		c.yellow, c.reset,
		c.yellow, c.reset,
		c.yellow, c.reset,
		c.yellow, c.green, c.yellow, c.reset,
		c.yellow, c.reset,
		c.yellow, c.green, c.yellow, c.reset,
		c.yellow, c.reset,
		c.yellow, c.cyan, c.yellow, c.reset,
		c.yellow, c.reset,
	)
}

// printOutputFilesBanner prints the location of generated output files to stderr.
func printOutputFilesBanner(paths *ScanOutputPaths) {
	// Get absolute paths for clearer output
	absJSON := printable(getAbsolutePath(paths.JSON))
	absCSV := printable(getAbsolutePath(paths.CSV))
	absHTML := printable(getAbsolutePath(paths.HTML))
	c := paletteFor(os.Stderr)

	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s%sDetailed results saved to:%s\n", c.bold, c.cyan, c.reset)
	fmt.Fprintf(os.Stderr, "  %s•%s JSON: %s%s%s\n", c.green, c.reset, c.dim, absJSON, c.reset)
	fmt.Fprintf(os.Stderr, "  %s•%s CSV:  %s%s%s\n", c.green, c.reset, c.dim, absCSV, c.reset)
	fmt.Fprintf(os.Stderr, "  %s•%s HTML: %s%s%s\n", c.green, c.reset, c.dim, absHTML, c.reset)
	fmt.Fprintln(os.Stderr)
}

// getAbsolutePath returns the absolute path, or the original if it fails.
func getAbsolutePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// writeScanReports writes the JSON, CSV and HTML reports for a scan to dir. A report
// that cannot be written is logged and skipped rather than failing the scan,
// since the results are already in hand. Returns nil when the output directory
// could not be resolved.
func writeScanReports(result *types.ScanResult, dir string) *ScanOutputPaths {
	outputPaths, err := GetScanOutputPaths(dir)
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

// printScanResult renders a scan to stdout in format; "" prints nothing.
func printScanResult(result *types.ScanResult, format string, verbose bool) error {
	switch format {
	case "":
		return nil
	case formatJSON:
		return outputScanJSON(result)
	case formatCSV:
		return outputScanCSV(result)
	default:
		return outputScanTable(result, verbose)
	}
}

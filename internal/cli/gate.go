package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/unicrons/tagctl/internal/engine"
	"github.com/unicrons/tagctl/internal/report"
	"github.com/unicrons/tagctl/internal/types"
)

// addGateFlags registers the flags that turn a command into a pipeline gate.
func addGateFlags(cmd *cobra.Command) {
	cmd.Flags().Float64("fail-under", 0, "exit 1 if compliance is below this percentage (0 disables the check)")
	cmd.Flags().Bool("fail-on-new", false, "exit 1 if any finding regressed against --baseline")
	cmd.Flags().String("baseline", "", "baseline scan file to compare against for --fail-on-new")
	cmd.Flags().String("sarif", "", "also write findings as SARIF to this path (- for stdout)")
	cmd.Flags().String("junit", "", "also write findings as JUnit XML to this path (- for stdout)")
	cmd.Flags().String("ocsf", "", "also write findings as OCSF Compliance Finding events to this path (- for stdout)")
}

// gateOptions holds the parsed gate flags for one command run.
type gateOptions struct {
	gate      report.Gate
	baseline  string
	sarifPath string
	junitPath string
	ocsfPath  string
}

// readGateFlags parses the gate flags from a command.
func readGateFlags(cmd *cobra.Command) gateOptions {
	failUnder, _ := cmd.Flags().GetFloat64("fail-under")
	failOnNew, _ := cmd.Flags().GetBool("fail-on-new")
	baseline, _ := cmd.Flags().GetString("baseline")
	sarifPath, _ := cmd.Flags().GetString("sarif")
	junitPath, _ := cmd.Flags().GetString("junit")
	ocsfPath, _ := cmd.Flags().GetString("ocsf")

	return gateOptions{
		gate:      report.Gate{FailUnder: failUnder, FailOnNew: failOnNew},
		baseline:  baseline,
		sarifPath: sarifPath,
		junitPath: junitPath,
		ocsfPath:  ocsfPath,
	}
}

// writeReports writes any machine-readable reports the flags asked for.
func (o gateOptions) writeReports(scan *types.ScanResult, policyFile string) error {
	if o.sarifPath != "" {
		if err := writeToPathOrStdout(o.sarifPath, func(f *os.File) error {
			return report.WriteSARIF(f, scan, report.SARIFOptions{
				PolicyFile: policyFile,
				Version:    Version,
			})
		}); err != nil {
			return fmt.Errorf("failed to write SARIF report: %w", err)
		}
	}

	if o.junitPath != "" {
		if err := writeToPathOrStdout(o.junitPath, func(f *os.File) error {
			return report.WriteJUnit(f, scan)
		}); err != nil {
			return fmt.Errorf("failed to write JUnit report: %w", err)
		}
	}

	if o.ocsfPath != "" {
		if err := writeToPathOrStdout(o.ocsfPath, func(f *os.File) error {
			return report.WriteOCSF(f, scan, report.OCSFOptions{Version: Version})
		}); err != nil {
			return fmt.Errorf("failed to write OCSF report: %w", err)
		}
	}

	return nil
}

// evaluate applies the gate, loading the baseline scan when one is needed.
func (o gateOptions) evaluate(scan *types.ScanResult) (*report.GateResult, error) {
	var diff *types.DiffResult

	if o.gate.FailOnNew {
		if o.baseline == "" {
			return nil, fmt.Errorf("--fail-on-new needs a --baseline scan to compare against")
		}
		baseline, err := LoadScanFile(o.baseline)
		if err != nil {
			return nil, err
		}
		warnPartialScan(os.Stderr, o.baseline, baseline, "--fail-on-new may count resources it missed as regressions")
		diff = engine.Diff(baseline, scan)
	}

	return o.gate.Evaluate(scan, diff), nil
}

// check applies the gate and returns a gate failure when the scan does not pass.
func (o gateOptions) check(scan *types.ScanResult) error {
	result, err := o.evaluate(scan)
	if err != nil {
		return err
	}
	if !result.Passed {
		return gateFailed(result.Error())
	}
	return nil
}

// writeToPathOrStdout runs write against the named file, or stdout for "-".
func writeToPathOrStdout(path string, write func(*os.File) error) error {
	if path == "-" {
		return write(os.Stdout)
	}

	// #nosec G304 -- the report path is supplied by the user running the CLI.
	file, err := os.Create(filepath.Clean(path))
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	return write(file)
}

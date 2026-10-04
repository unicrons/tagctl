package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	cmd.Flags().String("sarif", "", "also write findings as SARIF to this path (- for stdout, instead of the command's output)")
	cmd.Flags().String("junit", "", "also write findings as JUnit XML to this path (- for stdout, instead of the command's output)")
	cmd.Flags().String("ocsf", "", "also write findings as OCSF Compliance Finding events to this path (- for stdout, instead of the command's output)")
	cmd.Flags().String("summary", "", "also write a Markdown summary to this path, e.g. \"$GITHUB_STEP_SUMMARY\" (- for stdout, instead of the command's output)")
}

// gateOptions holds the parsed gate flags for one command run.
type gateOptions struct {
	command     string
	gate        report.Gate
	baseline    string
	sarifPath   string
	junitPath   string
	ocsfPath    string
	summaryPath string
}

// readGateFlags parses the gate flags from a command.
func readGateFlags(cmd *cobra.Command) gateOptions {
	failUnder, _ := cmd.Flags().GetFloat64("fail-under")
	failOnNew, _ := cmd.Flags().GetBool("fail-on-new")
	baseline, _ := cmd.Flags().GetString("baseline")
	sarifPath, _ := cmd.Flags().GetString("sarif")
	junitPath, _ := cmd.Flags().GetString("junit")
	ocsfPath, _ := cmd.Flags().GetString("ocsf")
	summaryPath, _ := cmd.Flags().GetString("summary")

	return gateOptions{
		command:     cmd.Name(),
		gate:        report.Gate{FailUnder: failUnder, FailOnNew: failOnNew},
		baseline:    baseline,
		sarifPath:   sarifPath,
		junitPath:   junitPath,
		ocsfPath:    ocsfPath,
		summaryPath: summaryPath,
	}
}

// stdoutFormat resolves what the command prints on stdout: its -o format, or
// "" when a report is written there instead. Only one report may take stdout.
func (o gateOptions) stdoutFormat(cmd *cobra.Command, supported ...string) (string, error) {
	format, err := outputFormatFor(cmd, supported...)
	if err != nil {
		return "", err
	}

	var onStdout []string
	for _, report := range []struct{ flag, path string }{
		{"--sarif", o.sarifPath},
		{"--junit", o.junitPath},
		{"--ocsf", o.ocsfPath},
		{"--summary", o.summaryPath},
	} {
		if report.path == "-" {
			onStdout = append(onStdout, report.flag)
		}
	}

	switch {
	case len(onStdout) == 0:
		return format, nil
	case len(onStdout) > 1:
		return "", fmt.Errorf("%s both write to stdout: send at most one report to -", strings.Join(onStdout, " and "))
	case cmd.Flags().Changed("output"):
		return "", fmt.Errorf("%s - replaces the %s output on stdout: drop -o or write the report to a file", onStdout[0], format)
	default:
		return "", nil
	}
}

// writeReports writes any machine-readable reports the flags asked for.
// policyFile is the config file the command read, "" when there was none.
func (o gateOptions) writeReports(scan *types.ScanResult, policyFile string) error {
	if o.sarifPath != "" {
		if err := writeToPathOrStdout(o.sarifPath, func(f *os.File) error {
			return report.WriteSARIF(f, scan, report.SARIFOptions{
				PolicyFile: artifactURI(policyFile),
				Command:    o.command,
				Version:    appVersion,
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
			return report.WriteOCSF(f, scan, report.OCSFOptions{Version: appVersion})
		}); err != nil {
			return fmt.Errorf("failed to write OCSF report: %w", err)
		}
	}

	if o.summaryPath != "" {
		if err := writeToPathOrStdout(o.summaryPath, func(f *os.File) error {
			return report.WriteMarkdown(f, scan, report.MarkdownOptions{Version: appVersion})
		}); err != nil {
			return fmt.Errorf("failed to write Markdown summary: %w", err)
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

// artifactURI is path relative to the working directory with forward slashes,
// which is how code scanning resolves a file in the checked-out repository.
func artifactURI(path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		if wd, err := os.Getwd(); err == nil {
			if rel, relErr := filepath.Rel(wd, path); relErr == nil {
				path = rel
			}
		}
	}
	return filepath.ToSlash(filepath.Clean(path))
}

// writeToPathOrStdout runs write against the named file, or stdout for "-".
func writeToPathOrStdout(path string, write func(*os.File) error) error {
	if path == "-" {
		return write(os.Stdout)
	}

	file, err := createReport(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	return write(file)
}

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// valueProd is the environment tag value used across this package's fixtures
// and sample plans.
const valueProd = "prod"

// Output formats accepted by the -o/--output flag.
const (
	formatTable = "table"
	formatJSON  = "json"
	formatCSV   = "csv"
)

// OutputDir is the directory scans and plans go to unless --output-dir names another.
var OutputDir = "output"

const flagOutputDir = "output-dir"

// addOutputDirFlag registers --output-dir on a command that reads or writes scan and plan files.
func addOutputDirFlag(cmd *cobra.Command, usage string) {
	cmd.Flags().String(flagOutputDir, OutputDir, usage)
}

// outputDirFor resolves the directory a command reads and writes its files in.
func outputDirFor(cmd *cobra.Command) (string, error) {
	if !cmd.Flags().Changed(flagOutputDir) {
		return OutputDir, nil
	}
	dir, _ := cmd.Flags().GetString(flagOutputDir)
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("--%s needs a directory", flagOutputDir)
	}
	return filepath.Clean(dir), nil
}

// EnsureOutputDir creates dir if it doesn't exist.
func EnsureOutputDir(dir string) error {
	return os.MkdirAll(dir, 0o750)
}

// GetPlanPath returns the full path for a plan file: a bare filename lands in
// dir, an empty one gets a timestamped name there.
func GetPlanPath(dir, filename string) (string, error) {
	if filename != "" {
		// If absolute path or explicitly specified, use as-is
		if filepath.IsAbs(filename) || strings.Contains(filename, string(filepath.Separator)) {
			return filename, nil
		}
		// Otherwise, put it in the output directory
		if err := EnsureOutputDir(dir); err != nil {
			return "", fmt.Errorf("failed to create output directory: %w", err)
		}
		return filepath.Join(dir, filename), nil
	}

	// Generate timestamped filename in output directory
	if err := EnsureOutputDir(dir); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}
	return filepath.Join(dir, fmt.Sprintf("plan-%s.json", time.Now().Format("20060102-150405"))), nil
}

// ScanOutputPaths holds the paths for scan output files.
type ScanOutputPaths struct {
	JSON string
	CSV  string
	HTML string
}

// GetScanOutputPaths returns timestamped paths for the scan output files in dir.
func GetScanOutputPaths(dir string) (*ScanOutputPaths, error) {
	if err := EnsureOutputDir(dir); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	base := filepath.Join(dir, fmt.Sprintf("scan-%s", timestamp))

	return &ScanOutputPaths{
		JSON: base + ".json",
		CSV:  base + ".csv",
		HTML: base + ".html",
	}, nil
}

// FindLatestPlanInDir finds the most recent plan file in dir.
func FindLatestPlanInDir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("output directory %s not found. Run 'tagctl plan' first", dir)
		}
		return "", fmt.Errorf("failed to read output directory: %w", err)
	}

	var latestPlan string
	var latestTime time.Time

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasPrefix(entry.Name(), "plan-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latestPlan = filepath.Join(dir, entry.Name())
		}
	}

	if latestPlan == "" {
		return "", fmt.Errorf("no plan files found in %s. Run 'tagctl plan' first", dir)
	}

	return latestPlan, nil
}

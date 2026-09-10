package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
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

// OutputDir is the default directory for all tagctl outputs (plans, reports, etc.)
var OutputDir = "output"

// EnsureOutputDir creates the output directory if it doesn't exist.
func EnsureOutputDir() error {
	return os.MkdirAll(OutputDir, 0o750)
}

// GetPlanPath returns the full path for a plan file.
// If filename is empty, generates a timestamped filename.
func GetPlanPath(filename string) (string, error) {
	if filename != "" {
		// If absolute path or explicitly specified, use as-is
		if filepath.IsAbs(filename) || strings.Contains(filename, string(filepath.Separator)) {
			return filename, nil
		}
		// Otherwise, put it in the output directory
		if err := EnsureOutputDir(); err != nil {
			return "", fmt.Errorf("failed to create output directory: %w", err)
		}
		return filepath.Join(OutputDir, filename), nil
	}

	// Generate timestamped filename in output directory
	if err := EnsureOutputDir(); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}
	return filepath.Join(OutputDir, fmt.Sprintf("plan-%s.json", time.Now().Format("20060102-150405"))), nil
}

// ScanOutputPaths holds the paths for scan output files.
type ScanOutputPaths struct {
	JSON string
	CSV  string
	HTML string
}

// GetScanOutputPaths returns paths for scan output files with a timestamp.
func GetScanOutputPaths() (*ScanOutputPaths, error) {
	if err := EnsureOutputDir(); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	base := filepath.Join(OutputDir, fmt.Sprintf("scan-%s", timestamp))

	return &ScanOutputPaths{
		JSON: base + ".json",
		CSV:  base + ".csv",
		HTML: base + ".html",
	}, nil
}

// FindLatestPlanInDir finds the most recent plan file in the output directory.
func FindLatestPlanInDir() (string, error) {
	entries, err := os.ReadDir(OutputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no output directory found. Run 'tagctl plan' first")
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
			latestPlan = filepath.Join(OutputDir, entry.Name())
		}
	}

	if latestPlan == "" {
		return "", fmt.Errorf("no plan files found in %s. Run 'tagctl plan' first", OutputDir)
	}

	return latestPlan, nil
}

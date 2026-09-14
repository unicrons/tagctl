package cli

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/cobra"

	"github.com/unicrons/tagctl/internal/types"
)

// commandWithGateFlags builds a bare command carrying the gate flags.
func commandWithGateFlags(t *testing.T, args ...string) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{Use: "test", RunE: func(*cobra.Command, []string) error { return nil }}
	addGateFlags(cmd)
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("parsing flags %v: %v", args, err)
	}
	return cmd
}

func failedScan(pct float64) *types.ScanResult {
	return &types.ScanResult{
		CompliancePct: pct,
		Findings: []types.Finding{
			{
				Resource: types.Resource{ID: "i-1", Type: "aws_instance", Account: "111", Provider: "aws"},
				Tag:      "owner",
				Status:   types.StatusFailed,
				Reason:   types.ReasonMissing,
			},
		},
	}
}

func TestReadGateFlags_Defaults(t *testing.T) {
	opts := readGateFlags(commandWithGateFlags(t))

	if opts.gate.IsEnabled() {
		t.Error("gate is enabled with no flags set")
	}
	if opts.sarifPath != "" || opts.junitPath != "" || opts.ocsfPath != "" || opts.baseline != "" {
		t.Errorf("unexpected defaults: %+v", opts)
	}
}

func TestReadGateFlags_Parsed(t *testing.T) {
	opts := readGateFlags(commandWithGateFlags(t,
		"--fail-under", "85.5",
		"--fail-on-new",
		"--baseline", "base.json",
		"--sarif", "out.sarif",
		"--junit", "out.xml",
		"--ocsf", "out.ocsf.json",
	))

	if opts.gate.FailUnder != 85.5 {
		t.Errorf("FailUnder = %v, want 85.5", opts.gate.FailUnder)
	}
	if !opts.gate.FailOnNew {
		t.Error("FailOnNew = false, want true")
	}
	if opts.baseline != "base.json" {
		t.Errorf("baseline = %q, want base.json", opts.baseline)
	}
	if opts.sarifPath != "out.sarif" || opts.junitPath != "out.xml" || opts.ocsfPath != "out.ocsf.json" {
		t.Errorf("report paths = %q/%q/%q, want out.sarif/out.xml/out.ocsf.json", opts.sarifPath, opts.junitPath, opts.ocsfPath)
	}
}

func TestGateOptions_WriteReports(t *testing.T) {
	dir := t.TempDir()
	sarifPath := filepath.Join(dir, "out.sarif")
	junitPath := filepath.Join(dir, "out.xml")
	ocsfPath := filepath.Join(dir, "out.ocsf.json")

	opts := gateOptions{sarifPath: sarifPath, junitPath: junitPath, ocsfPath: ocsfPath}
	if err := opts.writeReports(failedScan(50), "tagctl.yaml"); err != nil {
		t.Fatalf("writeReports() error = %v", err)
	}

	sarifData, err := os.ReadFile(sarifPath) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatalf("reading SARIF: %v", err)
	}
	var sarif map[string]any
	if unmarshalErr := json.Unmarshal(sarifData, &sarif); unmarshalErr != nil {
		t.Errorf("SARIF is not valid JSON: %v", unmarshalErr)
	}
	if sarif["version"] != "2.1.0" {
		t.Errorf("SARIF version = %v, want 2.1.0", sarif["version"])
	}

	junitData, err := os.ReadFile(junitPath) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatalf("reading JUnit: %v", err)
	}
	var suites struct {
		Failures int `xml:"failures,attr"`
	}
	if err = xml.Unmarshal(junitData, &suites); err != nil {
		t.Errorf("JUnit is not valid XML: %v", err)
	}
	if suites.Failures != 1 {
		t.Errorf("JUnit failures = %d, want 1", suites.Failures)
	}

	ocsfData, err := os.ReadFile(ocsfPath) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatalf("reading OCSF: %v", err)
	}
	var events []struct {
		ClassUID int `json:"class_uid"`
	}
	if err := json.Unmarshal(ocsfData, &events); err != nil {
		t.Errorf("OCSF is not a JSON array: %v", err)
	}
	if len(events) == 0 || events[0].ClassUID != 2003 {
		t.Errorf("OCSF events = %+v, want Compliance Finding (2003) events", events)
	}
}

// Asking for no reports must not create any file.
func TestGateOptions_WriteReports_NoneRequested(t *testing.T) {
	dir := t.TempDir()

	if err := (gateOptions{}).writeReports(failedScan(50), "tagctl.yaml"); err != nil {
		t.Fatalf("writeReports() error = %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("created %d files when no report was requested", len(entries))
	}
}

func TestGateOptions_Evaluate_FailUnder(t *testing.T) {
	opts := gateOptions{gate: readGateFlags(commandWithGateFlags(t, "--fail-under", "80")).gate}

	result, err := opts.evaluate(failedScan(62))
	if err != nil {
		t.Fatalf("evaluate() error = %v", err)
	}
	if result.Passed {
		t.Error("gate passed at 62% against a floor of 80%")
	}
}

// --fail-on-new without a baseline is a usage error, not a silent pass.
func TestGateOptions_Evaluate_FailOnNewNeedsBaseline(t *testing.T) {
	opts := gateOptions{gate: readGateFlags(commandWithGateFlags(t, "--fail-on-new")).gate}

	if _, err := opts.evaluate(failedScan(50)); err == nil {
		t.Fatal("evaluate() with --fail-on-new and no baseline returned nil error")
	}
}

func TestGateOptions_Evaluate_FailOnNewAgainstBaseline(t *testing.T) {
	dir := t.TempDir()

	// The baseline has i-1 passing; the current scan has it failing.
	baseline := &types.ScanResult{
		CompliancePct: 100,
		Findings: []types.Finding{
			{
				Resource: types.Resource{ID: "i-1", Type: "aws_instance", Account: "111", Provider: "aws"},
				Tag:      "owner",
				Status:   types.StatusPass,
				Reason:   types.ReasonCompliant,
			},
		},
	}
	baselinePath := writeScan(t, dir, "baseline.json", baseline)

	opts := gateOptions{
		gate:     readGateFlags(commandWithGateFlags(t, "--fail-on-new")).gate,
		baseline: baselinePath,
	}

	result, err := opts.evaluate(failedScan(0))
	if err != nil {
		t.Fatalf("evaluate() error = %v", err)
	}
	if result.Passed {
		t.Error("gate passed despite i-1 regressing from PASS to FAILED")
	}

	// The same scan compared against itself has no regression.
	sameOpts := opts
	sameOpts.baseline = writeScan(t, dir, "same.json", failedScan(0))
	sameResult, err := sameOpts.evaluate(failedScan(0))
	if err != nil {
		t.Fatalf("evaluate() error = %v", err)
	}
	if !sameResult.Passed {
		t.Errorf("gate failed comparing a scan against itself: %v", sameResult.Reasons)
	}
}

func TestGateOptions_Evaluate_MissingBaselineFile(t *testing.T) {
	opts := gateOptions{
		gate:     readGateFlags(commandWithGateFlags(t, "--fail-on-new")).gate,
		baseline: filepath.Join(t.TempDir(), "does-not-exist.json"),
	}

	if _, err := opts.evaluate(failedScan(50)); err == nil {
		t.Fatal("evaluate() with an unreadable baseline returned nil error")
	}
}

func TestWriteToPathOrStdout_File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")

	err := writeToPathOrStdout(path, func(f *os.File) error {
		_, writeErr := f.WriteString("written")
		return writeErr
	})
	if err != nil {
		t.Fatalf("writeToPathOrStdout() error = %v", err)
	}

	data, err := os.ReadFile(path) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "written" {
		t.Errorf("file contains %q, want %q", data, "written")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); runtime.GOOS != "windows" && mode != 0o600 {
		t.Errorf("report mode = %o, want 600: reports carry every tag value and ARN", mode)
	}
}

func TestArtifactURI(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "no config file", path: "", want: ""},
		{name: "relative path", path: filepath.Join(".", "config", "prod.yaml"), want: "config/prod.yaml"},
		{name: "absolute path under the working directory", path: filepath.Join(wd, "config", "prod.yaml"), want: "config/prod.yaml"},
		{name: "absolute path above the working directory", path: filepath.Join(filepath.Dir(wd), "tagctl.yaml"), want: "../tagctl.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := artifactURI(tt.path); got != tt.want {
				t.Errorf("artifactURI(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestWriteToPathOrStdout_UnwritablePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "out.txt")

	err := writeToPathOrStdout(path, func(f *os.File) error { return nil })
	if err == nil {
		t.Error("writeToPathOrStdout() into a missing directory returned nil error")
	}
}

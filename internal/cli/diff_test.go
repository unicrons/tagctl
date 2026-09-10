package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/unicrons/tagctl/internal/types"
)

// Scan file names reused across these tests.
const (
	scanJan = "scan-20260101-100000.json"
	scanFeb = "scan-20260201-100000.json"
	scanMar = "scan-20260301-100000.json"
)

// writeScan writes a scan file into dir and returns its path.
func writeScan(t *testing.T, dir, name string, result *types.ScanResult) string {
	t.Helper()

	path := filepath.Join(dir, name)
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal scan: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write scan: %v", err)
	}
	return path
}

func TestLoadScanFile(t *testing.T) {
	dir := t.TempDir()
	want := &types.ScanResult{
		ScannedAt:      time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC),
		TotalResources: 12,
		CompliancePct:  75.5,
		Findings: []types.Finding{
			{
				Resource: types.Resource{ID: "i-1", Provider: "aws", Account: "111"},
				Tag:      "owner",
				Status:   types.StatusFailed,
				Reason:   types.ReasonMissing,
			},
		},
	}
	path := writeScan(t, dir, "scan-20260203-100000.json", want)

	got, err := LoadScanFile(path)
	if err != nil {
		t.Fatalf("LoadScanFile() error = %v", err)
	}
	if got.TotalResources != 12 {
		t.Errorf("TotalResources = %d, want 12", got.TotalResources)
	}
	if got.CompliancePct != 75.5 {
		t.Errorf("CompliancePct = %v, want 75.5", got.CompliancePct)
	}
	if len(got.Findings) != 1 || got.Findings[0].Tag != "owner" {
		t.Errorf("Findings = %+v, want one owner finding", got.Findings)
	}
}

func TestLoadScanFile_Errors(t *testing.T) {
	dir := t.TempDir()

	if _, err := LoadScanFile(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("LoadScanFile() on a missing file returned nil error")
	}

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadScanFile(bad); err == nil {
		t.Error("LoadScanFile() on malformed JSON returned nil error")
	}
}

func TestIsScanFileName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"scan-20260203-100000.json", true},
		{"scan-.json", true},
		{"plan-20260203-100000.json", false},
		{"scan-20260203-100000.csv", false},
		{"scan-20260203-100000.html", false},
		{"notascan.json", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isScanFileName(tt.name); got != tt.want {
				t.Errorf("isScanFileName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// findRecentScans must return the newest first and ignore non-scan files.
func TestFindRecentScans(t *testing.T) {
	dir := t.TempDir()

	original := OutputDir
	OutputDir = dir
	defer func() { OutputDir = original }()

	empty := &types.ScanResult{}
	oldest := writeScan(t, dir, scanJan, empty)
	middle := writeScan(t, dir, scanFeb, empty)
	newest := writeScan(t, dir, scanMar, empty)
	writeScan(t, dir, "plan-20260301-100000.json", empty) // must be ignored

	// Modification times decide the order, so set them explicitly.
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, path := range []string{oldest, middle, newest} {
		stamp := base.AddDate(0, i, 0)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}

	got, err := findRecentScans(2)
	if err != nil {
		t.Fatalf("findRecentScans() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d scans, want 2", len(got))
	}
	if filepath.Base(got[0]) != scanMar {
		t.Errorf("first = %s, want the newest scan", filepath.Base(got[0]))
	}
	if filepath.Base(got[1]) != scanFeb {
		t.Errorf("second = %s, want the second newest scan", filepath.Base(got[1]))
	}
}

func TestResolveDiffInputs(t *testing.T) {
	dir := t.TempDir()

	original := OutputDir
	OutputDir = dir
	defer func() { OutputDir = original }()

	empty := &types.ScanResult{}
	older := writeScan(t, dir, scanJan, empty)
	newer := writeScan(t, dir, scanFeb, empty)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(older, base, base); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	later := base.AddDate(0, 1, 0)
	if err := os.Chtimes(newer, later, later); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	t.Run("two explicit arguments are used as given", func(t *testing.T) {
		baseline, current, err := resolveDiffInputs([]string{"a.json", "b.json"})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if baseline != "a.json" || current != "b.json" {
			t.Errorf("got %s/%s, want a.json/b.json", baseline, current)
		}
	})

	t.Run("one argument is the baseline against the latest scan", func(t *testing.T) {
		baseline, current, err := resolveDiffInputs([]string{"a.json"})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if baseline != "a.json" {
			t.Errorf("baseline = %s, want a.json", baseline)
		}
		if filepath.Base(current) != scanFeb {
			t.Errorf("current = %s, want the newest scan", filepath.Base(current))
		}
	})

	t.Run("no arguments compares the two most recent, oldest as baseline", func(t *testing.T) {
		baseline, current, err := resolveDiffInputs(nil)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if filepath.Base(baseline) != scanJan {
			t.Errorf("baseline = %s, want the older scan", filepath.Base(baseline))
		}
		if filepath.Base(current) != scanFeb {
			t.Errorf("current = %s, want the newer scan", filepath.Base(current))
		}
	})
}

// With only one scan on disk there is nothing to compare against.
func TestResolveDiffInputs_NeedsTwoScans(t *testing.T) {
	dir := t.TempDir()

	original := OutputDir
	OutputDir = dir
	defer func() { OutputDir = original }()

	writeScan(t, dir, scanJan, &types.ScanResult{})

	if _, _, err := resolveDiffInputs(nil); err == nil {
		t.Fatal("resolveDiffInputs() with a single scan returned nil error")
	}
}

func TestSignedPct(t *testing.T) {
	tests := []struct {
		delta float64
		want  string
	}{
		{5.25, "+5.2%"},
		{-12.5, "-12.5%"},
		{0, "no change"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := signedPct(tt.delta); got != tt.want {
				t.Errorf("signedPct(%v) = %q, want %q", tt.delta, got, tt.want)
			}
		})
	}
}

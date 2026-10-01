package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/unicrons/tagctl/internal/types"
)

func TestOutputDirConstant(t *testing.T) {
	if OutputDir != "output" {
		t.Errorf("OutputDir should be 'output', got %q", OutputDir)
	}
}

func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, name string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureOutputDir(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	// Test creating output directory
	err := EnsureOutputDir()
	if err != nil {
		t.Fatalf("EnsureOutputDir() error = %v", err)
	}

	// Verify directory exists
	info, err := os.Stat(OutputDir)
	if err != nil {
		t.Fatalf("Output directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("OutputDir is not a directory")
	}

	// Test calling again (should not error)
	err = EnsureOutputDir()
	if err != nil {
		t.Errorf("EnsureOutputDir() second call error = %v", err)
	}
}

func TestGetPlanPath_EmptyFilename(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	path, err := GetPlanPath("")
	if err != nil {
		t.Fatalf("GetPlanPath('') error = %v", err)
	}

	// Should be in output directory
	dir := filepath.Dir(path)
	if dir != OutputDir {
		t.Errorf("Plan should be in %q directory, got %q", OutputDir, dir)
	}

	// Should have plan- prefix
	base := filepath.Base(path)
	if len(base) < 5 || base[:5] != "plan-" {
		t.Errorf("Plan filename should start with 'plan-', got %q", base)
	}

	// Should have .json suffix
	if filepath.Ext(path) != ".json" {
		t.Errorf("Plan should have .json extension, got %q", filepath.Ext(path))
	}

	// Verify output directory was created
	if _, err := os.Stat(OutputDir); err != nil {
		t.Errorf("Output directory should exist: %v", err)
	}
}

func TestGetPlanPath_SimpleFilename(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	path, err := GetPlanPath("my-plan.json")
	if err != nil {
		t.Fatalf("GetPlanPath('my-plan.json') error = %v", err)
	}

	expected := filepath.Join(OutputDir, "my-plan.json")
	if path != expected {
		t.Errorf("GetPlanPath('my-plan.json') = %q, want %q", path, expected)
	}
}

func TestGetPlanPath_AbsolutePath(t *testing.T) {
	tempDir := t.TempDir()
	absPath := filepath.Join(tempDir, "custom-plan.json")

	path, err := GetPlanPath(absPath)
	if err != nil {
		t.Fatalf("GetPlanPath(absolute) error = %v", err)
	}

	if path != absPath {
		t.Errorf("GetPlanPath(absolute) = %q, want %q", path, absPath)
	}
}

func TestGetPlanPath_RelativePathWithDir(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	// Create the custom directory
	mustMkdirAll(t, "custom/dir")

	path, err := GetPlanPath("custom/dir/plan.json")
	if err != nil {
		t.Fatalf("GetPlanPath('custom/dir/plan.json') error = %v", err)
	}

	// Should use the path as-is since it contains a separator
	if path != "custom/dir/plan.json" {
		t.Errorf("GetPlanPath('custom/dir/plan.json') = %q, want 'custom/dir/plan.json'", path)
	}
}

func TestFindLatestPlanInDir_NoDirectory(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	_, err := FindLatestPlanInDir()
	if err == nil {
		t.Error("FindLatestPlanInDir() should error when no directory exists")
	}
}

func TestFindLatestPlanInDir_EmptyDirectory(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	mustMkdirAll(t, OutputDir)

	_, err := FindLatestPlanInDir()
	if err == nil {
		t.Error("FindLatestPlanInDir() should error when no plan files exist")
	}
}

func TestFindLatestPlanInDir_SinglePlan(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	mustMkdirAll(t, OutputDir)

	planFile := filepath.Join(OutputDir, "plan-20240101-120000.json")
	mustWriteFile(t, planFile)

	found, err := FindLatestPlanInDir()
	if err != nil {
		t.Fatalf("FindLatestPlanInDir() error = %v", err)
	}

	if found != planFile {
		t.Errorf("FindLatestPlanInDir() = %q, want %q", found, planFile)
	}
}

func TestFindLatestPlanInDir_MultiplePlans(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	mustMkdirAll(t, OutputDir)

	// Create older plan
	oldPlan := filepath.Join(OutputDir, "plan-20240101-100000.json")
	mustWriteFile(t, oldPlan)

	// Sleep to ensure different modification times
	time.Sleep(10 * time.Millisecond)

	// Create newer plan
	newPlan := filepath.Join(OutputDir, "plan-20240101-120000.json")
	mustWriteFile(t, newPlan)

	found, err := FindLatestPlanInDir()
	if err != nil {
		t.Fatalf("FindLatestPlanInDir() error = %v", err)
	}

	if found != newPlan {
		t.Errorf("FindLatestPlanInDir() = %q, want %q (newest)", found, newPlan)
	}
}

func TestFindLatestPlanInDir_IgnoresNonPlanFiles(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	mustMkdirAll(t, OutputDir)

	// Create non-plan files
	mustWriteFile(t, filepath.Join(OutputDir, "config.json"))
	mustWriteFile(t, filepath.Join(OutputDir, "report.json"))
	mustWriteFile(t, filepath.Join(OutputDir, "plan.txt")) // Wrong extension

	// Create valid plan file
	planFile := filepath.Join(OutputDir, "plan-20240101-120000.json")
	mustWriteFile(t, planFile)

	found, err := FindLatestPlanInDir()
	if err != nil {
		t.Fatalf("FindLatestPlanInDir() error = %v", err)
	}

	if found != planFile {
		t.Errorf("FindLatestPlanInDir() = %q, want %q", found, planFile)
	}
}

func TestFindLatestPlanInDir_IgnoresDirectories(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	mustMkdirAll(t, OutputDir)

	// Create a directory that looks like a plan file
	mustMkdirAll(t, filepath.Join(OutputDir, "plan-20240101-130000.json"))

	// Create actual plan file
	planFile := filepath.Join(OutputDir, "plan-20240101-120000.json")
	mustWriteFile(t, planFile)

	found, err := FindLatestPlanInDir()
	if err != nil {
		t.Fatalf("FindLatestPlanInDir() error = %v", err)
	}

	if found != planFile {
		t.Errorf("FindLatestPlanInDir() = %q, want %q", found, planFile)
	}
}

// Integration test: verify plan is saved to output directory
func TestPlanSavedToOutputDir(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	// Get a plan path
	planPath, err := GetPlanPath("")
	if err != nil {
		t.Fatalf("GetPlanPath() error = %v", err)
	}

	// Create a mock plan
	plan := &types.Plan{
		ID:        "test-plan",
		CreatedAt: time.Now(),
		Changes:   []types.TagChange{},
		Summary: types.PlanSummary{
			TotalResources: 0,
			TotalChanges:   0,
		},
	}

	// Serialize and save
	data, _ := json.MarshalIndent(plan, "", "  ")
	if writeErr := os.WriteFile(planPath, data, 0o600); writeErr != nil {
		t.Fatalf("Failed to write plan: %v", writeErr)
	}

	// Verify it's in the output directory
	dir := filepath.Dir(planPath)
	if dir != OutputDir {
		t.Errorf("Plan saved to %q, expected %q", dir, OutputDir)
	}

	// Verify FindLatestPlanInDir can find it
	found, err := FindLatestPlanInDir()
	if err != nil {
		t.Fatalf("FindLatestPlanInDir() error = %v", err)
	}
	if found != planPath {
		t.Errorf("FindLatestPlanInDir() = %q, want %q", found, planPath)
	}
}

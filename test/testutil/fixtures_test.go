package testutil

import (
	"os"
	"testing"
)

func TestConfigPath(t *testing.T) {
	path := ConfigPath("valid.yaml")

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Errorf("ConfigPath('valid.yaml') returned non-existent path: %s", path)
	}
}

func TestFixturePath(t *testing.T) {
	path := FixturePath("plan-with-changes.json")

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Errorf("FixturePath('plan-with-changes.json') returned non-existent path: %s", path)
	}
}

func TestLoadPlan(t *testing.T) {
	tests := []struct {
		name          string
		fixture       string
		wantErr       bool
		wantChanges   int
		wantResources int
	}{
		{
			name:          "empty plan",
			fixture:       "plan-empty.json",
			wantErr:       false,
			wantChanges:   0,
			wantResources: 0,
		},
		{
			name:          "plan with changes",
			fixture:       "plan-with-changes.json",
			wantErr:       false,
			wantChanges:   5,
			wantResources: 3,
		},
		{
			name:    "non-existent file",
			fixture: "does-not-exist.json",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := LoadPlan(tt.fixture)

			if tt.wantErr {
				if err == nil {
					t.Error("LoadPlan() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("LoadPlan() unexpected error: %v", err)
			}

			if len(plan.Changes) != tt.wantChanges {
				t.Errorf("LoadPlan() got %d changes, want %d", len(plan.Changes), tt.wantChanges)
			}

			if plan.Summary.TotalResources != tt.wantResources {
				t.Errorf("LoadPlan() got %d resources, want %d", plan.Summary.TotalResources, tt.wantResources)
			}
		})
	}
}

func TestLoadResources(t *testing.T) {
	tests := []struct {
		name      string
		fixture   string
		wantErr   bool
		wantCount int
	}{
		{
			name:      "AWS resources",
			fixture:   "resources-aws.json",
			wantErr:   false,
			wantCount: 7,
		},
		{
			name:      "K8s resources",
			fixture:   "resources-k8s.json",
			wantErr:   false,
			wantCount: 6,
		},
		{
			name:    "non-existent file",
			fixture: "does-not-exist.json",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resources, err := LoadResources(tt.fixture)

			if tt.wantErr {
				if err == nil {
					t.Error("LoadResources() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("LoadResources() unexpected error: %v", err)
			}

			if len(resources) != tt.wantCount {
				t.Errorf("LoadResources() got %d resources, want %d", len(resources), tt.wantCount)
			}
		})
	}
}

func TestLoadViolations(t *testing.T) {
	violations, err := LoadViolations("violations.json")
	if err != nil {
		t.Fatalf("LoadViolations() error: %v", err)
	}

	if len(violations) != 7 {
		t.Errorf("LoadViolations() got %d violations, want 7", len(violations))
	}

	// Verify first violation has expected fields
	if violations[0].Tag != "environment" {
		t.Errorf("First violation tag = %q, want %q", violations[0].Tag, "environment")
	}
}

func TestLoadScanResult(t *testing.T) {
	result, err := LoadScanResult("scan-result.json")
	if err != nil {
		t.Fatalf("LoadScanResult() error: %v", err)
	}

	if result.TotalResources != 10 {
		t.Errorf("ScanResult.TotalResources = %d, want 10", result.TotalResources)
	}

	if result.CompliantCount != 6 {
		t.Errorf("ScanResult.CompliantCount = %d, want 6", result.CompliantCount)
	}

	if result.ViolationCount != 4 {
		t.Errorf("ScanResult.ViolationCount = %d, want 4", result.ViolationCount)
	}
}

func TestMustLoadPlan(t *testing.T) {
	// Should not panic
	plan := MustLoadPlan("plan-with-changes.json")
	if plan == nil {
		t.Error("MustLoadPlan() returned nil")
	}
}

func TestMustLoadResources(t *testing.T) {
	// Should not panic
	resources := MustLoadResources("resources-aws.json")
	if len(resources) == 0 {
		t.Error("MustLoadResources() returned empty slice")
	}
}

func TestMustLoadPlan_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustLoadPlan() should panic on non-existent file")
		}
	}()

	MustLoadPlan("non-existent.json")
}

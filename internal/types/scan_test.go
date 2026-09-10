package types

import (
	"testing"
)

func TestNewScanResult(t *testing.T) {
	result := NewScanResult()

	if result == nil {
		t.Fatal("NewScanResult() returned nil")
	}

	if result.ByAccount == nil {
		t.Error("ByAccount should be initialized")
	}

	if result.ByTag == nil {
		t.Error("ByTag should be initialized")
	}

	if result.ScannedAt.IsZero() {
		t.Error("ScannedAt should be set")
	}
}

func TestScanResult_CalculateCompliance(t *testing.T) {
	tests := []struct {
		name           string
		result         *ScanResult
		wantCompliance float64
	}{
		{
			name: "100% compliance",
			result: &ScanResult{
				TotalResources: 100,
				CompliantCount: 100,
				ByAccount:      make(map[string]*AccountStats),
				ByTag:          make(map[string]*TagStats),
			},
			wantCompliance: 100.0,
		},
		{
			name: "50% compliance",
			result: &ScanResult{
				TotalResources: 100,
				CompliantCount: 50,
				ByAccount:      make(map[string]*AccountStats),
				ByTag:          make(map[string]*TagStats),
			},
			wantCompliance: 50.0,
		},
		{
			name: "0% compliance",
			result: &ScanResult{
				TotalResources: 100,
				CompliantCount: 0,
				ByAccount:      make(map[string]*AccountStats),
				ByTag:          make(map[string]*TagStats),
			},
			wantCompliance: 0.0,
		},
		{
			name: "no resources",
			result: &ScanResult{
				TotalResources: 0,
				CompliantCount: 0,
				ByAccount:      make(map[string]*AccountStats),
				ByTag:          make(map[string]*TagStats),
			},
			wantCompliance: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.result.CalculateCompliance()
			if tt.result.CompliancePct != tt.wantCompliance {
				t.Errorf("CompliancePct = %v, want %v", tt.result.CompliancePct, tt.wantCompliance)
			}
		})
	}
}

func TestScanResult_CalculateCompliance_ByAccount(t *testing.T) {
	result := &ScanResult{
		TotalResources: 150,
		CompliantCount: 100,
		ByAccount: map[string]*AccountStats{
			"prod": {
				Account:   "prod",
				Provider:  "aws",
				Total:     100,
				Compliant: 90,
			},
			"staging": {
				Account:   "staging",
				Provider:  "aws",
				Total:     50,
				Compliant: 10,
			},
		},
		ByTag: make(map[string]*TagStats),
	}

	result.CalculateCompliance()

	if result.ByAccount["prod"].CompliancePct != 90.0 {
		t.Errorf("prod CompliancePct = %v, want 90.0", result.ByAccount["prod"].CompliancePct)
	}

	if result.ByAccount["staging"].CompliancePct != 20.0 {
		t.Errorf("staging CompliancePct = %v, want 20.0", result.ByAccount["staging"].CompliancePct)
	}
}

func TestScanResult_CalculateCompliance_ByTag(t *testing.T) {
	result := &ScanResult{
		TotalResources: 100,
		CompliantCount: 80,
		ByAccount:      make(map[string]*AccountStats),
		ByTag: map[string]*TagStats{
			"environment": {
				Tag:      "environment",
				Required: true,
				Present:  90,
				Missing:  10,
				Invalid:  5,
			},
			"owner": {
				Tag:      "owner",
				Required: true,
				Present:  50,
				Missing:  50,
				Invalid:  0,
			},
		},
	}

	result.CalculateCompliance()

	// environment: (90 - 5) / (90 + 10) = 85%
	if result.ByTag["environment"].CompliancePct != 85.0 {
		t.Errorf("environment CompliancePct = %v, want 85.0", result.ByTag["environment"].CompliancePct)
	}

	// owner: (50 - 0) / (50 + 50) = 50%
	if result.ByTag["owner"].CompliancePct != 50.0 {
		t.Errorf("owner CompliancePct = %v, want 50.0", result.ByTag["owner"].CompliancePct)
	}
}

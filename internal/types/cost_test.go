package types

import (
	"testing"
	"time"
)

func TestCostReport_Days(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	report := &CostReport{Start: start, End: start.AddDate(0, 1, 0)}
	if got := report.Days(); got != 31 {
		t.Errorf("Days() = %d, want 31 for January", got)
	}

	weekly := &CostReport{Start: start, End: start.AddDate(0, 0, 7)}
	if got := weekly.Days(); got != 7 {
		t.Errorf("Days() = %d, want 7", got)
	}
}

func TestTagCost_Coverage(t *testing.T) {
	tag := &TagCost{Tag: "owner", Attributed: 7500, Unattributed: 2500}

	if got := tag.Total(); got != 10000 {
		t.Errorf("Total() = %v, want 10000", got)
	}
	if got := tag.CoveragePct(); got != 75 {
		t.Errorf("CoveragePct() = %v, want 75", got)
	}
}

// No spend must not divide by zero.
func TestTagCost_ZeroSpend(t *testing.T) {
	tag := &TagCost{Tag: "owner"}

	if got := tag.CoveragePct(); got != 0 {
		t.Errorf("CoveragePct() = %v for zero spend, want 0", got)
	}
}

func TestCostReport_WorstCoverage(t *testing.T) {
	report := &CostReport{
		Tags: map[string]*TagCost{
			"owner":       {Tag: "owner", Attributed: 9000, Unattributed: 1000},
			"cost-center": {Tag: "cost-center", Attributed: 2000, Unattributed: 8000},
			"environment": {Tag: "environment", Attributed: 5000, Unattributed: 5000},
		},
	}

	worst := report.WorstCoverage()
	if worst == nil {
		t.Fatal("WorstCoverage() = nil")
	}
	if worst.Tag != "cost-center" {
		t.Errorf("WorstCoverage() = %q, want cost-center", worst.Tag)
	}
}

// Ties break by name, so the answer does not depend on map iteration order.
func TestCostReport_WorstCoverageIsStable(t *testing.T) {
	report := &CostReport{
		Tags: map[string]*TagCost{
			"zebra": {Tag: "zebra", Attributed: 5000, Unattributed: 5000},
			"alpha": {Tag: "alpha", Attributed: 5000, Unattributed: 5000},
			"mango": {Tag: "mango", Attributed: 5000, Unattributed: 5000},
		},
	}

	for i := 0; i < 10; i++ {
		if got := report.WorstCoverage().Tag; got != "alpha" {
			t.Fatalf("run %d: WorstCoverage() = %q, want alpha every time", i, got)
		}
	}
}

func TestCostReport_WorstCoverageEmpty(t *testing.T) {
	if got := (&CostReport{Tags: map[string]*TagCost{}}).WorstCoverage(); got != nil {
		t.Errorf("WorstCoverage() = %+v for an empty report, want nil", got)
	}
}

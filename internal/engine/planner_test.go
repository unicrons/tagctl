package engine

import (
	"context"
	"testing"

	"github.com/unicrons/tagctl/internal/types"
)

func TestMockPlanner_Plan(t *testing.T) {
	planner := NewMockPlanner()
	scanResult := types.NewScanResult()

	plan, err := planner.Plan(context.Background(), scanResult)

	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	if plan == nil {
		t.Fatal("Plan() returned nil")
	}

	if plan.IsEmpty() {
		t.Error("Plan should not be empty")
	}

	if plan.ID == "" {
		t.Error("Plan ID should not be empty")
	}

	if plan.Summary.TotalChanges != len(plan.Changes) {
		t.Errorf("Summary.TotalChanges = %d, want %d", plan.Summary.TotalChanges, len(plan.Changes))
	}
}

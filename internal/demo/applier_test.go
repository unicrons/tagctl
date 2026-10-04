package demo

import (
	"context"
	"testing"

	"github.com/unicrons/tagctl/internal/engine"
	"github.com/unicrons/tagctl/internal/types"
)

var _ engine.Applier = Applier{}

func TestApplier_ReportsEveryChangeAsApplied(t *testing.T) {
	plan := &types.Plan{Changes: []types.TagChange{
		{Resource: types.Resource{ID: "i-123", Type: "aws_instance"}, Tag: "environment", Action: types.ActionAdd, NewValue: "prod"},
		{Resource: types.Resource{ID: "i-123", Type: "aws_instance"}, Tag: "Env", Action: types.ActionRemove, OldValue: "prod"},
	}}

	result, err := Applier{}.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if result.TotalChanges != 2 || result.SuccessCount != 2 || result.ErrorCount != 0 || len(result.Errors) != 0 {
		t.Errorf("result = %+v, want 2 changes, all applied", result)
	}
	if result.Duration < 2*changeDelay {
		t.Errorf("Duration = %s, want at least %s", result.Duration, 2*changeDelay)
	}
}

func TestApplier_EmptyPlan(t *testing.T) {
	result, err := Applier{}.Apply(context.Background(), &types.Plan{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.TotalChanges != 0 || result.SuccessCount != 0 || result.ErrorCount != 0 {
		t.Errorf("result = %+v, want no changes", result)
	}
}

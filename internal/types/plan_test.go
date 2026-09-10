package types

import (
	"testing"
	"time"
)

func TestPlan_IsEmpty(t *testing.T) {
	tests := []struct {
		name string
		plan Plan
		want bool
	}{
		{
			name: "empty plan",
			plan: Plan{
				Changes: []TagChange{},
			},
			want: true,
		},
		{
			name: "nil changes",
			plan: Plan{
				Changes: nil,
			},
			want: true,
		},
		{
			name: "plan with changes",
			plan: Plan{
				Changes: []TagChange{
					{Tag: "environment", Action: ActionAdd},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.plan.IsEmpty(); got != tt.want {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChangeAction_Constants(t *testing.T) {
	tests := []struct {
		action ChangeAction
		want   string
	}{
		{ActionAdd, "add"},
		{ActionUpdate, "update"},
		{ActionRemove, "remove"},
	}

	for _, tt := range tests {
		t.Run(string(tt.action), func(t *testing.T) {
			if string(tt.action) != tt.want {
				t.Errorf("ChangeAction = %v, want %v", tt.action, tt.want)
			}
		})
	}
}

func TestChangeReason_Constants(t *testing.T) {
	tests := []struct {
		reason ChangeReason
		want   string
	}{
		{ReasonInferred, "inferred"},
		{ReasonInherited, "inherited"},
		{ReasonDefault, "default"},
		{ReasonManual, "manual"},
	}

	for _, tt := range tests {
		t.Run(string(tt.reason), func(t *testing.T) {
			if string(tt.reason) != tt.want {
				t.Errorf("ChangeReason = %v, want %v", tt.reason, tt.want)
			}
		})
	}
}

func TestPlan_Serialization(t *testing.T) {
	// Test that Plan can be created with all fields
	plan := Plan{
		ID:        "test-plan-123",
		CreatedAt: time.Now(),
		Changes: []TagChange{
			{
				Resource: Resource{
					ID:       "i-123",
					Type:     "aws_instance",
					Provider: "aws",
				},
				Tag:      "environment",
				Action:   ActionAdd,
				NewValue: "prod",
				Reason:   ReasonInferred,
				Source:   "name pattern",
			},
		},
		Summary: PlanSummary{
			TotalResources: 1,
			TotalChanges:   1,
			TagsAdded:      1,
			TagsUpdated:    0,
			TagsRemoved:    0,
		},
	}

	if plan.IsEmpty() {
		t.Error("Plan should not be empty")
	}

	if plan.Summary.TotalChanges != 1 {
		t.Errorf("TotalChanges = %d, want 1", plan.Summary.TotalChanges)
	}
}

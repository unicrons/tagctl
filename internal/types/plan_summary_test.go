package types

import "testing"

func TestPlanSummarize_IgnoresStoredSummary(t *testing.T) {
	r := Resource{ID: "i-1", Provider: "aws", Account: "1", Region: "us-east-1"}
	p := Plan{
		Summary: PlanSummary{TotalResources: 0, TotalChanges: 0},
		Changes: []TagChange{
			{Resource: r, Tag: "a", Action: ActionAdd},
			{Resource: r, Tag: "b", Action: ActionUpdate},
			{Resource: Resource{ID: "i-2", Provider: "aws", Account: "1", Region: "us-east-1"}, Tag: "c", Action: ActionRemove},
		},
	}
	got := p.Summarize()
	want := PlanSummary{TotalResources: 2, TotalChanges: 3, TagsAdded: 1, TagsUpdated: 1, TagsRemoved: 1}
	if got != want {
		t.Errorf("Summarize() = %+v, want %+v", got, want)
	}
}

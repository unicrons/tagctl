package engine

import (
	"fmt"
	"testing"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/types"
)

func changeKeys(plan *types.Plan) []string {
	keys := make([]string, 0, len(plan.Changes))
	for _, c := range plan.Changes {
		value := c.NewValue
		if c.Action == types.ActionRemove {
			value = c.OldValue
		}
		keys = append(keys, fmt.Sprintf("%s %s %s=%s (%s)", c.Resource.ID, c.Action, c.Tag, value, c.Reason))
	}
	return keys
}

func assertChanges(t *testing.T, plan *types.Plan, want ...string) {
	t.Helper()
	got := changeKeys(plan)
	if len(got) != len(want) {
		t.Fatalf("changes = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("change %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func renamePlanner(t *testing.T, rules config.RulesConfig) *RealPlanner {
	t.Helper()
	planner, err := NewPlanner(rules)
	if err != nil {
		t.Fatal(err)
	}
	return planner
}

func instanceWith(id string, tags map[string]string) types.Resource {
	return types.Resource{ID: id, ARN: "arn:aws:ec2:us-east-1:123456789012:instance/" + id, Type: "aws_instance", Tags: tags}
}

func TestRealPlanner_RenameMovesTheValueAndRemovesTheOldKey(t *testing.T) {
	planner := renamePlanner(t, config.RulesConfig{
		Rename:   []config.RenameRule{{From: "Env", To: "environment"}},
		Defaults: []config.DefaultRule{{Resource: "*", Set: map[string]string{"environment": "unknown"}}},
	})
	legacy := instanceWith("i-1", map[string]string{"Env": "prod"})

	plan := planFor(t, planner, missing("environment", legacy))

	assertChanges(t, plan,
		"i-1 add environment=prod (renamed)",
		"i-1 remove Env=prod (renamed)",
	)
	if plan.Changes[0].Source != "renamed from 'Env'" || plan.Changes[1].Source != "renamed to 'environment'" {
		t.Errorf("sources = %q and %q", plan.Changes[0].Source, plan.Changes[1].Source)
	}
	want := types.PlanSummary{TotalResources: 1, TotalChanges: 2, TagsAdded: 1, TagsRemoved: 1}
	if plan.Summary != want {
		t.Errorf("Summary = %+v, want %+v", plan.Summary, want)
	}
	if err := ValidatePlan(plan); err != nil {
		t.Errorf("ValidatePlan() = %v", err)
	}
}

func TestRealPlanner_RenameOnlyRemovesWhenTheTargetAlreadyHoldsTheValue(t *testing.T) {
	planner := renamePlanner(t, config.RulesConfig{Rename: []config.RenameRule{{From: "Env", To: "environment"}}})
	both := instanceWith("i-1", map[string]string{"Env": "prod", "environment": "prod"})

	plan := planFor(t, planner, compliant("environment", both))

	assertChanges(t, plan, "i-1 remove Env=prod (renamed)")
	if len(plan.Conflicts) != 0 {
		t.Errorf("Conflicts = %+v, want none", plan.Conflicts)
	}
}

func TestRealPlanner_RenameConflictIsReportedAndSkipped(t *testing.T) {
	planner := renamePlanner(t, config.RulesConfig{Rename: []config.RenameRule{{From: "Env", To: "environment"}}})
	conflicting := instanceWith("i-1", map[string]string{"Env": "prod", "environment": "dev"})
	clean := instanceWith("i-2", map[string]string{"Env": "prod"})

	plan := planFor(t, planner, compliant("environment", conflicting), missing("environment", clean))

	assertChanges(t, plan,
		"i-2 add environment=prod (renamed)",
		"i-2 remove Env=prod (renamed)",
	)
	if len(plan.Conflicts) != 1 || plan.Summary.Conflicts != 1 {
		t.Fatalf("Conflicts = %+v, summary = %+v, want one", plan.Conflicts, plan.Summary)
	}
	c := plan.Conflicts[0]
	if c.Resource.ID != "i-1" || c.From != "Env" || c.Value != "prod" || c.To != "environment" || c.ExistingValue != "dev" {
		t.Errorf("conflict = %+v", c)
	}
	if got, want := c.Message(), "cannot rename 'Env' to 'environment': 'environment' is already 'dev', not 'prod'"; got != want {
		t.Errorf("Message() = %q, want %q", got, want)
	}
}

func TestRealPlanner_TwoRenamesIntoOneKey(t *testing.T) {
	planner := renamePlanner(t, config.RulesConfig{Rename: []config.RenameRule{
		{From: "Env", To: "environment"},
		{From: "env", To: "environment"},
	}})

	agreeing := planFor(t, planner, missing("environment", instanceWith("i-1", map[string]string{"Env": "prod", "env": "prod"})))
	assertChanges(t, agreeing,
		"i-1 add environment=prod (renamed)",
		"i-1 remove Env=prod (renamed)",
		"i-1 remove env=prod (renamed)",
	)

	disagreeing := planFor(t, planner, missing("environment", instanceWith("i-1", map[string]string{"Env": "prod", "env": "dev"})))
	assertChanges(t, disagreeing,
		"i-1 add environment=prod (renamed)",
		"i-1 remove Env=prod (renamed)",
	)
	if len(disagreeing.Conflicts) != 1 || disagreeing.Conflicts[0].From != "env" || disagreeing.Conflicts[0].ExistingValue != "prod" {
		t.Errorf("Conflicts = %+v, want env against the value the first rename sets", disagreeing.Conflicts)
	}
}

func TestRealPlanner_RenameHonoursTheResourceGlob(t *testing.T) {
	planner := renamePlanner(t, config.RulesConfig{Rename: []config.RenameRule{{Resource: "aws_s3_*", From: "Env", To: "environment"}}})
	bucket := types.Resource{ID: "logs", ARN: "arn:aws:s3:::logs", Type: "aws_s3_bucket", Tags: map[string]string{"Env": "prod"}}

	plan := planFor(t, planner, missing("environment", bucket), missing("environment", instanceWith("i-1", map[string]string{"Env": "prod"})))

	assertChanges(t, plan,
		"logs add environment=prod (renamed)",
		"logs remove Env=prod (renamed)",
	)
}

func TestRealPlanner_RemovesForbiddenTags(t *testing.T) {
	planner := renamePlanner(t, config.RulesConfig{})
	resource := instanceWith("i-1", map[string]string{"temp": "1"})
	forbidden := types.Finding{Resource: resource, Tag: "temp", Status: types.StatusFailed, Reason: types.ReasonForbidden, Actual: "1"}

	plan := planFor(t, planner, forbidden)

	assertChanges(t, plan, "i-1 remove temp=1 (forbidden)")
	if plan.Changes[0].Source != "forbidden by policy" || plan.Summary.TagsRemoved != 1 || plan.Summary.TagsAdded != 0 {
		t.Errorf("change = %+v, summary = %+v", plan.Changes[0], plan.Summary)
	}
}

func TestRealPlanner_RenamedForbiddenKeyIsRemovedOnce(t *testing.T) {
	planner := renamePlanner(t, config.RulesConfig{Rename: []config.RenameRule{{From: "Env", To: "environment"}}})
	resource := instanceWith("i-1", map[string]string{"Env": "prod"})
	forbidden := types.Finding{Resource: resource, Tag: "Env", Status: types.StatusFailed, Reason: types.ReasonForbidden, Actual: "prod"}

	plan := planFor(t, planner, missing("environment", resource), forbidden)

	assertChanges(t, plan,
		"i-1 add environment=prod (renamed)",
		"i-1 remove Env=prod (renamed)",
	)
}

func TestRealPlanner_ForbiddenKeyOfARenameConflictIsKept(t *testing.T) {
	planner := renamePlanner(t, config.RulesConfig{Rename: []config.RenameRule{{From: "Env", To: "environment"}}})
	resource := instanceWith("i-1", map[string]string{"Env": "prod", "environment": "dev"})
	forbidden := types.Finding{Resource: resource, Tag: "Env", Status: types.StatusFailed, Reason: types.ReasonForbidden, Actual: "prod"}

	plan := planFor(t, planner, compliant("environment", resource), forbidden)

	if len(plan.Changes) != 0 || len(plan.Conflicts) != 1 {
		t.Errorf("changes = %q, conflicts = %d; the conflicting value must not be deleted", changeKeys(plan), len(plan.Conflicts))
	}
}

package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/unicrons/tagctl/internal/types"
)

func removal(id, tag, value string, reason types.ChangeReason) types.TagChange {
	return types.TagChange{
		Resource: types.Resource{ID: id, Provider: "aws", Type: "aws_instance", Region: "us-east-1"},
		Tag:      tag, Action: types.ActionRemove, OldValue: value, Reason: reason,
	}
}

func TestPrintPlanSummary_ListsRemovalsApart(t *testing.T) {
	plan := &types.Plan{
		CreatedAt: time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC),
		Changes: []types.TagChange{
			{Resource: types.Resource{ID: "i-1", Provider: "aws", Type: "aws_instance", Region: "us-east-1"}, Tag: "environment", Action: types.ActionAdd, NewValue: "prod"},
			removal("i-1", "Env", "prod", types.ReasonRenamed),
			removal("i-2", "temp", "1", types.ReasonForbiddenTag),
		},
	}

	var buf bytes.Buffer
	printPlanSummary(&buf, "plan.json", plan)

	for _, want := range []string{
		"  • 2 resources will be modified\n",
		"  • 1 tags will be added\n",
		"  • 2 tags will be REMOVED:\n",
		`      - Env="prod" on aws_instance i-1 (renamed)` + "\n",
		`      - temp="1" on aws_instance i-2 (forbidden)` + "\n",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("summary lacks %q:\n%s", want, buf.String())
		}
	}
}

func TestPrintPlanSummary_WithoutRemovalsSaysNothingAboutThem(t *testing.T) {
	plan := &types.Plan{Changes: []types.TagChange{
		{Resource: types.Resource{ID: "i-1"}, Tag: "environment", Action: types.ActionAdd, NewValue: "prod"},
	}}

	var buf bytes.Buffer
	printPlanSummary(&buf, "plan.json", plan)

	if strings.Contains(buf.String(), "REMOVED") {
		t.Errorf("summary mentions removals:\n%s", buf.String())
	}
}

func TestPrintPlanSummary_CapsTheListedRemovals(t *testing.T) {
	plan := &types.Plan{}
	for i := range maxListedRemovals + 5 {
		plan.Changes = append(plan.Changes, removal(fmt.Sprintf("i-%d", i), "temp", "1", types.ReasonForbiddenTag))
	}

	var buf bytes.Buffer
	printPlanSummary(&buf, "plan.json", plan)

	if got := strings.Count(buf.String(), "      - temp="); got != maxListedRemovals {
		t.Errorf("listed %d removals, want %d", got, maxListedRemovals)
	}
	if !strings.Contains(buf.String(), "... and 5 more, see plan.json") {
		t.Errorf("summary does not say how many removals were left out:\n%s", buf.String())
	}
}

func TestDescribeChange(t *testing.T) {
	if got := describeChange(removal("i-1", "Env", "prod", types.ReasonRenamed)); got != "remove Env" {
		t.Errorf("describeChange(remove) = %q", got)
	}
	add := types.TagChange{Tag: "environment", Action: types.ActionAdd, NewValue: "prod"}
	if got := describeChange(add); got != "environment: prod" {
		t.Errorf("describeChange(add) = %q", got)
	}
}

func TestPrintPlanConflicts(t *testing.T) {
	plan := &types.Plan{Conflicts: []types.RenameConflict{{
		Resource: types.Resource{ID: "i-1", Name: "web", Type: "aws_instance"},
		From:     "Env", Value: "prod", To: "environment", ExistingValue: "dev",
	}}}

	var buf bytes.Buffer
	printPlanConflicts(&buf, plan)

	want := "\nConflicts: 1 rename(s) skipped, the target tag holds another value\n" +
		"  ! aws_instance (web): cannot rename 'Env' to 'environment': 'environment' is already 'dev', not 'prod'\n"
	if buf.String() != want {
		t.Errorf("printPlanConflicts() wrote %q, want %q", buf.String(), want)
	}

	buf.Reset()
	printPlanConflicts(&buf, &types.Plan{})
	if buf.Len() != 0 {
		t.Errorf("printPlanConflicts() wrote %q for a plan without conflicts", buf.String())
	}
}

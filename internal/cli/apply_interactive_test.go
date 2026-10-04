package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/unicrons/tagctl/internal/engine"
	"github.com/unicrons/tagctl/internal/types"
)

func reviewResource(region, id string) types.Resource {
	return types.Resource{
		ID:       id,
		ARN:      "arn:aws:lambda:" + region + ":123456789012:function:" + id,
		Type:     "aws_lambda_function",
		Provider: "aws",
		Region:   region,
	}
}

func addTag(resource types.Resource, tag, value string) types.TagChange {
	return types.TagChange{Resource: resource, Tag: tag, Action: types.ActionAdd, NewValue: value, Reason: types.ReasonDefault}
}

// reviewPlan has three resources; the first two share an ID across regions
// and the first one has two changes split around the second resource.
func reviewPlan() *types.Plan {
	east, west, other := reviewResource("us-east-1", "handler"), reviewResource("us-west-2", "handler"), reviewResource("us-east-1", "worker")
	return &types.Plan{
		ID:        "plan-1",
		CreatedAt: time.Date(2026, 2, 2, 10, 0, 0, 0, time.UTC),
		Changes: []types.TagChange{
			addTag(east, "owner", "team@example.com"),
			addTag(west, "owner", "team@example.com"),
			addTag(east, "environment", "prod"),
			{Resource: other, Tag: "environment", Action: types.ActionUpdate, OldValue: "Prod", NewValue: "prod", Reason: types.ReasonInferred},
		},
	}
}

func identities(groups []resourceChanges) []string {
	ids := make([]string, len(groups))
	for i, g := range groups {
		ids[i] = g.resource.Identity()
	}
	return ids
}

func TestReviewChanges_AppliesOnlyWhatWasApproved(t *testing.T) {
	plan := reviewPlan()
	east, west, other := plan.Changes[0].Resource.Identity(), plan.Changes[1].Resource.Identity(), plan.Changes[3].Resource.Identity()

	tests := []struct {
		name         string
		input        string
		wantApproved []string
		wantSkipped  []string
		wantPrompts  int
	}{
		{name: "yes to every resource", input: "y\ny\ny\n", wantApproved: []string{east, west, other}, wantPrompts: 3},
		{name: "no to every resource", input: "n\nn\nn\n", wantSkipped: []string{east, west, other}, wantPrompts: 3},
		{name: "same ID in two regions is decided per resource", input: "y\nn\ny\n", wantApproved: []string{east, other}, wantSkipped: []string{west}, wantPrompts: 3},
		{name: "all approves this and the remaining", input: "n\na\n", wantApproved: []string{west, other}, wantSkipped: []string{east}, wantPrompts: 2},
		{name: "quit skips this and the remaining", input: "y\nq\n", wantApproved: []string{east}, wantSkipped: []string{west, other}, wantPrompts: 2},
		{name: "full words in capitals", input: " YES \nNo\nAll\n", wantApproved: []string{east, other}, wantSkipped: []string{west}, wantPrompts: 3},
		{name: "unknown answer asks again", input: "maybe\n\ny\nq\n", wantApproved: []string{east}, wantSkipped: []string{west, other}, wantPrompts: 4},
		{name: "last answer without newline", input: "n\nn\ny", wantApproved: []string{other}, wantSkipped: []string{east, west}, wantPrompts: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer

			review, err := reviewChanges(context.Background(), &out, strings.NewReader(tt.input), plan)
			if err != nil {
				t.Fatalf("reviewChanges() error = %v", err)
			}

			if got := identities(review.approved); !slices.Equal(got, tt.wantApproved) {
				t.Errorf("approved = %v, want %v", got, tt.wantApproved)
			}
			if got := identities(review.skipped); !slices.Equal(got, tt.wantSkipped) {
				t.Errorf("skipped = %v, want %v", got, tt.wantSkipped)
			}
			if got := strings.Count(out.String(), "[y]es / [n]o / [a]ll remaining / [q]uit"); got != tt.wantPrompts {
				t.Errorf("prompted %d times, want %d:\n%s", got, tt.wantPrompts, out.String())
			}
		})
	}
}

func TestReviewChanges_ShowsEveryChangeOfTheResource(t *testing.T) {
	var out bytes.Buffer

	if _, err := reviewChanges(context.Background(), &out, strings.NewReader("y\ny\ny\n"), reviewPlan()); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"[1/3] aws_lambda_function handler (arn:aws:lambda:us-east-1:123456789012:function:handler)\n" +
			"  + owner: \"team@example.com\"\n  + environment: \"prod\"\n",
		"[2/3] aws_lambda_function handler (arn:aws:lambda:us-west-2:123456789012:function:handler)\n",
		"[3/3] aws_lambda_function worker (arn:aws:lambda:us-east-1:123456789012:function:worker)\n" +
			"  ~ environment: \"Prod\" -> \"prod\"\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("review output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestReviewChanges_EndOfInputApprovesNothing(t *testing.T) {
	review, err := reviewChanges(context.Background(), io.Discard, strings.NewReader("y\n"), reviewPlan())

	if err == nil || review != nil {
		t.Fatalf("reviewChanges() = %v, %v; want an error and no review", review, err)
	}
	if !strings.Contains(err.Error(), "nothing was applied") {
		t.Errorf("error = %v, want one saying nothing was applied", err)
	}
}

func TestReviewChanges_CancelledAtPromptReturnsWithoutInput(t *testing.T) {
	stdin, keyboard := io.Pipe()
	t.Cleanup(func() { _ = keyboard.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	review, err := reviewChanges(ctx, io.Discard, stdin, reviewPlan())

	if review != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("reviewChanges() = %v, %v; want no review and a context.Canceled error", review, err)
	}
}

func TestReviewChanges_ControlCharactersAreNotPrinted(t *testing.T) {
	resource := types.Resource{ID: "i-1", Name: "web\x1b[2J", Type: "aws_instance", Provider: "aws"}
	plan := &types.Plan{Changes: []types.TagChange{
		{Resource: resource, Tag: "owner\x1b[31m", Action: types.ActionUpdate, OldValue: "old\x07", NewValue: "new\x1b]0;x\x07"},
	}}
	var out bytes.Buffer

	review, err := reviewChanges(context.Background(), &out, strings.NewReader("n\n"), plan)
	if err != nil {
		t.Fatal(err)
	}
	printReviewSummary(&out, review)

	if strings.ContainsAny(out.String(), "\x1b\x07") {
		t.Errorf("review output carries control characters: %q", out.String())
	}
	if !strings.Contains(out.String(), "web?[2J") {
		t.Errorf("review output lacks the sanitised name: %q", out.String())
	}
}

func TestApprovedPlan_KeepsPlanOrderAndPassesValidation(t *testing.T) {
	plan := reviewPlan()
	review, err := reviewChanges(context.Background(), io.Discard, strings.NewReader("y\nn\ny\n"), plan)
	if err != nil {
		t.Fatal(err)
	}

	approved := review.approvedPlan(plan)

	if err = engine.ValidatePlan(approved); err != nil {
		t.Fatalf("ValidatePlan(approved) = %v", err)
	}
	want := []types.TagChange{plan.Changes[0], plan.Changes[2], plan.Changes[3]}
	if !reflect.DeepEqual(approved.Changes, want) {
		t.Errorf("approved changes = %+v, want %+v", approved.Changes, want)
	}
	wantSummary := types.PlanSummary{TotalResources: 2, TotalChanges: 3, TagsAdded: 2, TagsUpdated: 1}
	if approved.Summary != wantSummary || approved.ID != plan.ID || !approved.CreatedAt.Equal(plan.CreatedAt) {
		t.Errorf("approved plan = %+v, want summary %+v and the original id and date", approved, wantSummary)
	}
	if review.skippedChanges() != 1 || len(plan.Changes) != 4 {
		t.Errorf("skipped %d changes and left %d in the original plan, want 1 and 4", review.skippedChanges(), len(plan.Changes))
	}
}

func writeReviewPlan(t *testing.T) string {
	t.Helper()
	data, err := json.Marshal(reviewPlan())
	if err != nil {
		t.Fatal(err)
	}
	return writeFixture(t, t.TempDir(), "plan.json", string(data))
}

func answerApplyPrompts(t *testing.T, input string, terminal bool) {
	t.Helper()
	originalInput, originalTerminal := applyInput, stdinIsTerminal
	applyInput, stdinIsTerminal = strings.NewReader(input), func() bool { return terminal }
	t.Cleanup(func() { applyInput, stdinIsTerminal = originalInput, originalTerminal })
}

func TestApplyInteractive_AppliesApprovedAndReportsSkipped(t *testing.T) {
	answerApplyPrompts(t, "y\nn\nn\n", true)

	run := execute(t, "apply", "--mock", "-i", "--plan", writeReviewPlan(t))

	if run.err != nil {
		t.Fatalf("apply -i error = %v\n%s", run.err, run.stderr)
	}
	for _, want := range []string{
		"[1/2] aws_lambda_function.handler (owner: team@example.com)",
		"[2/2] aws_lambda_function.handler (environment: prod)",
		"Applied successfully: 2 changes\n",
		"Skipped: 2 changes\n",
		"Errors: 0\n",
	} {
		if !strings.Contains(run.stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, run.stdout)
		}
	}
	if strings.Contains(run.stdout, "worker") || strings.Contains(run.stdout, "[q]uit") {
		t.Errorf("stdout carries a skipped resource or a prompt:\n%s", run.stdout)
	}
	for _, want := range []string{
		"[y]es / [n]o / [a]ll remaining / [q]uit",
		"Review: 1 resource(s) approved (2 changes), 2 skipped (2 changes)",
		"aws_lambda_function worker (arn:aws:lambda:us-east-1:123456789012:function:worker): environment",
	} {
		if !strings.Contains(run.stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, run.stderr)
		}
	}
	if strings.Contains(run.stderr, "Do you want to apply these changes?") {
		t.Errorf("apply -i also asked the yes/no confirmation:\n%s", run.stderr)
	}
}

func TestApplyInteractive_NothingApprovedAppliesNothing(t *testing.T) {
	answerApplyPrompts(t, "q\n", true)

	run := execute(t, "apply", "--mock", "--interactive", "--plan", writeReviewPlan(t))

	if run.err != nil {
		t.Fatalf("apply --interactive error = %v", run.err)
	}
	if run.stdout != "" {
		t.Errorf("stdout = %q, want nothing applied and nothing printed", run.stdout)
	}
	if !strings.Contains(run.stderr, "No changes approved, nothing applied.") {
		t.Errorf("stderr lacks the nothing-applied notice:\n%s", run.stderr)
	}
}

func TestApplyInteractive_TruncatedInputAppliesNothing(t *testing.T) {
	answerApplyPrompts(t, "y\n", true)

	run := execute(t, "apply", "--mock", "-i", "--plan", writeReviewPlan(t))

	if run.err == nil || !strings.Contains(run.err.Error(), "nothing was applied") {
		t.Fatalf("apply -i error = %v, want one saying nothing was applied", run.err)
	}
	if run.stdout != "" {
		t.Errorf("stdout = %q, want no change applied", run.stdout)
	}
}

func TestApplyInteractive_RejectsUnusableFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		terminal bool
		wantErr  string
	}{
		{name: "with --auto-approve", args: []string{"-i", "--auto-approve"}, terminal: true, wantErr: "--interactive and --auto-approve cannot be used together"},
		{name: "without a terminal", args: []string{"--interactive"}, terminal: false, wantErr: "--interactive needs a terminal on stdin; use --auto-approve"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			answerApplyPrompts(t, "a\n", tt.terminal)

			run := execute(t, append([]string{"apply", "--mock", "--plan", writeReviewPlan(t)}, tt.args...)...)

			if run.err == nil || !strings.Contains(run.err.Error(), tt.wantErr) {
				t.Fatalf("apply %v error = %v, want one containing %q", tt.args, run.err, tt.wantErr)
			}
			if code := ExitCode(run.err); code != exitError {
				t.Errorf("exit code = %d, want %d", code, exitError)
			}
			if run.stdout != "" || strings.Contains(run.stderr, "[q]uit") {
				t.Errorf("apply %v prompted or applied before rejecting the flags:\n%s%s", tt.args, run.stdout, run.stderr)
			}
		})
	}
}

func TestApply_WithoutInteractiveReportsNoSkippedLine(t *testing.T) {
	run := execute(t, "apply", "--mock", "--auto-approve", "--plan", writeReviewPlan(t))

	if run.err != nil {
		t.Fatalf("apply error = %v", run.err)
	}
	if !strings.Contains(run.stdout, "Applied successfully: 4 changes") || strings.Contains(run.stdout, "Skipped") {
		t.Errorf("stdout = %q, want 4 applied changes and no skipped line", run.stdout)
	}
}

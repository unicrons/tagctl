package engine

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// removingProvider records every call in order.
type removingProvider struct {
	mu        sync.Mutex
	calls     []string
	applyErr  error
	removeErr error
}

func (p *removingProvider) Name() string { return "aws" }
func (p *removingProvider) ListResources(context.Context) ([]types.Resource, error) {
	return nil, nil
}

func (p *removingProvider) ApplyTags(_ context.Context, id string, tags map[string]string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, "set "+id+" "+strings.Join(slices.Sorted(maps.Keys(tags)), ","))
	return p.applyErr
}

func (p *removingProvider) RemoveTags(_ context.Context, resource types.Resource, keys []string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, "remove "+resource.Identity()+" "+strings.Join(keys, ","))
	return p.removeErr
}

const renamedVolumeARN = "arn:aws:ec2:us-east-1:123456789012:volume/vol-1"

func renamePlan() *types.Plan {
	volume := types.Resource{ID: "vol-1", ARN: renamedVolumeARN, Provider: "aws", Type: "aws_ebs_volume", Region: "us-east-1"}
	return &types.Plan{Changes: []types.TagChange{
		{Resource: volume, Tag: "environment", Action: types.ActionAdd, NewValue: "prod", Reason: types.ReasonRenamed},
		{Resource: volume, Tag: "temp", Action: types.ActionRemove, OldValue: "1", Reason: types.ReasonForbiddenTag},
		{Resource: volume, Tag: "Env", Action: types.ActionRemove, OldValue: "prod", Reason: types.ReasonRenamed},
	}}
}

func failedTags(result *ApplyResult) []string {
	tags := make([]string, 0, len(result.Errors))
	for _, e := range result.Errors {
		tags = append(tags, e.Change.Tag)
	}
	slices.Sort(tags)
	return tags
}

func TestRealApplier_SetsTagsBeforeRemovingKeys(t *testing.T) {
	p := &removingProvider{}
	applier := NewApplier([]provider.Provider{p})
	reported := make(map[string]bool)
	applier.SetCallback(func(change types.TagChange, success bool, _ error) { reported[change.Tag] = success })

	result, err := applier.Apply(context.Background(), renamePlan())
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"set vol-1 environment", "remove " + renamedVolumeARN + " Env,temp"}
	if !slices.Equal(p.calls, want) {
		t.Errorf("calls = %q, want %q", p.calls, want)
	}
	if result.SuccessCount != 3 || result.ErrorCount != 0 {
		t.Errorf("result = %+v, want 3 successes", result)
	}
	if !reported["environment"] || !reported["Env"] || !reported["temp"] {
		t.Errorf("callback reported %v, want every change as a success", reported)
	}
}

func TestRealApplier_KeepsTheOldKeyWhenSettingFails(t *testing.T) {
	p := &removingProvider{applyErr: errors.New("denied")}

	result, err := NewApplier([]provider.Provider{p}).Apply(context.Background(), renamePlan())
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(p.calls, []string{"set vol-1 environment"}) {
		t.Errorf("calls = %q; nothing may be removed after a failed set", p.calls)
	}
	if result.ErrorCount != 3 || result.SuccessCount != 0 {
		t.Fatalf("result = %+v, want every change failed", result)
	}
	for _, e := range result.Errors {
		if removal := e.Change.Action == types.ActionRemove; removal != strings.HasPrefix(e.Error, "not removed:") {
			t.Errorf("%s %s failed with %q", e.Change.Action, e.Change.Tag, e.Error)
		}
	}
}

func TestRealApplier_FailedRemovalOnlyFailsTheRemovals(t *testing.T) {
	p := &removingProvider{removeErr: errors.New("AccessDenied: ec2:DeleteTags")}

	result, err := NewApplier([]provider.Provider{p}).Apply(context.Background(), renamePlan())
	if err != nil {
		t.Fatal(err)
	}

	if result.SuccessCount != 1 || !slices.Equal(failedTags(result), []string{"Env", "temp"}) {
		t.Errorf("result = %+v, want the add applied and both removals failed", result)
	}
}

func TestRealApplier_RemovalOnlyPlanSkipsApplyTags(t *testing.T) {
	p := &removingProvider{}
	plan := renamePlan()
	plan.Changes = plan.Changes[1:]

	result, err := NewApplier([]provider.Provider{p}).Apply(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(p.calls, []string{"remove " + renamedVolumeARN + " Env,temp"}) || result.SuccessCount != 2 {
		t.Errorf("calls = %q, result = %+v", p.calls, result)
	}
}

func TestRealApplier_ProviderWithoutTagRemoval(t *testing.T) {
	var sets int
	p := funcProvider(func(string) error { sets++; return nil })

	result, err := NewApplier([]provider.Provider{p}).Apply(context.Background(), renamePlan())
	if err != nil {
		t.Fatal(err)
	}

	if sets != 1 || result.SuccessCount != 1 || !slices.Equal(failedTags(result), []string{"Env", "temp"}) {
		t.Fatalf("sets = %d, result = %+v", sets, result)
	}
	if got := result.Errors[0].Error; got != "the aws provider cannot remove tags" {
		t.Errorf("error = %q", got)
	}
}

func TestValidatePlan(t *testing.T) {
	r := types.Resource{ID: "i-1", ARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-1", Provider: "aws"}
	other := types.Resource{ID: "i-2", ARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-2", Provider: "aws"}
	change := func(res types.Resource, tag string, action types.ChangeAction) types.TagChange {
		return types.TagChange{Resource: res, Tag: tag, Action: action}
	}

	cases := []struct {
		name    string
		changes []types.TagChange
		wantErr string
	}{
		{"add, update and remove of different tags", []types.TagChange{change(r, "a", types.ActionAdd), change(r, "b", types.ActionUpdate), change(r, "c", types.ActionRemove)}, ""},
		{"same tag set on one resource and removed on another", []types.TagChange{change(r, "a", types.ActionAdd), change(other, "a", types.ActionRemove)}, ""},
		{"tag set then removed", []types.TagChange{change(r, "a", types.ActionAdd), change(r, "a", types.ActionRemove)}, `tag "a" on ` + r.ARN + " is both set and removed"},
		{"tag removed then set", []types.TagChange{change(r, "a", types.ActionRemove), change(r, "a", types.ActionUpdate)}, "is both set and removed"},
		{"unknown action", []types.TagChange{change(r, "a", "rename")}, `unsupported action "rename" for tag "a"`},
		{"remove without a tag name", []types.TagChange{change(r, "", types.ActionRemove)}, "remove change without a tag name on " + r.ARN},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePlan(&types.Plan{Changes: tc.changes})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidatePlan() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidatePlan() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

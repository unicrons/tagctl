package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

func TestMockApplier_Apply(t *testing.T) {
	applier := NewMockApplier()
	plan := &types.Plan{
		ID:        "test-plan",
		CreatedAt: time.Now(),
		Changes: []types.TagChange{
			{
				Resource: types.Resource{ID: "i-123", Type: "aws_instance"},
				Tag:      "environment",
				Action:   types.ActionAdd,
				NewValue: "prod",
			},
		},
	}

	result, err := applier.Apply(context.Background(), plan)

	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if result == nil {
		t.Fatal("Apply() returned nil result")
	}

	if result.TotalChanges != 1 {
		t.Errorf("TotalChanges = %d, want 1", result.TotalChanges)
	}

	if result.SuccessCount != 1 {
		t.Errorf("SuccessCount = %d, want 1", result.SuccessCount)
	}

	if result.ErrorCount != 0 {
		t.Errorf("ErrorCount = %d, want 0", result.ErrorCount)
	}
}

func TestMockApplier_Apply_EmptyPlan(t *testing.T) {
	applier := NewMockApplier()
	plan := &types.Plan{
		ID:        "empty-plan",
		CreatedAt: time.Now(),
		Changes:   []types.TagChange{},
	}

	result, err := applier.Apply(context.Background(), plan)

	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if result.TotalChanges != 0 {
		t.Errorf("TotalChanges = %d, want 0", result.TotalChanges)
	}
}

// Changes for the same ID in two regions must reach two resources, not be
// applied twice to whichever one comes first.
func TestGroupChangesByResource_SeparatesRegions(t *testing.T) {
	changes := []types.TagChange{
		{Resource: types.Resource{ID: "/aws/lambda/fn", Provider: "aws", Account: "111", Region: "eu-west-1"}, Tag: "owner"},
		{Resource: types.Resource{ID: "/aws/lambda/fn", Provider: "aws", Account: "111", Region: "us-east-1"}, Tag: "owner"},
		{Resource: types.Resource{ID: "/aws/lambda/fn", Provider: "aws", Account: "111", Region: "us-east-1"}, Tag: "team"},
	}

	grouped := groupChangesByResource(changes)

	if len(grouped) != 2 {
		t.Fatalf("grouped into %d resources, want 2", len(grouped))
	}
	if got := len(grouped["aws/111/us-east-1//aws/lambda/fn"]); got != 2 {
		t.Errorf("us-east-1 copy has %d changes, want 2", got)
	}
}

type slowProvider struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (p *slowProvider) Name() string { return "aws" }
func (p *slowProvider) ListResources(ctx context.Context) ([]types.Resource, error) {
	return nil, nil
}
func (p *slowProvider) ApplyTags(ctx context.Context, id string, tags map[string]string) error {
	time.Sleep(50 * time.Millisecond)
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return p.err
}

func changesFor(resources int, tagsPerResource int) []types.TagChange {
	var changes []types.TagChange
	for r := 0; r < resources; r++ {
		res := types.Resource{ID: fmt.Sprintf("i-%d", r), Provider: "aws", Type: "aws_instance", Region: "us-east-1"}
		for t := 0; t < tagsPerResource; t++ {
			changes = append(changes, types.TagChange{Resource: res, Tag: fmt.Sprintf("tag%d", t), NewValue: "v", Action: types.ActionAdd})
		}
	}
	return changes
}

func TestRealApplier_WaitsForEveryResource(t *testing.T) {
	p := &slowProvider{}
	result, err := NewApplier([]provider.Provider{p}).Apply(context.Background(), &types.Plan{Changes: changesFor(3, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if p.calls != 3 || result.SuccessCount != 3 || result.ErrorCount != 0 {
		t.Errorf("calls = %d, result = %+v; Apply must not return before the providers finish", p.calls, result)
	}
}

func TestRealApplier_FailedResourceReportsEveryChange(t *testing.T) {
	p := &slowProvider{err: errors.New("denied")}
	result, err := NewApplier([]provider.Provider{p}).Apply(context.Background(), &types.Plan{Changes: changesFor(1, 3)})
	if err != nil {
		t.Fatal(err)
	}
	if p.calls != 1 || result.ErrorCount != 3 || result.SuccessCount != 0 || len(result.Errors) != 3 {
		t.Errorf("calls = %d, result = %+v", p.calls, result)
	}
}

type funcProvider func(id string) error

func (f funcProvider) Name() string { return "aws" }
func (f funcProvider) ListResources(context.Context) ([]types.Resource, error) {
	return nil, nil
}
func (f funcProvider) ApplyTags(_ context.Context, id string, _ map[string]string) error {
	return f(id)
}

func TestRealApplier_SerializesCallback(t *testing.T) {
	changes := changesFor(200, 2)
	applier := NewApplier([]provider.Provider{funcProvider(func(string) error { return nil })})
	var reported []string
	applier.SetCallback(func(change types.TagChange, _ bool, _ error) {
		reported = append(reported, change.Tag)
	})

	if _, err := applier.Apply(context.Background(), &types.Plan{Changes: changes}); err != nil {
		t.Fatal(err)
	}
	if len(reported) != len(changes) {
		t.Errorf("callback reported %d changes, want %d", len(reported), len(changes))
	}
}

func TestRealApplier_StopsDispatchingWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	applier := NewApplier([]provider.Provider{funcProvider(func(string) error {
		calls.Add(1)
		cancel()
		return nil
	})})
	applier.SetConcurrency(1)

	result, err := applier.Apply(ctx, &types.Plan{Changes: changesFor(5, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || result.SuccessCount != 2 || result.ErrorCount != 8 {
		t.Fatalf("calls = %d, result = %+v", calls.Load(), result)
	}
	for _, e := range result.Errors {
		if e.Error != context.Canceled.Error() {
			t.Errorf("%s on %s failed with %q, want the context error", e.Change.Tag, e.Change.Resource.ID, e.Error)
		}
	}
}

func TestRealApplier_RejectsUnsupportedActionsBeforeAnyCall(t *testing.T) {
	for _, action := range []types.ChangeAction{"", "rename"} {
		t.Run(string(action), func(t *testing.T) {
			var calls atomic.Int32
			p := funcProvider(func(string) error { calls.Add(1); return nil })
			changes := changesFor(2, 1)
			changes[1].Action = action

			_, err := NewApplier([]provider.Provider{p}).Apply(context.Background(), &types.Plan{Changes: changes})

			if err == nil || !strings.Contains(err.Error(), `"tag0"`) || !strings.Contains(err.Error(), "i-1") {
				t.Errorf("err = %v, want an error naming the tag and resource", err)
			}
			if calls.Load() != 0 {
				t.Errorf("provider called %d times, want 0", calls.Load())
			}
		})
	}
}

type accountAwareProvider struct {
	slowProvider
	account string
	tagged  []string
}

func (p *accountAwareProvider) AccountID() string { return p.account }
func (p *accountAwareProvider) ApplyTags(ctx context.Context, id string, tags map[string]string) error {
	p.mu.Lock()
	p.tagged = append(p.tagged, id)
	p.mu.Unlock()
	return nil
}

func TestRealApplier_RoutesChangesToTheirOwnAccount(t *testing.T) {
	a := &accountAwareProvider{account: "111111111111"}
	b := &accountAwareProvider{account: "222222222222"}
	plan := &types.Plan{Changes: []types.TagChange{
		{Resource: types.Resource{ID: "i-a", Provider: "aws", Account: "111111111111", Type: "aws_instance"}, Tag: "owner", NewValue: "x", Action: types.ActionAdd},
		{Resource: types.Resource{ID: "i-b", Provider: "aws", Account: "222222222222", Type: "aws_instance"}, Tag: "owner", NewValue: "x", Action: types.ActionAdd},
		{Resource: types.Resource{ID: "i-c", Provider: "aws", Account: "333333333333", Type: "aws_instance"}, Tag: "owner", NewValue: "x", Action: types.ActionAdd},
	}}
	result, err := NewApplier([]provider.Provider{a, b}).Apply(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.tagged) != 1 || a.tagged[0] != "i-a" || len(b.tagged) != 1 || b.tagged[0] != "i-b" {
		t.Errorf("a tagged %v, b tagged %v", a.tagged, b.tagged)
	}
	if result.ErrorCount != 1 || !strings.Contains(result.Errors[0].Error, `account "333333333333"`) {
		t.Errorf("unconfigured account must fail, got %+v", result)
	}
}

type resourceRecorder struct {
	funcProvider
	mu     sync.Mutex
	tagged map[string]types.Resource
	tags   map[string]map[string]string
}

func (p *resourceRecorder) TagResource(_ context.Context, resource types.Resource, tags map[string]string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tagged[resource.ID] = resource
	p.tags[resource.ID] = tags
	return nil
}

func TestRealApplier_HandsTheWholeResourceToResourceTaggers(t *testing.T) {
	p := &resourceRecorder{
		funcProvider: func(id string) error { return fmt.Errorf("ApplyTags called for %s instead of TagResource", id) },
		tagged:       map[string]types.Resource{},
		tags:         map[string]map[string]string{},
	}
	instance := types.Resource{ID: "i-1", ARN: "arn:aws:ec2:eu-west-1:123456789012:instance/i-1", Provider: "aws", Type: "aws_instance", Region: "eu-west-1"}
	volume := types.Resource{ID: "vol-1", Provider: "aws", Type: "aws_ebs_volume", Region: "ap-south-1"}
	plan := &types.Plan{Changes: []types.TagChange{
		{Resource: instance, Tag: "owner", NewValue: "x", Action: types.ActionAdd},
		{Resource: instance, Tag: "team", NewValue: "y", Action: types.ActionUpdate},
		{Resource: volume, Tag: "owner", NewValue: "x", Action: types.ActionAdd},
	}}

	result, err := NewApplier([]provider.Provider{p}).Apply(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorCount != 0 {
		t.Fatalf("errors = %+v", result.Errors)
	}
	for _, want := range []types.Resource{instance, volume} {
		got := p.tagged[want.ID]
		if got.Identity() != want.Identity() || got.ARN != want.ARN || got.Type != want.Type || got.Region != want.Region {
			t.Errorf("TagResource got %+v, want %+v", got, want)
		}
	}
	if want := map[string]string{"owner": "x", "team": "y"}; !maps.Equal(p.tags["i-1"], want) {
		t.Errorf("tags of i-1 = %v, want %v in one call", p.tags["i-1"], want)
	}
}

func TestRealApplier_AddressesOtherProvidersByResourceID(t *testing.T) {
	var ids []string
	p := funcProvider(func(id string) error { ids = append(ids, id); return nil })
	plan := &types.Plan{Changes: []types.TagChange{{
		Resource: types.Resource{ID: "fn", ARN: "arn:aws:lambda:us-east-1:123456789012:function:fn", Provider: "aws", Type: "aws_lambda_function", Region: "us-east-1"},
		Tag:      "owner", NewValue: "x", Action: types.ActionAdd,
	}}}

	result, err := NewApplier([]provider.Provider{p}).Apply(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorCount != 0 || !slices.Equal(ids, []string{"fn"}) {
		t.Errorf("ApplyTags called with %q, errors %+v; want the resource ID", ids, result.Errors)
	}
}

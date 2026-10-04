package engine

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// ApplyResult contains the results of applying a plan.
type ApplyResult struct {
	// TotalChanges is the total number of changes attempted.
	TotalChanges int

	// SuccessCount is the number of successful changes.
	SuccessCount int

	// ErrorCount is the number of failed changes.
	ErrorCount int

	// Errors contains details of any failures.
	Errors []ApplyError

	// Duration is how long the apply took.
	Duration time.Duration
}

// ApplyError represents a single failed change.
type ApplyError struct {
	Change types.TagChange
	Error  string
}

// Applier applies tag changes to cloud resources.
type Applier interface {
	// Apply executes the changes in a plan.
	Apply(ctx context.Context, plan *types.Plan) (*ApplyResult, error)
}

// ApplyCallback is called once per change; calls never run concurrently.
type ApplyCallback func(change types.TagChange, success bool, err error)

// RealApplier is the production implementation of Applier.
type RealApplier struct {
	providers   map[string]provider.Provider
	concurrency int
	callback    ApplyCallback
}

// accountProvider is implemented by providers bound to one cloud account.
type accountProvider interface {
	AccountID() string
}

// regionalTagger is implemented by providers that need a resource's region to
// address it, because the tagging identifier alone does not carry one.
type regionalTagger interface {
	ApplyTagsInRegion(ctx context.Context, resourceID, region string, tags map[string]string) error
}

// providerKey addresses a provider by name and, when known, account, so a
// plan entry is only ever applied through the credentials of its own account.
func providerKey(name, account string) string {
	if account == "" {
		return name
	}
	return name + "/" + account
}

// NewApplier creates a new Applier with the given providers.
func NewApplier(providers []provider.Provider) *RealApplier {
	providerMap := make(map[string]provider.Provider)
	for _, p := range providers {
		if _, seen := providerMap[p.Name()]; !seen {
			providerMap[p.Name()] = p
		}
		if acc, ok := p.(accountProvider); ok {
			providerMap[providerKey(p.Name(), acc.AccountID())] = p
		}
	}

	return &RealApplier{
		providers:   providerMap,
		concurrency: 10, // Default concurrency
	}
}

// SetConcurrency sets the number of concurrent operations.
func (a *RealApplier) SetConcurrency(n int) {
	if n > 0 {
		a.concurrency = n
	}
}

// SetCallback sets the callback for each change applied.
func (a *RealApplier) SetCallback(cb ApplyCallback) {
	a.callback = cb
}

// ValidatePlan returns an error for the first change the applier cannot
// perform: an unknown action, a tag without a name, or a tag that one
// resource is asked to both set and remove.
func ValidatePlan(plan *types.Plan) error {
	type key struct{ identity, tag string }
	removed := make(map[key]bool, len(plan.Changes))
	for _, c := range plan.Changes {
		identity := c.Resource.Identity()
		switch c.Action {
		case types.ActionAdd, types.ActionUpdate, types.ActionRemove:
		default:
			return fmt.Errorf("unsupported action %q for tag %q on %s: apply only adds, updates and removes tags",
				c.Action, c.Tag, identity)
		}
		if c.Tag == "" {
			return fmt.Errorf("%s change without a tag name on %s", c.Action, identity)
		}

		k := key{identity, c.Tag}
		removing := c.Action == types.ActionRemove
		if previous, seen := removed[k]; seen && previous != removing {
			return fmt.Errorf("tag %q on %s is both set and removed by the plan", c.Tag, identity)
		}
		removed[k] = removing
	}
	return nil
}

// Apply executes the changes in a plan. It rejects the whole plan before any
// provider call when ValidatePlan fails.
func (a *RealApplier) Apply(ctx context.Context, plan *types.Plan) (*ApplyResult, error) {
	if err := ValidatePlan(plan); err != nil {
		return nil, err
	}

	start := time.Now()

	result := &ApplyResult{
		TotalChanges: len(plan.Changes),
	}

	if len(plan.Changes) == 0 {
		result.Duration = time.Since(start)
		return result, nil
	}

	// Group changes by resource to batch tag updates
	changesByResource := groupChangesByResource(plan.Changes)

	sem := make(chan struct{}, a.concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	record := func(changes []types.TagChange, setErr, removeErr error) {
		mu.Lock()
		defer mu.Unlock()
		for _, change := range changes {
			err := setErr
			if change.Action == types.ActionRemove {
				err = removeErr
			}
			if err != nil {
				result.Errors = append(result.Errors, ApplyError{Change: change, Error: err.Error()})
			}
			if a.callback != nil {
				a.callback(change, err == nil, err)
			}
		}
	}

	for _, changes := range changesByResource {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			record(changes, ctx.Err(), ctx.Err())
			continue
		}
		wg.Go(func() {
			defer func() { <-sem }()
			setErr, removeErr := a.applyResourceChanges(ctx, changes)
			record(changes, setErr, removeErr)
		})
	}
	wg.Wait()

	result.ErrorCount = len(result.Errors)
	result.SuccessCount = result.TotalChanges - result.ErrorCount
	result.Duration = time.Since(start)
	return result, nil
}

// applyResourceChanges applies every tag change for a single resource: one
// provider call for the tags to set, then one for the keys to remove. It
// returns the error of each half. Nothing is removed when setting failed, so
// a rename never drops the old key without the new one in place.
func (a *RealApplier) applyResourceChanges(ctx context.Context, changes []types.TagChange) (setErr, removeErr error) {
	// select picks at random, so a slot can still be handed out after cancel.
	if err := ctx.Err(); err != nil {
		return err, err
	}

	resource := changes[0].Resource
	p, ok := a.providers[providerKey(resource.Provider, resource.Account)]
	if !ok {
		err := fmt.Errorf("no %s provider configured for account %q", resource.Provider, resource.Account)
		return err, err
	}

	var set []types.TagChange
	var remove []string
	for _, change := range changes {
		if change.Action == types.ActionRemove {
			remove = append(remove, change.Tag)
		} else {
			set = append(set, change)
		}
	}

	if len(set) > 0 {
		if setErr = setTags(ctx, p, resource, set); setErr != nil {
			return setErr, fmt.Errorf("not removed: setting the other tags of the resource failed: %w", setErr)
		}
	}
	if len(remove) == 0 {
		return nil, nil
	}
	remover, ok := p.(provider.TagRemover)
	if !ok {
		return nil, fmt.Errorf("the %s provider cannot remove tags", resource.Provider)
	}
	slices.Sort(remove)
	return nil, remover.RemoveTags(ctx, resource, remove)
}

// setTags writes the tags of changes on a resource with one provider call.
func setTags(ctx context.Context, p provider.Provider, resource types.Resource, changes []types.TagChange) error {
	tags := make(map[string]string, len(changes))
	for _, change := range changes {
		tags[change.Tag] = change.NewValue
	}
	if regional, ok := p.(regionalTagger); ok {
		return regional.ApplyTagsInRegion(ctx, taggingIdentifier(resource), resource.Region, tags)
	}
	return p.ApplyTags(ctx, taggingIdentifier(resource), tags)
}

// idAddressedTypes lists the AWS resource types whose tagging API takes the
// bare ID (EC2 family). Every other type is addressed by ARN.
var idAddressedTypes = map[string]bool{
	"aws_instance":         true,
	"aws_ami":              true,
	"aws_ebs_volume":       true,
	"aws_ebs_snapshot":     true,
	"aws_launch_template":  true,
	"aws_vpc":              true,
	"aws_subnet":           true,
	"aws_security_group":   true,
	"aws_internet_gateway": true,
	"aws_nat_gateway":      true,
	"aws_vpc_endpoint":     true,
	"aws_eip":              true,
}

// taggingIdentifier returns the identifier a provider expects when tagging a
// resource: the ID for idAddressedTypes, the ARN otherwise.
func taggingIdentifier(resource types.Resource) string {
	if resource.Provider == "aws" && resource.ARN != "" && !idAddressedTypes[resource.Type] {
		return resource.ARN
	}
	return resource.ID
}

// groupChangesByResource groups changes by resource identity.
func groupChangesByResource(changes []types.TagChange) map[string][]types.TagChange {
	grouped := make(map[string][]types.TagChange)
	for _, change := range changes {
		key := change.Resource.Identity()
		grouped[key] = append(grouped[key], change)
	}
	return grouped
}

// MockApplier is an Applier implementation that simulates changes.
type MockApplier struct{}

// NewMockApplier creates a new MockApplier.
func NewMockApplier() *MockApplier {
	return &MockApplier{}
}

// Apply simulates applying changes and returns success.
func (a *MockApplier) Apply(ctx context.Context, plan *types.Plan) (*ApplyResult, error) {
	start := time.Now()

	// Simulate applying each change
	for range plan.Changes {
		time.Sleep(50 * time.Millisecond)
	}

	return &ApplyResult{
		TotalChanges: len(plan.Changes),
		SuccessCount: len(plan.Changes),
		ErrorCount:   0,
		Errors:       nil,
		Duration:     time.Since(start),
	}, nil
}

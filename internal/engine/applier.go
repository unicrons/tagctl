package engine

import (
	"context"
	"fmt"
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

// ValidatePlan returns an error for the first change that is not an add or an
// update, the only actions the applier performs.
func ValidatePlan(plan *types.Plan) error {
	for _, c := range plan.Changes {
		if c.Action != types.ActionAdd && c.Action != types.ActionUpdate {
			return fmt.Errorf("unsupported action %q for tag %q on %s: apply only adds and updates tags",
				c.Action, c.Tag, c.Resource.Identity())
		}
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
	record := func(changes []types.TagChange, err error) {
		mu.Lock()
		defer mu.Unlock()
		for _, change := range changes {
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
			record(changes, ctx.Err())
			continue
		}
		wg.Go(func() {
			defer func() { <-sem }()
			record(changes, a.applyResourceChanges(ctx, changes))
		})
	}
	wg.Wait()

	result.ErrorCount = len(result.Errors)
	result.SuccessCount = result.TotalChanges - result.ErrorCount
	result.Duration = time.Since(start)
	return result, nil
}

// applyResourceChanges applies every tag change for a single resource in one
// provider call.
func (a *RealApplier) applyResourceChanges(ctx context.Context, changes []types.TagChange) error {
	// select picks at random, so a slot can still be handed out after cancel.
	if err := ctx.Err(); err != nil {
		return err
	}

	resource := changes[0].Resource
	p, ok := a.providers[providerKey(resource.Provider, resource.Account)]
	if !ok {
		return fmt.Errorf("no %s provider configured for account %q", resource.Provider, resource.Account)
	}

	tags := make(map[string]string, len(changes))
	for _, change := range changes {
		tags[change.Tag] = change.NewValue
	}
	return p.ApplyTags(ctx, taggingIdentifier(resource), tags)
}

// idAddressedTypes lists the AWS resource types whose tagging API takes the
// bare ID (EC2 family) or name (S3). Every other type is addressed by ARN.
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
	"aws_s3_bucket":        true,
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

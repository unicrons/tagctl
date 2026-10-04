package demo

import (
	"context"
	"time"

	"github.com/unicrons/tagctl/internal/engine"
	"github.com/unicrons/tagctl/internal/types"
)

// changeDelay makes a simulated apply take as long as a small real one.
const changeDelay = 50 * time.Millisecond

// Applier is an engine.Applier that simulates an apply: every change succeeds
// and no provider is called.
type Applier struct{}

// Apply reports every change of the plan as applied.
func (Applier) Apply(_ context.Context, plan *types.Plan) (*engine.ApplyResult, error) {
	start := time.Now()

	for range plan.Changes {
		time.Sleep(changeDelay)
	}

	return &engine.ApplyResult{
		TotalChanges: len(plan.Changes),
		SuccessCount: len(plan.Changes),
		Duration:     time.Since(start),
	}, nil
}

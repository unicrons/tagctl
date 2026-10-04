package demo

import (
	"regexp"
	"testing"
	"time"

	"github.com/unicrons/tagctl/internal/engine"
)

func TestPlan_MatchesGolden(t *testing.T) {
	plan := Plan()

	if !regexp.MustCompile(`^plan-\d{8}-\d{6}$`).MatchString(plan.ID) {
		t.Errorf("ID = %q, want plan-YYYYMMDD-HHMMSS", plan.ID)
	}
	if age := time.Since(plan.CreatedAt); age < 0 || age > time.Minute {
		t.Errorf("CreatedAt = %s, want the current time", plan.CreatedAt)
	}
	plan.ID, plan.CreatedAt = "plan-demo", time.Time{}
	assertGolden(t, "demo-plan.golden.json", plan)
}

func TestPlan_IsAPlanApplyAccepts(t *testing.T) {
	plan := Plan()

	if err := engine.ValidatePlan(plan); err != nil {
		t.Errorf("ValidatePlan() = %v", err)
	}
	if plan.IsEmpty() || plan.Summary != plan.Summarize() {
		t.Errorf("Summary = %+v, want %+v", plan.Summary, plan.Summarize())
	}
}

package engine

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/types"
)

// Planner generates fix plans based on scan results and rules.
type Planner interface {
	// Plan generates a fix plan from scan results.
	Plan(ctx context.Context, scanResult *types.ScanResult) (*types.Plan, error)
}

// RealPlanner generates fix plans based on scan results and configured rules.
type RealPlanner struct {
	rules    config.RulesConfig
	compiled map[string][]*compiledInferRule
}

type compiledInferRule struct {
	pattern *regexp.Regexp
	value   string
}

// NewPlanner creates a new RealPlanner with the given rules.
func NewPlanner(rules config.RulesConfig) (*RealPlanner, error) {
	log.Debug("Planner: Creating planner with %d infer rules, %d inherit rules, %d default rules",
		len(rules.Infer), len(rules.Inherit), len(rules.Defaults))

	p := &RealPlanner{
		rules:    rules,
		compiled: make(map[string][]*compiledInferRule),
	}

	// Pre-compile infer rule patterns
	for _, rule := range rules.Infer {
		for _, pattern := range rule.FromName {
			re, err := regexp.Compile(pattern.Pattern)
			if err != nil {
				return nil, fmt.Errorf("invalid infer pattern for tag %s: %w", rule.Tag, err)
			}
			p.compiled[rule.Tag] = append(p.compiled[rule.Tag], &compiledInferRule{
				pattern: re,
				value:   pattern.Value,
			})
		}
	}

	return p, nil
}

// Plan generates a fix plan based on scan violations and configured rules.
func (p *RealPlanner) Plan(ctx context.Context, scanResult *types.ScanResult) (*types.Plan, error) {
	log.Info("Planner: Analyzing %d violations for auto-fix opportunities", len(scanResult.Violations))

	plan := &types.Plan{
		ID:        fmt.Sprintf("plan-%s", time.Now().Format("20060102-150405")),
		CreatedAt: time.Now(),
	}

	// Track unique resources for summary
	resourcesWithChanges := make(map[string]bool)

	for _, violation := range scanResult.Violations {
		// Only try to fix missing tags (not invalid values for now)
		if violation.Reason != types.ReasonMissing {
			log.Debug("Planner: Skipping violation for %s.%s (reason: %s)",
				violation.Resource.ID, violation.Tag, violation.Reason)
			continue
		}

		change := p.tryFixViolation(violation)
		if change != nil {
			plan.Changes = append(plan.Changes, *change)
			resourcesWithChanges[violation.Resource.Identity()] = true
			log.Debug("Planner: Found fix for %s.%s: %s=%s (%s)",
				violation.Resource.ID, violation.Tag, change.Tag, change.NewValue, change.Reason)
		} else {
			log.Debug("Planner: No auto-fix available for %s.%s",
				violation.Resource.ID, violation.Tag)
		}
	}

	// Calculate summary
	plan.Summary = types.PlanSummary{
		TotalResources: len(resourcesWithChanges),
		TotalChanges:   len(plan.Changes),
		TagsAdded:      len(plan.Changes), // All changes are additions for now
		TagsUpdated:    0,
		TagsRemoved:    0,
	}

	log.Info("Planner: Generated plan with %d changes for %d resources",
		plan.Summary.TotalChanges, plan.Summary.TotalResources)

	return plan, nil
}

// tryFixViolation attempts to find a fix for a missing tag violation.
func (p *RealPlanner) tryFixViolation(violation types.Violation) *types.TagChange {
	// Try inference rules first
	if change := p.tryInfer(violation); change != nil {
		return change
	}

	// Try default rules
	if change := p.tryDefault(violation); change != nil {
		return change
	}

	return nil
}

// tryInfer tries to infer a tag value from the resource name.
func (p *RealPlanner) tryInfer(violation types.Violation) *types.TagChange {
	rules, ok := p.compiled[violation.Tag]
	if !ok {
		return nil
	}

	resourceName := violation.Resource.Name
	if resourceName == "" {
		resourceName = violation.Resource.ID
	}

	for _, rule := range rules {
		if rule.pattern.MatchString(resourceName) {
			return &types.TagChange{
				Resource: violation.Resource,
				Tag:      violation.Tag,
				Action:   types.ActionAdd,
				NewValue: rule.value,
				Reason:   types.ReasonInferred,
				Source:   fmt.Sprintf("name matches '%s'", rule.pattern.String()),
			}
		}
	}

	return nil
}

// tryDefault tries to apply a default value for a missing tag.
func (p *RealPlanner) tryDefault(violation types.Violation) *types.TagChange {
	for _, rule := range p.rules.Defaults {
		// Check resource type matches
		if !matchGlob(rule.Resource, violation.Resource.Type) {
			continue
		}

		// Check conditions (when clause)
		if !p.checkConditions(rule.When, violation) {
			continue
		}

		// Check if this rule sets the tag we need
		if value, ok := rule.Set[violation.Tag]; ok {
			return &types.TagChange{
				Resource: violation.Resource,
				Tag:      violation.Tag,
				Action:   types.ActionAdd,
				NewValue: value,
				Reason:   types.ReasonDefault,
				Source:   fmt.Sprintf("default for %s", rule.Resource),
			}
		}
	}

	return nil
}

// checkConditions reports whether every condition holds. A condition it does
// not understand fails, so a malformed rule never sets a tag.
func (p *RealPlanner) checkConditions(when map[string]string, violation types.Violation) bool {
	for condition, value := range when {
		tagName, ok := strings.CutPrefix(condition, config.ConditionTagPrefix)
		if !ok || tagName == "" {
			return false
		}
		if value == config.ConditionAbsent {
			if violation.Resource.HasTag(tagName) {
				return false
			}
		} else if violation.Resource.GetTag(tagName) != value {
			return false
		}
	}
	return true
}

// MockPlanner is a Planner implementation that returns mock data.
type MockPlanner struct{}

// NewMockPlanner creates a new MockPlanner.
func NewMockPlanner() *MockPlanner {
	return &MockPlanner{}
}

// Plan returns a mock plan for demonstration.
func (p *MockPlanner) Plan(ctx context.Context, scanResult *types.ScanResult) (*types.Plan, error) {
	plan := &types.Plan{
		ID:        fmt.Sprintf("plan-%s", time.Now().Format("20060102-150405")),
		CreatedAt: time.Now(),
		Changes: []types.TagChange{
			{
				Resource: types.Resource{
					ID:       "i-0abc123",
					Name:     "web-prod-api-1",
					Type:     "aws_instance",
					Account:  "production",
					Provider: "aws",
				},
				Tag:      "environment",
				Action:   types.ActionAdd,
				NewValue: "prod",
				Reason:   types.ReasonInferred,
				Source:   "name contains '-prod-'",
			},
			{
				Resource: types.Resource{
					ID:       "i-0abc123",
					Name:     "web-prod-api-1",
					Type:     "aws_instance",
					Account:  "production",
					Provider: "aws",
				},
				Tag:      "team",
				Action:   types.ActionAdd,
				NewValue: "backend",
				Reason:   types.ReasonInherited,
				Source:   "from ASG 'backend-asg'",
			},
			{
				Resource: types.Resource{
					ID:       "vol-xyz789",
					Name:     "",
					Type:     "aws_ebs_volume",
					Account:  "production",
					Provider: "aws",
				},
				Tag:      "environment",
				Action:   types.ActionAdd,
				NewValue: "prod",
				Reason:   types.ReasonInherited,
				Source:   "from attached instance i-0abc123",
			},
			{
				Resource: types.Resource{
					ID:       "legacy-bucket",
					Name:     "legacy-data-2019",
					Type:     "aws_s3_bucket",
					Account:  "production",
					Provider: "aws",
				},
				Tag:      "owner",
				Action:   types.ActionAdd,
				NewValue: "platform-team@company.com",
				Reason:   types.ReasonDefault,
				Source:   "default for untagged S3 buckets",
			},
			{
				Resource: types.Resource{
					ID:       "legacy-bucket",
					Name:     "legacy-data-2019",
					Type:     "aws_s3_bucket",
					Account:  "production",
					Provider: "aws",
				},
				Tag:      "needs-review",
				Action:   types.ActionAdd,
				NewValue: "true",
				Reason:   types.ReasonDefault,
				Source:   "default for untagged S3 buckets",
			},
		},
		Summary: types.PlanSummary{
			TotalResources: 3,
			TotalChanges:   5,
			TagsAdded:      5,
			TagsUpdated:    0,
			TagsRemoved:    0,
		},
	}

	return plan, nil
}

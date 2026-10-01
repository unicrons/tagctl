package engine

import (
	"context"
	"fmt"
	"regexp"
	"slices"
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
	rules      config.RulesConfig
	compiled   map[string][]*compiledInferRule
	tagSources map[string][]config.TagSource
}

type compiledInferRule struct {
	pattern *regexp.Regexp
	value   string
}

// NewPlanner creates a new RealPlanner with the given rules.
func NewPlanner(rules config.RulesConfig) (*RealPlanner, error) {
	p := &RealPlanner{
		rules:      rules,
		compiled:   make(map[string][]*compiledInferRule),
		tagSources: make(map[string][]config.TagSource),
	}

	// Pre-compile infer rule patterns
	for _, rule := range rules.Infer {
		p.tagSources[rule.Tag] = append(p.tagSources[rule.Tag], rule.FromTag...)
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

// Plan generates a fix plan based on the scan's failed findings and configured rules.
func (p *RealPlanner) Plan(ctx context.Context, scanResult *types.ScanResult) (*types.Plan, error) {
	failures := scanResult.FailedFindings()
	log.Info("Planner: Analyzing %d violations for auto-fix opportunities", len(failures))

	plan := &types.Plan{
		ID:        fmt.Sprintf("plan-%s", time.Now().Format("20060102-150405")),
		CreatedAt: time.Now(),
	}

	index := newScanIndex(scanResult)
	if len(p.rules.Inherit) > 0 && !index.hasParents {
		plan.Warnings = append(plan.Warnings, noParentsWarning)
	}

	// Track unique resources for summary
	resourcesWithChanges := make(map[string]bool)

	for _, finding := range failures {
		// Only try to fix missing tags (not invalid values for now)
		if finding.Reason != types.ReasonMissing {
			log.Debug("Planner: Skipping violation for %s.%s (reason: %s)",
				finding.Resource.ID, finding.Tag, finding.Reason)
			continue
		}

		change := p.tryFix(finding, index)
		if change != nil {
			plan.Changes = append(plan.Changes, *change)
			resourcesWithChanges[finding.Resource.Identity()] = true
			log.Debug("Planner: Found fix for %s.%s: %s=%s (%s)",
				finding.Resource.ID, finding.Tag, change.Tag, change.NewValue, change.Reason)
		} else {
			log.Debug("Planner: No auto-fix available for %s.%s",
				finding.Resource.ID, finding.Tag)
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

// tryFix attempts to find a fix for a missing tag finding.
func (p *RealPlanner) tryFix(finding types.Finding, index *scanIndex) *types.TagChange {
	// Try inference rules first
	if change := p.tryInfer(finding); change != nil {
		return change
	}

	if change := p.tryInherit(finding, index); change != nil {
		return change
	}

	// Try default rules
	if change := p.tryDefault(finding); change != nil {
		return change
	}

	return nil
}

// tryInfer tries to infer a tag value from another tag on the resource, then
// from the resource name.
func (p *RealPlanner) tryInfer(finding types.Finding) *types.TagChange {
	if change := p.tryInferFromTag(finding); change != nil {
		return change
	}

	rules, ok := p.compiled[finding.Tag]
	if !ok {
		return nil
	}

	resourceName := finding.Resource.Name
	if resourceName == "" {
		resourceName = finding.Resource.ID
	}

	for _, rule := range rules {
		if rule.pattern.MatchString(resourceName) {
			return &types.TagChange{
				Resource: finding.Resource,
				Tag:      finding.Tag,
				Action:   types.ActionAdd,
				NewValue: rule.value,
				Reason:   types.ReasonInferred,
				Source:   fmt.Sprintf("name matches '%s'", rule.pattern.String()),
			}
		}
	}

	return nil
}

// tryInferFromTag takes the value from the first source tag the resource
// carries with a usable value.
func (p *RealPlanner) tryInferFromTag(finding types.Finding) *types.TagChange {
	for _, source := range p.tagSources[finding.Tag] {
		value, ok := finding.Resource.Tags[source.Tag]
		if !ok {
			continue
		}
		if len(source.Values) > 0 {
			if value, ok = source.Values[value]; !ok {
				continue
			}
		}
		if value == "" {
			continue
		}
		return &types.TagChange{
			Resource: finding.Resource,
			Tag:      finding.Tag,
			Action:   types.ActionAdd,
			NewValue: value,
			Reason:   types.ReasonInferred,
			Source:   fmt.Sprintf("from tag '%s'", source.Tag),
		}
	}
	return nil
}

// noParentsWarning is what a plan reports when inherit rules had nothing to
// resolve parents with.
const noParentsWarning = "rules.inherit could not be applied: the scan records no parent resources. " +
	"A scan written by an older tagctl has none; run 'tagctl scan' again"

// maxInheritDepth bounds the walk up a chain of parents.
const maxInheritDepth = 8

// scanIndex resolves the parents a scan recorded.
type scanIndex struct {
	resources  map[string]types.Resource
	failed     map[string]map[string]bool // identity -> tags with a FAILED finding
	hasParents bool
}

func newScanIndex(scan *types.ScanResult) *scanIndex {
	index := &scanIndex{
		resources: make(map[string]types.Resource),
		failed:    make(map[string]map[string]bool),
	}
	add := func(f types.Finding) {
		id := f.Resource.Identity()
		index.resources[id] = f.Resource
		if len(f.Resource.Parents) > 0 {
			index.hasParents = true
		}
		if f.Status == types.StatusFailed {
			if index.failed[id] == nil {
				index.failed[id] = make(map[string]bool)
			}
			index.failed[id][f.Tag] = true
		}
	}
	for _, f := range scan.Findings {
		add(f)
	}
	if len(scan.Findings) == 0 {
		for _, f := range types.ViolationsToFindings(scan.Violations) {
			add(f)
		}
	}
	return index
}

// tryInherit copies a missing tag from the parent the first matching rule
// points at.
func (p *RealPlanner) tryInherit(finding types.Finding, index *scanIndex) *types.TagChange {
	relation, parent, value := p.inherited(finding.Resource, finding.Tag, index, 0)
	if relation == "" {
		return nil
	}
	return &types.TagChange{
		Resource: finding.Resource,
		Tag:      finding.Tag,
		Action:   types.ActionAdd,
		NewValue: value,
		Reason:   types.ReasonInherited,
		Source:   fmt.Sprintf("from %s %s", strings.ReplaceAll(relation, "_", " "), parent.ID),
	}
}

// inherited returns the relation, parent and value a resource inherits for a
// tag, or an empty relation. A parent that lacks the tag hands down what it
// inherits itself; one whose value fails the policy hands down nothing.
func (p *RealPlanner) inherited(resource types.Resource, tag string, index *scanIndex, depth int) (string, types.Resource, string) {
	if depth >= maxInheritDepth {
		return "", types.Resource{}, ""
	}
	for _, rule := range p.rules.Inherit {
		if !matchGlob(rule.Resource, resource.Type) || !slices.Contains(rule.Tags, tag) {
			continue
		}
		for _, relation := range parentRelations(resource, rule.From) {
			parent, ok := index.resources[resource.Parents[relation]]
			if !ok {
				continue
			}
			value, present := parent.Tags[tag]
			switch {
			case present && index.failed[parent.Identity()][tag]:
				continue
			case !present:
				if _, _, value = p.inherited(parent, tag, index, depth+1); value == "" {
					continue
				}
			case value == "":
				continue
			}
			return relation, parent, value
		}
	}
	return "", types.Resource{}, ""
}

// parentRelations lists the relations of a resource a rule may follow: the
// one it names, or all of them in a stable order.
func parentRelations(resource types.Resource, from string) []string {
	if from != "" {
		if _, ok := resource.Parents[from]; !ok {
			return nil
		}
		return []string{from}
	}
	relations := make([]string, 0, len(resource.Parents))
	for relation := range resource.Parents {
		relations = append(relations, relation)
	}
	slices.Sort(relations)
	return relations
}

// tryDefault tries to apply a default value for a missing tag.
func (p *RealPlanner) tryDefault(finding types.Finding) *types.TagChange {
	for _, rule := range p.rules.Defaults {
		// Check resource type matches
		if !matchGlob(rule.Resource, finding.Resource.Type) {
			continue
		}

		// Check conditions (when clause)
		if !p.checkConditions(rule.When, finding) {
			continue
		}

		// Check if this rule sets the tag we need
		if value, ok := rule.Set[finding.Tag]; ok {
			return &types.TagChange{
				Resource: finding.Resource,
				Tag:      finding.Tag,
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
func (p *RealPlanner) checkConditions(when map[string]string, finding types.Finding) bool {
	for condition, value := range when {
		tagName, ok := strings.CutPrefix(condition, config.ConditionTagPrefix)
		if !ok || tagName == "" {
			return false
		}
		if value == config.ConditionAbsent {
			if finding.Resource.HasTag(tagName) {
				return false
			}
		} else if finding.Resource.GetTag(tagName) != value {
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

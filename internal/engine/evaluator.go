package engine

import (
	"regexp"
	"slices"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/types"
)

// Evaluator evaluates resources against tag policies.
type Evaluator struct {
	policy    config.PolicyConfig
	compiled  map[string]*regexp.Regexp // keyed by pattern text
	forbidden map[string]config.ForbiddenTag
	checked   map[string]bool // tags with a required or optional requirement
}

// NewEvaluator creates a new Evaluator with the given policy.
func NewEvaluator(policy config.PolicyConfig) (*Evaluator, error) {
	e := &Evaluator{
		policy:    policy,
		compiled:  make(map[string]*regexp.Regexp),
		forbidden: make(map[string]config.ForbiddenTag, len(policy.Forbidden)),
		checked:   make(map[string]bool),
	}

	for _, req := range slices.Concat(policy.Required, policy.Optional) {
		if err := e.compile(req.Name, req.Pattern); err != nil {
			return nil, err
		}
		e.checked[req.Name] = true
	}
	for _, tag := range policy.Forbidden {
		if err := e.compile(tag.Name, tag.Pattern); err != nil {
			return nil, err
		}
		e.forbidden[tag.Name] = tag
	}

	return e, nil
}

func (e *Evaluator) compile(tag, pattern string) error {
	if pattern == "" || e.compiled[pattern] != nil {
		return nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return &EvaluatorError{Tag: tag, Message: "invalid pattern: " + err.Error()}
	}
	e.compiled[pattern] = re
	return nil
}

// EvaluatorError represents an error in the evaluator.
type EvaluatorError struct {
	Tag     string
	Message string
}

func (e *EvaluatorError) Error() string {
	return "evaluator error for tag '" + e.Tag + "': " + e.Message
}

// EvaluateResourceFindings evaluates a single resource and returns all findings
// (PASS and FAILED). An optional tag is only checked when the resource carries
// it, and a forbidden tag only produces a finding when it is violated.
func (e *Evaluator) EvaluateResourceFindings(resource types.Resource) []types.Finding {
	findings := make([]types.Finding, 0, len(e.policy.Required)+len(e.policy.Optional))

	for _, req := range e.policy.Required {
		findings = append(findings, e.evaluateRequirement(resource, req))
	}
	for _, req := range e.policy.Optional {
		if resource.HasTag(req.Name) {
			findings = append(findings, e.evaluateRequirement(resource, req))
		}
	}
	for _, tag := range e.policy.Forbidden {
		if e.checked[tag.Name] {
			continue // reported by evaluateRequirement, one finding per tag
		}
		if finding, violated := e.forbiddenFinding(resource, tag); violated {
			findings = append(findings, finding)
		}
	}

	return findings
}

// forbiddenFinding returns the FAILED finding for a forbidden tag the
// resource carries. A value only known after apply cannot match a value rule.
func (e *Evaluator) forbiddenFinding(resource types.Resource, tag config.ForbiddenTag) (types.Finding, bool) {
	finding := types.Finding{Resource: resource, Tag: tag.Name, Status: types.StatusFailed, Reason: types.ReasonForbidden}

	value, present := resource.Tags[tag.Name]
	unknown := valueUnknown(resource, tag.Name)
	if tag.Unconditional() {
		finding.Actual = value
		return finding, present || unknown
	}
	if !present || unknown {
		return finding, false
	}

	finding.Actual = value
	switch {
	case slices.Contains(tag.Values, value):
		finding.Expected = "not one of: " + formatAllowedValues(tag.Values)
	case tag.Pattern != "" && e.compiled[tag.Pattern].MatchString(value):
		finding.Expected = "not matching: " + tag.Pattern
	default:
		return finding, false
	}
	return finding, true
}

// evaluateRequirement checks one tag requirement: presence, then allowed values, then pattern.
func (e *Evaluator) evaluateRequirement(resource types.Resource, req config.TagRequirement) types.Finding {
	if tag, ok := e.forbidden[req.Name]; ok {
		if finding, violated := e.forbiddenFinding(resource, tag); violated {
			return finding
		}
	}

	finding := types.Finding{Resource: resource, Tag: req.Name, Status: types.StatusFailed}

	value, ok := resource.Tags[req.Name]
	switch {
	case valueUnknown(resource, req.Name):
		finding.Status = types.StatusPass
		finding.Reason = types.ReasonCompliant
		return finding
	case !ok:
		finding.Reason = types.ReasonMissing
		return finding
	case len(req.Values) > 0 && !slices.Contains(req.Values, value):
		finding.Reason = types.ReasonInvalidValue
		finding.Expected = formatAllowedValues(req.Values)
	case req.Pattern != "" && !e.compiled[req.Pattern].MatchString(value):
		finding.Reason = types.ReasonInvalidFormat
		finding.Expected = req.Pattern
	default:
		finding.Status = types.StatusPass
		finding.Reason = types.ReasonCompliant
	}
	finding.Actual = value

	return finding
}

// valueUnknown reports whether a tag's value is only known once the resource
// exists: the key counts as present and compliant, its value is not checked.
func valueUnknown(resource types.Resource, tag string) bool {
	return slices.Contains(resource.UnknownTags, tag)
}

// EvaluateResources evaluates multiple resources and returns a ScanResult.
func (e *Evaluator) EvaluateResources(resources []types.Resource) *types.ScanResult {
	result := types.NewScanResult()
	result.TotalResources = len(resources)
	result.Resources = make([]types.ResourceRef, 0, len(resources))

	for _, req := range e.policy.Required {
		result.ByTag[req.Name] = &types.TagStats{Tag: req.Name, Required: true}
	}
	for _, req := range e.policy.Optional {
		result.ByTag[req.Name] = &types.TagStats{Tag: req.Name}
	}
	for _, tag := range e.policy.Forbidden {
		if result.ByTag[tag.Name] == nil {
			result.ByTag[tag.Name] = &types.TagStats{Tag: tag.Name}
		}
		result.ByTag[tag.Name].Forbidden = true
	}

	nonCompliant := make(map[string]bool)
	for _, resource := range resources {
		accountKey := resource.Account
		if accountKey == "" {
			accountKey = "unknown"
		}
		account := result.ByAccount[accountKey]
		if account == nil {
			account = &types.AccountStats{Account: accountKey, Provider: resource.Provider}
			result.ByAccount[accountKey] = account
		}
		account.Total++
		result.Resources = append(result.Resources, resource.Ref())

		findings := e.EvaluateResourceFindings(resource)
		result.Findings = append(result.Findings, findings...)

		compliant := true
		for _, f := range findings {
			countFinding(result.ByTag[f.Tag], f)
			if f.Status == types.StatusFailed {
				compliant = false
				result.Violations = append(result.Violations, types.Violation(f))
			}
		}
		if compliant {
			account.Compliant++
		} else {
			nonCompliant[resource.Identity()] = true
		}
	}

	result.ViolationCount = len(result.Violations)
	result.CompliantCount = result.TotalResources - len(nonCompliant)
	result.CalculateCompliance()

	return result
}

// countFinding adds a finding to its tag's stats. Optional and forbidden tags
// only produce findings when present, so they never count as missing.
func countFinding(stats *types.TagStats, f types.Finding) {
	switch f.Reason {
	case types.ReasonMissing:
		stats.Missing++
	case types.ReasonInvalidValue, types.ReasonInvalidFormat, types.ReasonForbidden:
		stats.Present++
		stats.Invalid++
	default:
		stats.Present++
	}
}

// IsCompliant checks if a resource is fully compliant with the policy.
func (e *Evaluator) IsCompliant(resource types.Resource) bool {
	return !slices.ContainsFunc(e.EvaluateResourceFindings(resource), func(f types.Finding) bool {
		return f.Status == types.StatusFailed
	})
}

// formatAllowedValues formats a list of allowed values for display.
func formatAllowedValues(values []string) string {
	if len(values) == 0 {
		return ""
	}
	if len(values) == 1 {
		return values[0]
	}

	result := "["
	for i, v := range values {
		if i > 0 {
			result += ", "
		}
		result += v
	}
	result += "]"
	return result
}

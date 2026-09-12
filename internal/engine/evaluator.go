package engine

import (
	"regexp"
	"slices"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/types"
)

// Evaluator evaluates resources against tag policies.
type Evaluator struct {
	policy   config.PolicyConfig
	compiled map[string]*regexp.Regexp
}

// NewEvaluator creates a new Evaluator with the given policy.
func NewEvaluator(policy config.PolicyConfig) (*Evaluator, error) {
	e := &Evaluator{
		policy:   policy,
		compiled: make(map[string]*regexp.Regexp),
	}

	// Pre-compile all regex patterns for efficiency
	for _, req := range policy.Required {
		if req.Pattern != "" {
			re, err := regexp.Compile(req.Pattern)
			if err != nil {
				return nil, &EvaluatorError{
					Tag:     req.Name,
					Message: "invalid pattern: " + err.Error(),
				}
			}
			e.compiled[req.Name] = re
		}
	}

	for _, req := range policy.Optional {
		if req.Pattern != "" {
			re, err := regexp.Compile(req.Pattern)
			if err != nil {
				return nil, &EvaluatorError{
					Tag:     req.Name,
					Message: "invalid pattern: " + err.Error(),
				}
			}
			e.compiled[req.Name] = re
		}
	}

	return e, nil
}

// EvaluatorError represents an error in the evaluator.
type EvaluatorError struct {
	Tag     string
	Message string
}

func (e *EvaluatorError) Error() string {
	return "evaluator error for tag '" + e.Tag + "': " + e.Message
}

// EvaluateResource evaluates a single resource against the policy.
// Returns a list of violations (empty if compliant).
func (e *Evaluator) EvaluateResource(resource types.Resource) []types.Violation {
	var violations []types.Violation

	for _, req := range e.policy.Required {
		v := e.evaluateRequirement(resource, req, true)
		if v != nil {
			violations = append(violations, *v)
		}
	}

	// Optional tags are tracked but don't generate violations for missing
	// Only check value validity if the tag is present
	for _, req := range e.policy.Optional {
		if resource.HasTag(req.Name) {
			v := e.evaluateRequirement(resource, req, false)
			if v != nil {
				violations = append(violations, *v)
			}
		}
	}

	return violations
}

// EvaluateResourceFindings evaluates a single resource and returns all findings (PASS and FAILED).
func (e *Evaluator) EvaluateResourceFindings(resource types.Resource) []types.Finding {
	findings := make([]types.Finding, 0, len(e.policy.Required))

	for _, req := range e.policy.Required {
		f := e.evaluateRequirementFinding(resource, req, true)
		findings = append(findings, f)
	}

	// Optional tags: only check if present
	for _, req := range e.policy.Optional {
		if resource.HasTag(req.Name) {
			f := e.evaluateRequirementFinding(resource, req, false)
			findings = append(findings, f)
		}
	}

	return findings
}

// evaluateRequirementFinding checks a single tag requirement and returns a Finding.
func (e *Evaluator) evaluateRequirementFinding(resource types.Resource, req config.TagRequirement, required bool) types.Finding {
	value, hasTag := resource.Tags[req.Name]

	if valueUnknown(resource, req.Name) {
		return types.Finding{
			Resource: resource,
			Tag:      req.Name,
			Status:   types.StatusPass,
			Reason:   types.ReasonCompliant,
		}
	}

	// Check if tag is missing
	if !hasTag {
		if required {
			return types.Finding{
				Resource: resource,
				Tag:      req.Name,
				Status:   types.StatusFailed,
				Reason:   types.ReasonMissing,
			}
		}
		// Optional tag missing - this shouldn't happen as we check HasTag before calling
		return types.Finding{
			Resource: resource,
			Tag:      req.Name,
			Status:   types.StatusPass,
			Reason:   types.ReasonCompliant,
		}
	}

	// Check allowed values
	if len(req.Values) > 0 {
		valid := false
		for _, allowed := range req.Values {
			if value == allowed {
				valid = true
				break
			}
		}
		if !valid {
			return types.Finding{
				Resource: resource,
				Tag:      req.Name,
				Status:   types.StatusFailed,
				Reason:   types.ReasonInvalidValue,
				Actual:   value,
				Expected: formatAllowedValues(req.Values),
			}
		}
	}

	// Check pattern
	if req.Pattern != "" {
		re := e.compiled[req.Name]
		if re != nil && !re.MatchString(value) {
			return types.Finding{
				Resource: resource,
				Tag:      req.Name,
				Status:   types.StatusFailed,
				Reason:   types.ReasonInvalidFormat,
				Actual:   value,
				Expected: req.Pattern,
			}
		}
	}

	// All checks passed
	return types.Finding{
		Resource: resource,
		Tag:      req.Name,
		Status:   types.StatusPass,
		Reason:   types.ReasonCompliant,
		Actual:   value,
	}
}

// evaluateRequirement checks a single tag requirement against a resource.
func (e *Evaluator) evaluateRequirement(resource types.Resource, req config.TagRequirement, required bool) *types.Violation {
	value, hasTag := resource.Tags[req.Name]

	if valueUnknown(resource, req.Name) {
		return nil
	}

	// Check if tag is missing
	if !hasTag {
		if required {
			return &types.Violation{
				Resource: resource,
				Tag:      req.Name,
				Status:   types.StatusFailed,
				Reason:   types.ReasonMissing,
			}
		}
		return nil
	}

	// Check allowed values
	if len(req.Values) > 0 {
		valid := false
		for _, allowed := range req.Values {
			if value == allowed {
				valid = true
				break
			}
		}
		if !valid {
			return &types.Violation{
				Resource: resource,
				Tag:      req.Name,
				Status:   types.StatusFailed,
				Reason:   types.ReasonInvalidValue,
				Actual:   value,
				Expected: formatAllowedValues(req.Values),
			}
		}
	}

	// Check pattern
	if req.Pattern != "" {
		re := e.compiled[req.Name]
		if re != nil && !re.MatchString(value) {
			return &types.Violation{
				Resource: resource,
				Tag:      req.Name,
				Status:   types.StatusFailed,
				Reason:   types.ReasonInvalidFormat,
				Actual:   value,
				Expected: req.Pattern,
			}
		}
	}

	return nil
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

	// Track unique resources with violations
	resourceViolations := make(map[string]bool)

	// Initialize tag stats
	for _, req := range e.policy.Required {
		result.ByTag[req.Name] = &types.TagStats{
			Tag:      req.Name,
			Required: true,
		}
	}
	for _, req := range e.policy.Optional {
		result.ByTag[req.Name] = &types.TagStats{
			Tag:      req.Name,
			Required: false,
		}
	}

	for _, resource := range resources {
		violations := e.EvaluateResource(resource)
		findings := e.EvaluateResourceFindings(resource)

		// Add findings to result
		result.Findings = append(result.Findings, findings...)

		// Track account stats
		accountKey := resource.Account
		if accountKey == "" {
			accountKey = "unknown"
		}
		if result.ByAccount[accountKey] == nil {
			result.ByAccount[accountKey] = &types.AccountStats{
				Account:  accountKey,
				Provider: resource.Provider,
			}
		}
		result.ByAccount[accountKey].Total++

		// Update tag stats based on resource tags
		for _, req := range e.policy.Required {
			stats := result.ByTag[req.Name]
			if resource.HasTag(req.Name) {
				stats.Present++
			} else {
				stats.Missing++
			}
		}
		for _, req := range e.policy.Optional {
			stats := result.ByTag[req.Name]
			if resource.HasTag(req.Name) {
				stats.Present++
			} else {
				stats.Missing++
			}
		}

		if len(violations) > 0 {
			resourceViolations[resource.Identity()] = true
			result.Violations = append(result.Violations, violations...)

			// Update invalid counts for tags with violations
			for _, v := range violations {
				if stats, ok := result.ByTag[v.Tag]; ok {
					if v.Reason == types.ReasonInvalidValue || v.Reason == types.ReasonInvalidFormat {
						stats.Invalid++
					}
				}
			}
		} else {
			result.ByAccount[accountKey].Compliant++
		}
	}

	result.ViolationCount = len(result.Violations)
	result.CompliantCount = result.TotalResources - len(resourceViolations)
	result.CalculateCompliance()

	return result
}

// IsCompliant checks if a resource is fully compliant with the policy.
func (e *Evaluator) IsCompliant(resource types.Resource) bool {
	return len(e.EvaluateResource(resource)) == 0
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

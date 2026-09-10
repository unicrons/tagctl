// Package engine contains the core business logic for tagctl.
package engine

import (
	"context"
	"sync"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// Scanner discovers cloud resources and evaluates tag compliance.
type Scanner interface {
	// Scan discovers resources and evaluates them against the policy.
	Scan(ctx context.Context) (*types.ScanResult, error)
}

// RealScanner is the production implementation of Scanner.
type RealScanner struct {
	providers []provider.Provider
	evaluator *Evaluator
	ignore    config.IgnoreConfig
}

// NewScanner creates a new Scanner with the given providers and policy.
func NewScanner(providers []provider.Provider, policy config.PolicyConfig, ignore config.IgnoreConfig) (*RealScanner, error) {
	log.Debug("Scanner: Creating scanner with %d provider(s)", len(providers))
	log.Debug("Scanner: Policy has %d required tags, %d optional tags", len(policy.Required), len(policy.Optional))

	evaluator, err := NewEvaluator(policy)
	if err != nil {
		log.Error("Scanner: Failed to create evaluator: %v", err)
		return nil, err
	}

	log.Debug("Scanner: Evaluator created successfully")
	return &RealScanner{
		providers: providers,
		evaluator: evaluator,
		ignore:    ignore,
	}, nil
}

// Scan discovers resources from all providers and evaluates them against the policy.
func (s *RealScanner) Scan(ctx context.Context) (*types.ScanResult, error) {
	log.Info("Scanner: Starting scan with %d provider(s)", len(s.providers))

	var allResources []types.Resource
	var mu sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, len(s.providers))

	// Collect resources from all providers in parallel
	for _, p := range s.providers {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()

			log.Debug("Scanner: Fetching resources from provider %s", p.Name())
			resources, err := p.ListResources(ctx)

			// Always add resources, even if there was an error (partial results)
			if len(resources) > 0 {
				log.Debug("Scanner: Provider %s returned %d resources", p.Name(), len(resources))
				mu.Lock()
				allResources = append(allResources, resources...)
				mu.Unlock()
			}

			if err != nil {
				log.Error("Scanner: Provider %s had errors: %v", p.Name(), err)
				errChan <- err
			}
		}()
	}

	wg.Wait()
	close(errChan)

	// Collect errors
	// errChan is closed after every writer finished, so len is the exact count.
	errs := make([]error, 0, len(errChan))
	for err := range errChan {
		errs = append(errs, err)
	}

	log.Info("Scanner: Collected %d total resources from all providers", len(allResources))

	// Filter ignored resources
	filteredResources := s.filterIgnored(allResources)
	if len(allResources) != len(filteredResources) {
		log.Debug("Scanner: Filtered out %d ignored resources", len(allResources)-len(filteredResources))
	}

	// Evaluate all resources
	log.Debug("Scanner: Evaluating %d resources against policy", len(filteredResources))
	result := s.evaluator.EvaluateResources(filteredResources)
	log.Info("Scanner: Evaluation complete - %d compliant, %d violations (%.1f%%)",
		result.CompliantCount, result.ViolationCount, result.CompliancePct)

	// Return partial results with first error if any
	if len(errs) > 0 {
		log.Error("Scanner: Scan completed with %d error(s)", len(errs))
		return result, errs[0]
	}

	return result, nil
}

// filterIgnored removes resources that should be ignored.
func (s *RealScanner) filterIgnored(resources []types.Resource) []types.Resource {
	if len(s.ignore.Resources) == 0 && len(s.ignore.Tags) == 0 {
		log.Debug("Scanner: No ignore rules configured, skipping filter")
		return resources
	}

	log.Debug("Scanner: Applying ignore rules (%d resource patterns, %d tag rules)",
		len(s.ignore.Resources), len(s.ignore.Tags))

	filtered := make([]types.Resource, 0, len(resources))
	for _, r := range resources {
		if s.shouldIgnore(r) {
			log.Debug("Scanner: Ignoring resource %s (%s)", r.ID, r.Type)
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}

// shouldIgnore checks if a resource should be ignored.
func (s *RealScanner) shouldIgnore(r types.Resource) bool {
	// Check resource type patterns
	for _, pattern := range s.ignore.Resources {
		if matchGlob(pattern, r.Type) {
			return true
		}
	}

	// Check tag values
	for tagKey, ignoreValues := range s.ignore.Tags {
		if value, ok := r.Tags[tagKey]; ok {
			for _, ignoreValue := range ignoreValues {
				if value == ignoreValue {
					return true
				}
			}
		}
	}

	return false
}

// matchGlob performs simple glob matching (supports * wildcard).
func matchGlob(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == value {
		return true
	}

	// Handle prefix wildcard (e.g., "aws_*")
	if len(pattern) > 1 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(value) >= len(prefix) && value[:len(prefix)] == prefix
	}

	// Handle suffix wildcard (e.g., "*_instance")
	if len(pattern) > 1 && pattern[0] == '*' {
		suffix := pattern[1:]
		return len(value) >= len(suffix) && value[len(value)-len(suffix):] == suffix
	}

	return false
}

// MockScanner is a Scanner implementation that returns mock data.
type MockScanner struct{}

// NewMockScanner creates a new MockScanner.
func NewMockScanner() *MockScanner {
	log.Debug("Scanner: Creating mock scanner for demo mode")
	return &MockScanner{}
}

// Scan returns mock scan results for demonstration.
func (s *MockScanner) Scan(ctx context.Context) (*types.ScanResult, error) {
	log.Debug("Scanner: Generating mock scan results")
	result := types.NewScanResult()
	result.TotalResources = 150
	result.CompliantCount = 98
	result.ViolationCount = 52
	result.CompliancePct = 65.3

	result.ByAccount["production"] = &types.AccountStats{
		Account:       "production",
		Provider:      "aws",
		Total:         100,
		Compliant:     85,
		CompliancePct: 85.0,
	}
	result.ByAccount["staging"] = &types.AccountStats{
		Account:       "staging",
		Provider:      "aws",
		Total:         50,
		Compliant:     13,
		CompliancePct: 26.0,
	}

	result.ByTag["environment"] = &types.TagStats{
		Tag:           "environment",
		Required:      true,
		Present:       140,
		Missing:       10,
		Invalid:       0,
		CompliancePct: 93.3,
	}
	result.ByTag["cost-center"] = &types.TagStats{
		Tag:           "cost-center",
		Required:      true,
		Present:       98,
		Missing:       52,
		Invalid:       5,
		CompliancePct: 62.0,
	}
	result.ByTag["owner"] = &types.TagStats{
		Tag:           "owner",
		Required:      true,
		Present:       75,
		Missing:       75,
		Invalid:       10,
		CompliancePct: 43.3,
	}

	// Mock resources for findings
	webProd := types.Resource{ID: "i-0prod123", Name: "web-prod-1", Type: "aws_instance", Account: "production", Provider: "aws", Region: "us-east-1", ARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-0prod123"}
	apiProd := types.Resource{ID: "i-0prod456", Name: "api-prod-1", Type: "aws_instance", Account: "production", Provider: "aws", Region: "us-east-1", ARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-0prod456"}
	webStaging := types.Resource{ID: "i-0abc123", Name: "web-staging-1", Type: "aws_instance", Account: "staging", Provider: "aws", Region: "us-east-1", ARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-0abc123"}
	apiStaging := types.Resource{ID: "i-0def456", Name: "api-staging-2", Type: "aws_instance", Account: "staging", Provider: "aws", Region: "us-east-1", ARN: "arn:aws:ec2:us-east-1:123456789012:instance/i-0def456"}
	bucketLegacy := types.Resource{ID: "bucket-legacy", Name: "legacy-data-bucket", Type: "aws_s3_bucket", Account: "production", Provider: "aws", Region: "us-west-2", ARN: "arn:aws:s3:::legacy-data-bucket"}

	result.Violations = []types.Violation{
		{Resource: webStaging, Tag: "cost-center", Reason: types.ReasonMissing},
		{Resource: apiStaging, Tag: "owner", Reason: types.ReasonMissing},
		{Resource: bucketLegacy, Tag: "owner", Reason: types.ReasonInvalidFormat, Actual: "john", Expected: "^.+@company\\.com$"},
	}

	// Findings include both PASS and FAILED
	result.Findings = []types.Finding{
		// web-prod-1: all tags pass
		{Resource: webProd, Tag: "environment", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "prod"},
		{Resource: webProd, Tag: "cost-center", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "TECH-001"},
		{Resource: webProd, Tag: "owner", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "team@company.com"},
		// api-prod-1: all tags pass
		{Resource: apiProd, Tag: "environment", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "prod"},
		{Resource: apiProd, Tag: "cost-center", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "TECH-002"},
		{Resource: apiProd, Tag: "owner", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "api@company.com"},
		// web-staging-1: cost-center missing
		{Resource: webStaging, Tag: "environment", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "staging"},
		{Resource: webStaging, Tag: "cost-center", Status: types.StatusFailed, Reason: types.ReasonMissing},
		{Resource: webStaging, Tag: "owner", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "dev@company.com"},
		// api-staging-2: owner missing
		{Resource: apiStaging, Tag: "environment", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "staging"},
		{Resource: apiStaging, Tag: "cost-center", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "DEV-001"},
		{Resource: apiStaging, Tag: "owner", Status: types.StatusFailed, Reason: types.ReasonMissing},
		// legacy-data-bucket: owner invalid
		{Resource: bucketLegacy, Tag: "environment", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "prod"},
		{Resource: bucketLegacy, Tag: "cost-center", Status: types.StatusPass, Reason: types.ReasonCompliant, Actual: "LEGACY-001"},
		{Resource: bucketLegacy, Tag: "owner", Status: types.StatusFailed, Reason: types.ReasonInvalidFormat, Actual: "john", Expected: "^.+@company\\.com$"},
	}

	return result, nil
}

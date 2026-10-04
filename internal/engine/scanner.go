// Package engine contains the core business logic for tagctl.
package engine

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sync"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// RealScanner discovers cloud resources and evaluates their tag compliance.
type RealScanner struct {
	providers []provider.Provider
	evaluator *Evaluator
	ignore    config.IgnoreConfig
	onlyTypes []string
}

// NewScanner creates a RealScanner with the given providers and policy.
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

// OnlyTypes restricts the scan to resource types matching any of the path.Match patterns.
func (s *RealScanner) OnlyTypes(patterns []string) error {
	if err := ValidateTypePatterns(patterns); err != nil {
		return err
	}
	s.onlyTypes = patterns
	return nil
}

// ValidateTypePatterns rejects resource type patterns that are empty or that path.Match cannot parse.
func ValidateTypePatterns(patterns []string) error {
	for _, pattern := range patterns {
		if pattern == "" {
			return errors.New("empty resource type pattern")
		}
		if _, err := path.Match(pattern, ""); err != nil {
			return fmt.Errorf("invalid resource type pattern %q: %w", pattern, err)
		}
	}
	return nil
}

// Scan discovers and evaluates resources; provider errors mark the result partial and are returned joined.
func (s *RealScanner) Scan(ctx context.Context) (*types.ScanResult, error) {
	log.Info("Scanner: Starting scan with %d provider(s)", len(s.providers))

	var allResources []types.Resource
	var mu sync.Mutex
	var wg sync.WaitGroup
	// One slot per provider keeps the joined error in provider order.
	errs := make([]error, len(s.providers))

	for i, p := range s.providers {
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
				errs[i] = fmt.Errorf("provider %s: %w", p.Name(), err)
			}
		}()
	}

	wg.Wait()

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("scan cancelled: %w", err)
	}

	log.Info("Scanner: Collected %d total resources from all providers", len(allResources))

	allResources = s.filterTypes(allResources)

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

	discoveryErr := errors.Join(errs...)
	if discoveryErr != nil {
		result.Partial = true
		for _, err := range errs {
			if err != nil {
				result.Errors = append(result.Errors, err.Error())
			}
		}
		log.Error("Scanner: Scan is partial, %d provider(s) failed", len(result.Errors))
	}

	return result, discoveryErr
}

// filterTypes keeps the resources whose type matches an OnlyTypes pattern.
func (s *RealScanner) filterTypes(resources []types.Resource) []types.Resource {
	if len(s.onlyTypes) == 0 {
		return resources
	}

	kept := make([]types.Resource, 0, len(resources))
	for _, r := range resources {
		if matchesAnyGlob(s.onlyTypes, r.Type) {
			kept = append(kept, r)
		}
	}
	log.Debug("Scanner: %d of %d resources match the resource type filter %v", len(kept), len(resources), s.onlyTypes)
	return kept
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
	if matchesAnyGlob(s.ignore.Resources, r.Type) {
		return true
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

// matchGlob matches a resource type against a path.Match pattern. Patterns
// are validated when the config loads, so a malformed one matches nothing.
func matchGlob(pattern, value string) bool {
	matched, err := path.Match(pattern, value)
	return err == nil && matched
}

// matchesAnyGlob reports whether value matches at least one pattern.
func matchesAnyGlob(patterns []string, value string) bool {
	for _, pattern := range patterns {
		if matchGlob(pattern, value) {
			return true
		}
	}
	return false
}

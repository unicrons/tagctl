// Package provider defines the interface for cloud resource providers.
package provider

import (
	"context"

	"github.com/unicrons/tagctl/internal/types"
)

// Provider is the interface that all cloud providers must implement.
type Provider interface {
	// Name returns the provider identifier (e.g., "aws", "kubernetes").
	Name() string

	// ListResources discovers and returns all taggable resources.
	ListResources(ctx context.Context) ([]types.Resource, error)

	// ApplyTags applies the given tags to a resource.
	ApplyTags(ctx context.Context, resourceID string, tags map[string]string) error
}

// ResourceFilter allows filtering resources during listing.
type ResourceFilter struct {
	// Types filters by resource type (e.g., "aws_instance", "k8s_pod").
	Types []string

	// Regions filters by region (AWS) or cluster (K8s).
	Regions []string

	// Accounts filters by account/profile.
	Accounts []string

	// Tags filters by existing tags (key=value).
	Tags map[string]string
}

// ProviderConfig contains common configuration for providers.
type ProviderConfig struct {
	// DryRun simulates operations without making changes.
	DryRun bool

	// Verbose enables detailed logging.
	Verbose bool

	// MaxRetries is the number of retries for API calls.
	MaxRetries int

	// Timeout is the timeout for API calls in seconds.
	Timeout int
}

// DefaultProviderConfig returns a ProviderConfig with sensible defaults.
func DefaultProviderConfig() ProviderConfig {
	return ProviderConfig{
		DryRun:     false,
		Verbose:    false,
		MaxRetries: 3,
		Timeout:    30,
	}
}

// Error types for provider operations.
type ProviderError struct {
	Provider   string
	Operation  string
	ResourceID string
	Err        error
}

func (e *ProviderError) Error() string {
	if e.ResourceID != "" {
		return e.Provider + ": " + e.Operation + " failed for " + e.ResourceID + ": " + e.Err.Error()
	}
	return e.Provider + ": " + e.Operation + " failed: " + e.Err.Error()
}

func (e *ProviderError) Unwrap() error {
	return e.Err
}

// NewProviderError creates a new ProviderError.
func NewProviderError(provider, operation, resourceID string, err error) *ProviderError {
	return &ProviderError{
		Provider:   provider,
		Operation:  operation,
		ResourceID: resourceID,
		Err:        err,
	}
}

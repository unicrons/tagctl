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

	// ApplyTags applies the given tags to the resource with that ID.
	ApplyTags(ctx context.Context, resourceID string, tags map[string]string) error
}

// ResourceTagger is implemented by providers that address a resource by more
// than its ID, such as an ARN or the region it lives in.
type ResourceTagger interface {
	// TagResource applies the given tags to a resource.
	TagResource(ctx context.Context, resource types.Resource, tags map[string]string) error
}

// TagRemover is implemented by providers that can delete tags from a resource.
type TagRemover interface {
	// RemoveTags deletes the given tag keys from a resource. A key the
	// resource does not carry is not an error.
	RemoveTags(ctx context.Context, resource types.Resource, keys []string) error
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

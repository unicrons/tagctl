// Package types contains the core domain types for tagctl.
package types

import (
	"strings"
	"time"
)

// Resource represents a cloud resource with its tags.
type Resource struct {
	// ID is the cloud-specific resource identifier.
	ID string `json:"id"`

	// ARN is the Amazon Resource Name (AWS-specific, optional).
	ARN string `json:"arn,omitempty"`

	// Type is the resource type (e.g., "aws_instance", "aws_s3_bucket").
	Type string `json:"type"`

	// Name is the human-readable name of the resource.
	Name string `json:"name"`

	// Region is the cloud region where the resource exists.
	Region string `json:"region"`

	// Account is the cloud account/project identifier.
	Account string `json:"account"`

	// Provider is the cloud provider (aws, gcp, azure).
	Provider string `json:"provider"`

	// Tags contains the current tags/labels on the resource.
	Tags map[string]string `json:"tags"`

	// UnknownTags lists the tag keys whose values are only known once the
	// resource exists, as in a Terraform plan; their values cannot be checked.
	UnknownTags []string `json:"unknown_tags,omitempty"`

	// CreatedAt is when the resource was created (if available).
	CreatedAt *time.Time `json:"created_at,omitempty"`

	// Parents maps a relation (see ParentRelations) to the Identity of the
	// resource this one hangs from, as recorded by the provider.
	Parents map[string]string `json:"parents,omitempty"`
}

// Relations a provider can record in Resource.Parents.
const (
	RelationAttachedInstance = "attached_instance"
	RelationSourceVolume     = "source_volume"
	RelationVPC              = "vpc"
)

// ParentRelations are every relation rules.inherit accepts in from.
var ParentRelations = []string{RelationAttachedInstance, RelationSourceVolume, RelationVPC}

// HasTag checks if the resource has a specific tag.
func (r *Resource) HasTag(key string) bool {
	_, exists := r.Tags[key]
	return exists
}

// GetTag returns the value of a tag, or empty string if not present.
func (r *Resource) GetTag(key string) string {
	return r.Tags[key]
}

// Identity returns a key that is stable across scans and unique across
// regions: the ARN when there is one, otherwise provider/account/region/id
// with empty segments dropped. Every place that counts, groups or compares
// resources keys by this, never by ID alone, because AWS repeats names such as
// log groups across regions.
func (r *Resource) Identity() string {
	if r.ARN != "" {
		return r.ARN
	}

	parts := make([]string, 0, 4)
	for _, part := range []string{r.Provider, r.Account, r.Region, r.ID} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "/")
}

// DisplayName returns a human-readable identifier for the resource.
func (r *Resource) DisplayName() string {
	if r.Name != "" {
		return r.Name
	}
	return r.ID
}

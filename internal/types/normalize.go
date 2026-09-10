package types

// NormalizeResult reports tag values that look like variants of one another.
type NormalizeResult struct {
	// Clusters are the groups of values that mean the same thing, one entry
	// per tag and concept.
	Clusters []ValueCluster `json:"clusters"`

	// ResourcesScanned is how many resources were examined.
	ResourcesScanned int `json:"resources_scanned"`

	// TagsScanned is how many distinct tag keys were examined.
	TagsScanned int `json:"tags_scanned"`
}

// IsEmpty reports whether no value drift was found.
func (r *NormalizeResult) IsEmpty() bool {
	return len(r.Clusters) == 0
}

// AffectedResources is the number of resources that would change.
func (r *NormalizeResult) AffectedResources() int {
	total := 0
	for _, cluster := range r.Clusters {
		for _, variant := range cluster.Variants {
			total += variant.Count
		}
	}
	return total
}

// ValueCluster is a set of tag values that mean the same thing, with the one
// value the others should collapse into.
type ValueCluster struct {
	// Tag is the tag key these values belong to.
	Tag string `json:"tag"`

	// Canonical is the value the variants should become.
	Canonical string `json:"canonical"`

	// CanonicalCount is how many resources already use the canonical value.
	CanonicalCount int `json:"canonical_count"`

	// CanonicalFromPolicy reports whether the canonical value was chosen
	// because the policy allows it, rather than by frequency.
	CanonicalFromPolicy bool `json:"canonical_from_policy"`

	// Variants are the values that should be rewritten, most common first.
	Variants []ValueVariant `json:"variants"`
}

// TotalResources is how many resources carry any value in this cluster.
func (c *ValueCluster) TotalResources() int {
	total := c.CanonicalCount
	for _, variant := range c.Variants {
		total += variant.Count
	}
	return total
}

// ValueVariant is a single non-canonical spelling of a value.
type ValueVariant struct {
	// Value is the spelling found on the resources.
	Value string `json:"value"`

	// Count is how many resources carry it.
	Count int `json:"count"`

	// Match explains why this value was grouped with the canonical one.
	Match MatchKind `json:"match"`

	// Resources are the resources carrying this value.
	Resources []Resource `json:"resources,omitempty"`
}

// MatchKind describes why two values were considered the same concept.
type MatchKind string

const (
	// MatchCasing means the values differ only by case, separators or spacing.
	MatchCasing MatchKind = "casing"

	// MatchTypo means the values are within the configured edit distance.
	MatchTypo MatchKind = "typo"

	// MatchAbbreviation means one value is a prefix of the other.
	MatchAbbreviation MatchKind = "abbreviation"
)

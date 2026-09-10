package engine

import (
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/unicrons/tagctl/internal/types"
)

// defaultIgnoredTags are tags whose values are legitimately unique per
// resource. Collapsing them would be nonsense: every Name tag is supposed to
// differ, and clustering them would propose renaming half the estate.
var defaultIgnoredTags = []string{"Name", "name"}

// NormalizeOptions controls how aggressively values are grouped.
type NormalizeOptions struct {
	// MaxDistance is the edit distance under which two values are treated as
	// a typo of each other. 0 disables typo matching.
	MaxDistance int

	// MatchAbbreviations groups a value with a longer one it is a prefix of,
	// which is what catches "prod" against "production".
	MatchAbbreviations bool

	// IgnoreTags are tag keys to leave alone, in addition to the defaults.
	IgnoreTags []string

	// AllowedValues maps a tag to the values the policy permits. A value the
	// policy already allows is never rewritten, and is preferred as the
	// canonical spelling.
	AllowedValues map[string][]string
}

// DefaultNormalizeOptions is the configuration used when none is given:
// single-character typos and abbreviations, which covers the common failure
// modes without inventing relationships between unrelated values.
func DefaultNormalizeOptions() NormalizeOptions {
	return NormalizeOptions{
		MaxDistance:        1,
		MatchAbbreviations: true,
	}
}

// minAbbreviationLength is the shortest prefix treated as an abbreviation.
// Below this, unrelated values collide far too easily.
const minAbbreviationLength = 3

// Normalizer finds tag values that are variants of one another.
type Normalizer struct {
	opts    NormalizeOptions
	ignored map[string]bool
	allowed map[string]map[string]bool
}

// NewNormalizer creates a Normalizer with the given options.
func NewNormalizer(opts NormalizeOptions) *Normalizer {
	ignored := make(map[string]bool)
	for _, tag := range defaultIgnoredTags {
		ignored[tag] = true
	}
	for _, tag := range opts.IgnoreTags {
		ignored[tag] = true
	}

	allowed := make(map[string]map[string]bool, len(opts.AllowedValues))
	for tag, values := range opts.AllowedValues {
		set := make(map[string]bool, len(values))
		for _, value := range values {
			set[value] = true
		}
		allowed[tag] = set
	}

	return &Normalizer{opts: opts, ignored: ignored, allowed: allowed}
}

// Normalize groups the tag values across resources into clusters of values
// that mean the same thing.
//
// Values are grouped by three signals, in order of confidence: they differ
// only in case, separators or spacing; they are within the configured edit
// distance of each other; or one is an abbreviation of the other. A cluster is
// only reported when it actually contains more than one spelling.
func (n *Normalizer) Normalize(resources []types.Resource) *types.NormalizeResult {
	byTag := n.collectValues(resources)

	result := &types.NormalizeResult{
		ResourcesScanned: len(resources),
		TagsScanned:      len(byTag),
	}

	tags := make([]string, 0, len(byTag))
	for tag := range byTag {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	for _, tag := range tags {
		result.Clusters = append(result.Clusters, n.clustersFor(tag, byTag[tag])...)
	}

	return result
}

// valueUsage records every resource carrying one spelling of a value.
type valueUsage struct {
	value     string
	resources []types.Resource
}

// collectValues indexes, per tag, every distinct value and who carries it.
func (n *Normalizer) collectValues(resources []types.Resource) map[string]map[string]*valueUsage {
	byTag := make(map[string]map[string]*valueUsage)

	for _, resource := range resources {
		for tag, value := range resource.Tags {
			if n.ignored[tag] || strings.TrimSpace(value) == "" {
				continue
			}

			if byTag[tag] == nil {
				byTag[tag] = make(map[string]*valueUsage)
			}
			if byTag[tag][value] == nil {
				byTag[tag][value] = &valueUsage{value: value}
			}
			byTag[tag][value].resources = append(byTag[tag][value].resources, resource)
		}
	}

	return byTag
}

// clustersFor groups one tag's values and turns each group into a cluster.
func (n *Normalizer) clustersFor(tag string, usages map[string]*valueUsage) []types.ValueCluster {
	values := make([]string, 0, len(usages))
	for value := range usages {
		values = append(values, value)
	}
	sort.Strings(values)

	groups := n.groupValues(tag, values)

	clusters := make([]types.ValueCluster, 0, len(groups))
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		if cluster, ok := n.buildCluster(tag, group, usages); ok {
			clusters = append(clusters, cluster)
		}
	}

	// Report the widest drift first: that is where the reporting damage is.
	sort.Slice(clusters, func(i, j int) bool {
		if clusters[i].TotalResources() != clusters[j].TotalResources() {
			return clusters[i].TotalResources() > clusters[j].TotalResources()
		}
		return clusters[i].Canonical < clusters[j].Canonical
	})

	return clusters
}

// groupValues partitions a tag's values into groups of the same concept,
// using union-find over every pair that matches.
func (n *Normalizer) groupValues(tag string, values []string) [][]string {
	parent := make([]int, len(values))
	for i := range parent {
		parent[i] = i
	}

	// Path-halving find; no recursion, so no forward declaration is needed.
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	union := func(a, b int) {
		rootA, rootB := find(a), find(b)
		if rootA != rootB {
			parent[rootB] = rootA
		}
	}

	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if _, ok := n.match(tag, values[i], values[j]); ok {
				union(i, j)
			}
		}
	}

	byRoot := make(map[int][]string)
	for i, value := range values {
		root := find(i)
		byRoot[root] = append(byRoot[root], value)
	}

	groups := make([][]string, 0, len(byRoot))
	for _, group := range byRoot {
		sort.Strings(group)
		groups = append(groups, group)
	}

	return groups
}

// match reports whether two values mean the same thing, and why.
//
// Two values the policy both allows are never matched: "dev" and "devops" can
// legitimately coexist, and proposing to collapse them would be wrong.
func (n *Normalizer) match(tag, a, b string) (types.MatchKind, bool) {
	if n.isAllowed(tag, a) && n.isAllowed(tag, b) {
		return "", false
	}

	normA, normB := normalizeValue(a), normalizeValue(b)
	if normA == "" || normB == "" {
		return "", false
	}

	if normA == normB {
		return types.MatchCasing, true
	}

	if n.opts.MaxDistance > 0 && editDistance(normA, normB) <= n.opts.MaxDistance {
		return types.MatchTypo, true
	}

	if n.opts.MatchAbbreviations && isAbbreviation(normA, normB) {
		return types.MatchAbbreviation, true
	}

	return "", false
}

// buildCluster turns a group of equivalent values into a cluster, choosing the
// canonical spelling. It reports false when there is nothing to change.
func (n *Normalizer) buildCluster(tag string, group []string, usages map[string]*valueUsage) (types.ValueCluster, bool) {
	canonical, fromPolicy := n.pickCanonical(tag, group, usages)

	cluster := types.ValueCluster{
		Tag:                 tag,
		Canonical:           canonical,
		CanonicalCount:      len(usages[canonical].resources),
		CanonicalFromPolicy: fromPolicy,
	}

	for _, value := range group {
		if value == canonical {
			continue
		}
		kind, ok := n.match(tag, canonical, value)
		if !ok {
			// Grouped transitively rather than directly; report it as the
			// weakest signal rather than claiming a match that was not made.
			kind = types.MatchAbbreviation
		}
		cluster.Variants = append(cluster.Variants, types.ValueVariant{
			Value:     value,
			Count:     len(usages[value].resources),
			Match:     kind,
			Resources: usages[value].resources,
		})
	}

	if len(cluster.Variants) == 0 {
		return types.ValueCluster{}, false
	}

	sort.Slice(cluster.Variants, func(i, j int) bool {
		if cluster.Variants[i].Count != cluster.Variants[j].Count {
			return cluster.Variants[i].Count > cluster.Variants[j].Count
		}
		return cluster.Variants[i].Value < cluster.Variants[j].Value
	})

	return cluster, true
}

// pickCanonical chooses the spelling the others should collapse into: a value
// the policy allows if there is one, otherwise the most common spelling.
func (n *Normalizer) pickCanonical(tag string, group []string, usages map[string]*valueUsage) (string, bool) {
	allowed := make([]string, 0, len(group))
	for _, value := range group {
		if n.isAllowed(tag, value) {
			allowed = append(allowed, value)
		}
	}

	candidates, fromPolicy := group, false
	if len(allowed) > 0 {
		candidates, fromPolicy = allowed, true
	}

	best := candidates[0]
	for _, value := range candidates[1:] {
		if preferCanonical(value, best, len(usages[value].resources), len(usages[best].resources)) {
			best = value
		}
	}

	return best, fromPolicy
}

// preferCanonical reports whether candidate should displace best as the
// canonical spelling. The most common spelling wins; ties are broken
// case-insensitively and then in favour of the plainer, lower-case form, so a
// tie never leaves a SHOUTED value as the canonical one by accident.
func preferCanonical(candidate, best string, candidateCount, bestCount int) bool {
	if candidateCount != bestCount {
		return candidateCount > bestCount
	}

	lowerCandidate, lowerBest := strings.ToLower(candidate), strings.ToLower(best)
	if lowerCandidate != lowerBest {
		return lowerCandidate < lowerBest
	}

	return upperCount(candidate) < upperCount(best)
}

// upperCount is how many upper-case letters a value carries.
func upperCount(value string) int {
	count := 0
	for _, r := range value {
		if unicode.IsUpper(r) {
			count++
		}
	}
	return count
}

// isAllowed reports whether the policy permits this value for this tag.
func (n *Normalizer) isAllowed(tag, value string) bool {
	values, ok := n.allowed[tag]
	if !ok {
		return false
	}
	return values[value]
}

// NormalizePlan turns a normalize result into a remediation plan, so the
// existing apply path does the writing.
func NormalizePlan(result *types.NormalizeResult) *types.Plan {
	plan := &types.Plan{
		ID:        "normalize-" + time.Now().UTC().Format("20060102-150405"),
		CreatedAt: time.Now().UTC(),
	}

	resources := make(map[string]bool)

	for _, cluster := range result.Clusters {
		for _, variant := range cluster.Variants {
			for _, resource := range variant.Resources {
				plan.Changes = append(plan.Changes, types.TagChange{
					Resource: resource,
					Tag:      cluster.Tag,
					Action:   types.ActionUpdate,
					OldValue: variant.Value,
					NewValue: cluster.Canonical,
					Reason:   types.ReasonManual,
					Source:   "normalize: " + string(variant.Match),
				})
				resources[resource.Identity()] = true
			}
		}
	}

	plan.Summary = types.PlanSummary{
		TotalResources: len(resources),
		TotalChanges:   len(plan.Changes),
		TagsUpdated:    len(plan.Changes),
	}

	return plan
}

// normalizeValue reduces a value to its comparable form: lowercase, with
// separators and spacing removed, so "Prod-1", "prod_1" and "PROD 1" all
// collapse to the same thing.
func normalizeValue(value string) string {
	var b strings.Builder
	b.Grow(len(value))

	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}

	return b.String()
}

// isAbbreviation reports whether one value is a prefix of the other and long
// enough for that to mean something.
func isAbbreviation(a, b string) bool {
	shorter, longer := a, b
	if len(shorter) > len(longer) {
		shorter, longer = longer, shorter
	}

	if len(shorter) < minAbbreviationLength || shorter == longer {
		return false
	}

	return strings.HasPrefix(longer, shorter)
}

// editDistance is the Levenshtein distance between two strings.
func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return len([]rune(b))
	}
	if b == "" {
		return len([]rune(a))
	}

	ra, rb := []rune(a), []rune(b)

	// Only the previous row is needed at any point.
	previous := make([]int, len(rb)+1)
	current := make([]int, len(rb)+1)

	for j := range previous {
		previous[j] = j
	}

	for i := 1; i <= len(ra); i++ {
		current[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			current[j] = min3(
				current[j-1]+1,     // insertion
				previous[j]+1,      // deletion
				previous[j-1]+cost, // substitution
			)
		}
		previous, current = current, previous
	}

	return previous[len(rb)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

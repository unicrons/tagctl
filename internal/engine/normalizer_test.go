package engine

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/unicrons/tagctl/internal/types"
)

// taggedResources builds one resource per value, so counts are predictable.
// Each entry is a value and how many resources carry it.
func taggedResources(tag string, valueCounts map[string]int) []types.Resource {
	var resources []types.Resource
	id := 0

	// Sort by iterating a stable list built from the map keys.
	values := make([]string, 0, len(valueCounts))
	for value := range valueCounts {
		values = append(values, value)
	}

	for _, value := range values {
		for i := 0; i < valueCounts[value]; i++ {
			id++
			resources = append(resources, types.Resource{
				ID:       "i-" + string(rune('a'+id%26)) + string(rune('0'+id/26)),
				Type:     "aws_instance",
				Account:  "111",
				Provider: "aws",
				Tags:     map[string]string{tag: value},
			})
		}
	}

	return resources
}

// findCluster returns the cluster for a tag, or nil.
func findCluster(result *types.NormalizeResult, tag string) *types.ValueCluster {
	for i := range result.Clusters {
		if result.Clusters[i].Tag == tag {
			return &result.Clusters[i]
		}
	}
	return nil
}

// variantValues lists the variant spellings of a cluster.
func variantValues(cluster *types.ValueCluster) []string {
	values := make([]string, 0, len(cluster.Variants))
	for _, variant := range cluster.Variants {
		values = append(values, variant.Value)
	}
	return values
}

// The headline case: prod, Production, PROD and production coexisting.
func TestNormalize_CollapsesEnvironmentSpellings(t *testing.T) {
	resources := taggedResources(tagEnvironment, map[string]int{
		valueProd:       10,
		valueProduction: 3,
		"PROD":          2,
		"production":    1,
	})

	result := NewNormalizer(DefaultNormalizeOptions()).Normalize(resources)

	cluster := findCluster(result, tagEnvironment)
	if cluster == nil {
		t.Fatalf("no cluster for environment; got %+v", result.Clusters)
	}

	// valueProd is the most common spelling, so it wins.
	if cluster.Canonical != valueProd {
		t.Errorf("canonical = %q, want prod (the most common)", cluster.Canonical)
	}
	if cluster.CanonicalCount != 10 {
		t.Errorf("CanonicalCount = %d, want 10", cluster.CanonicalCount)
	}
	if len(cluster.Variants) != 3 {
		t.Fatalf("got %d variants (%v), want 3", len(cluster.Variants), variantValues(cluster))
	}
	if cluster.TotalResources() != 16 {
		t.Errorf("TotalResources() = %d, want 16", cluster.TotalResources())
	}

	// Variants are ordered by how many resources carry them.
	if cluster.Variants[0].Value != valueProduction {
		t.Errorf("first variant = %q, want Production (3 resources)", cluster.Variants[0].Value)
	}
}

// Case, separators and spacing are the same value.
func TestNormalize_MatchesCasingAndSeparators(t *testing.T) {
	resources := taggedResources("cost-center", map[string]int{
		"team-platform": 5,
		"Team_Platform": 2,
		"team platform": 1,
	})

	result := NewNormalizer(DefaultNormalizeOptions()).Normalize(resources)

	cluster := findCluster(result, "cost-center")
	if cluster == nil {
		t.Fatal("no cluster for cost-center")
	}
	if cluster.Canonical != "team-platform" {
		t.Errorf("canonical = %q, want team-platform", cluster.Canonical)
	}
	for _, variant := range cluster.Variants {
		if variant.Match != types.MatchCasing {
			t.Errorf("variant %q matched as %q, want casing", variant.Value, variant.Match)
		}
	}
}

func TestNormalize_MatchesTypos(t *testing.T) {
	resources := taggedResources(tagEnvironment, map[string]int{
		"staging": 8,
		"stagign": 1,
	})

	result := NewNormalizer(NormalizeOptions{MaxDistance: 2}).Normalize(resources)

	cluster := findCluster(result, tagEnvironment)
	if cluster == nil {
		t.Fatal("no cluster for a two-character transposition")
	}
	if cluster.Canonical != "staging" {
		t.Errorf("canonical = %q, want staging", cluster.Canonical)
	}
	if cluster.Variants[0].Match != types.MatchTypo {
		t.Errorf("match kind = %q, want typo", cluster.Variants[0].Match)
	}
}

// With typo matching off, a misspelling is left alone.
func TestNormalize_TypoMatchingDisabled(t *testing.T) {
	resources := taggedResources(tagEnvironment, map[string]int{
		"staging": 8,
		"stagng":  1,
	})

	result := NewNormalizer(NormalizeOptions{MaxDistance: 0}).Normalize(resources)

	if !result.IsEmpty() {
		t.Errorf("found clusters with typo matching disabled: %+v", result.Clusters)
	}
}

func TestNormalize_MatchesAbbreviations(t *testing.T) {
	resources := taggedResources(tagEnvironment, map[string]int{
		"production": 6,
		valueProd:    2,
	})

	result := NewNormalizer(NormalizeOptions{MatchAbbreviations: true}).Normalize(resources)

	cluster := findCluster(result, tagEnvironment)
	if cluster == nil {
		t.Fatal("prod was not matched against production")
	}
	if cluster.Canonical != "production" {
		t.Errorf("canonical = %q, want production", cluster.Canonical)
	}
	if cluster.Variants[0].Match != types.MatchAbbreviation {
		t.Errorf("match kind = %q, want abbreviation", cluster.Variants[0].Match)
	}
}

// A short prefix would collide with unrelated values, so it is not treated as
// an abbreviation.
func TestNormalize_ShortPrefixesAreNotAbbreviations(t *testing.T) {
	resources := taggedResources("team", map[string]int{
		"ab":      3,
		"abcdefg": 3,
	})

	result := NewNormalizer(NormalizeOptions{MatchAbbreviations: true}).Normalize(resources)

	if !result.IsEmpty() {
		t.Errorf("two-character prefix was treated as an abbreviation: %+v", result.Clusters)
	}
}

// The whole safety property: two values the policy allows are never collapsed
// into each other, even though one is a prefix of the other.
func TestNormalize_NeverCollapsesTwoAllowedValues(t *testing.T) {
	resources := taggedResources("team", map[string]int{
		"dev":    5,
		"devops": 3,
	})

	normalizer := NewNormalizer(NormalizeOptions{
		MatchAbbreviations: true,
		MaxDistance:        2,
		AllowedValues:      map[string][]string{"team": {"dev", "devops"}},
	})

	result := normalizer.Normalize(resources)

	if !result.IsEmpty() {
		t.Errorf("collapsed two policy-allowed values: %+v", result.Clusters)
	}
}

// Without the policy anchoring them, the same two values do get grouped, which
// is why the allowed-values check matters.
func TestNormalize_GroupsSameValuesWithoutPolicy(t *testing.T) {
	resources := taggedResources("team", map[string]int{
		"dev":    5,
		"devops": 3,
	})

	result := NewNormalizer(NormalizeOptions{MatchAbbreviations: true}).Normalize(resources)

	if result.IsEmpty() {
		t.Error("expected dev and devops to group when the policy does not allow both")
	}
}

// The policy's spelling wins over the more common one.
func TestNormalize_PrefersPolicyValueAsCanonical(t *testing.T) {
	resources := taggedResources(tagEnvironment, map[string]int{
		valueProduction: 20, // most common, but not the policy spelling
		"production":    2,  // what the policy allows
	})

	normalizer := NewNormalizer(NormalizeOptions{
		AllowedValues: map[string][]string{tagEnvironment: {"production"}},
	})

	cluster := findCluster(normalizer.Normalize(resources), tagEnvironment)
	if cluster == nil {
		t.Fatal("no cluster for environment")
	}
	if cluster.Canonical != "production" {
		t.Errorf("canonical = %q, want production (the policy value)", cluster.Canonical)
	}
	if !cluster.CanonicalFromPolicy {
		t.Error("CanonicalFromPolicy = false, want true")
	}
	if cluster.Variants[0].Value != valueProduction {
		t.Errorf("variant = %q, want Production", cluster.Variants[0].Value)
	}
}

// Name is unique per resource; clustering it would propose renaming the estate.
func TestNormalize_IgnoresNameTagByDefault(t *testing.T) {
	resources := taggedResources("Name", map[string]int{
		"web-server-1": 1,
		"web-server-2": 1,
		"web-server-3": 1,
	})

	result := NewNormalizer(DefaultNormalizeOptions()).Normalize(resources)

	if !result.IsEmpty() {
		t.Errorf("clustered the Name tag: %+v", result.Clusters)
	}
}

func TestNormalize_IgnoresConfiguredTags(t *testing.T) {
	resources := taggedResources("build-id", map[string]int{
		"build-abc": 1,
		"build-abd": 1,
	})

	result := NewNormalizer(NormalizeOptions{
		MaxDistance: 2,
		IgnoreTags:  []string{"build-id"},
	}).Normalize(resources)

	if !result.IsEmpty() {
		t.Errorf("clustered an ignored tag: %+v", result.Clusters)
	}
}

func TestNormalize_ConsistentValuesProduceNoClusters(t *testing.T) {
	resources := taggedResources(tagEnvironment, map[string]int{valueProd: 25})

	result := NewNormalizer(DefaultNormalizeOptions()).Normalize(resources)

	if !result.IsEmpty() {
		t.Errorf("found drift in a consistent estate: %+v", result.Clusters)
	}
	if result.ResourcesScanned != 25 {
		t.Errorf("ResourcesScanned = %d, want 25", result.ResourcesScanned)
	}
	if result.TagsScanned != 1 {
		t.Errorf("TagsScanned = %d, want 1", result.TagsScanned)
	}
}

func TestNormalize_UnrelatedValuesAreNotGrouped(t *testing.T) {
	resources := taggedResources(tagEnvironment, map[string]int{
		"production": 5,
		"staging":    5,
		"sandbox":    5,
	})

	result := NewNormalizer(DefaultNormalizeOptions()).Normalize(resources)

	if !result.IsEmpty() {
		t.Errorf("grouped unrelated values: %+v", result.Clusters)
	}
}

func TestNormalize_EmptyValuesAreSkipped(t *testing.T) {
	resources := []types.Resource{
		{ID: "i-1", Provider: "aws", Account: "111", Tags: map[string]string{tagEnvironment: valueProd}},
		{ID: "i-2", Provider: "aws", Account: "111", Tags: map[string]string{tagEnvironment: ""}},
		{ID: "i-3", Provider: "aws", Account: "111", Tags: map[string]string{tagEnvironment: "   "}},
	}

	result := NewNormalizer(DefaultNormalizeOptions()).Normalize(resources)

	if !result.IsEmpty() {
		t.Errorf("clustered empty values: %+v", result.Clusters)
	}
}

func TestNormalize_TypoGuards(t *testing.T) {
	tests := []struct {
		name     string
		distance int
		values   map[string]int
		want     bool
	}{
		{"two-letter codes", 1, map[string]int{"us": 3, "uk": 2}, false},
		{"short versions", 1, map[string]int{"v1": 3, "v2": 2}, false},
		{"three letters each", 1, map[string]int{"api": 3, "app": 1}, false},
		{"three letters each, last differs", 1, map[string]int{"dev": 3, "dew": 1}, false},
		{"letter dropped from four", 1, map[string]int{valueProd: 3, "prd": 1}, true},
		{"four letters", 1, map[string]int{valueProd: 3, "prud": 1}, true},
		{"region numbers", 1, map[string]int{"us-east-1": 3, "us-east-2": 2}, false},
		{"build numbers", 1, map[string]int{"build-100": 3, "build-101": 2}, false},
		{"same digits", 1, map[string]int{"backend-1": 3, "backnd-1": 1}, true},
		{"length gap over the distance", 1, map[string]int{"platform": 3, "platfo": 1}, false},
		{"two edits in four letters", 2, map[string]int{"test": 3, "temp": 1}, false},
		{"two edits in four letters, swapped", 2, map[string]int{"east": 3, "west": 1}, false},
		{"two edits from three letters", 2, map[string]int{"dev": 3, "demo": 1}, false},
		{"two edits in seven letters", 2, map[string]int{"staging": 3, "stagign": 1}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewNormalizer(NormalizeOptions{MaxDistance: tt.distance}).Normalize(taggedResources("team", tt.values))

			if got := !result.IsEmpty(); got != tt.want {
				t.Fatalf("clustered = %v, want %v: %+v", got, tt.want, result.Clusters)
			}
			if tt.want && result.Clusters[0].Variants[0].Match != types.MatchTypo {
				t.Errorf("match kind = %q, want typo", result.Clusters[0].Variants[0].Match)
			}
		})
	}
}

func TestNormalize_AbbreviationsNeedSameDigits(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]int
		want   bool
	}{
		{"region number prefix", map[string]int{"us-east-1": 3, "us-east-10": 1}, false},
		{"build number prefix", map[string]int{"build-10": 3, "build-100": 1}, false},
		{"number added to a word", map[string]int{valueProd: 3, "prod2": 1}, false},
		{"plain prefix", map[string]int{"production": 3, valueProd: 1}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewNormalizer(NormalizeOptions{MatchAbbreviations: true}).Normalize(taggedResources(tagEnvironment, tt.values))

			if got := !result.IsEmpty(); got != tt.want {
				t.Fatalf("clustered = %v, want %v: %+v", got, tt.want, result.Clusters)
			}
			if tt.want && result.Clusters[0].Variants[0].Match != types.MatchAbbreviation {
				t.Errorf("match kind = %q, want abbreviation", result.Clusters[0].Variants[0].Match)
			}
		})
	}
}

// stagn is one edit from stagng but two from the canonical staging.
func transitiveResources() []types.Resource {
	return taggedResources(tagEnvironment, map[string]int{
		"staging": 5,
		"stagng":  2,
		"stagn":   1,
	})
}

func TestNormalize_LabelsIndirectMembersTransitive(t *testing.T) {
	cluster := findCluster(NewNormalizer(NormalizeOptions{MaxDistance: 1}).Normalize(transitiveResources()), tagEnvironment)
	if cluster == nil {
		t.Fatal("no cluster for environment")
	}

	want := map[string]types.MatchKind{"stagng": types.MatchTypo, "stagn": types.MatchTransitive}
	if len(cluster.Variants) != len(want) {
		t.Fatalf("variants = %v, want stagng and stagn", variantValues(cluster))
	}
	for _, variant := range cluster.Variants {
		if variant.Match != want[variant.Value] {
			t.Errorf("variant %q matched as %q, want %q", variant.Value, variant.Match, want[variant.Value])
		}
	}
}

func TestNormalize_AffectedResources(t *testing.T) {
	resources := taggedResources(tagEnvironment, map[string]int{
		valueProd:       10,
		valueProduction: 3,
		"PROD":          2,
	})

	result := NewNormalizer(DefaultNormalizeOptions()).Normalize(resources)

	// The 10 already-canonical resources are not affected; the other 5 are.
	if got := result.AffectedResources(); got != 5 {
		t.Errorf("AffectedResources() = %d, want 5", got)
	}
}

// --- Plan generation ---

func TestNormalizePlan(t *testing.T) {
	resources := taggedResources(tagEnvironment, map[string]int{
		valueProd:       4,
		valueProduction: 2,
	})

	result := NewNormalizer(DefaultNormalizeOptions()).Normalize(resources)
	plan := NormalizePlan(result)

	if plan.IsEmpty() {
		t.Fatal("plan has no changes")
	}
	if len(plan.Changes) != 2 {
		t.Fatalf("got %d changes, want 2 (one per non-canonical resource)", len(plan.Changes))
	}

	for _, change := range plan.Changes {
		if change.Action != types.ActionUpdate {
			t.Errorf("action = %q, want update", change.Action)
		}
		if change.Tag != tagEnvironment {
			t.Errorf("tag = %q, want environment", change.Tag)
		}
		if change.OldValue != valueProduction {
			t.Errorf("old value = %q, want Production", change.OldValue)
		}
		if change.NewValue != valueProd {
			t.Errorf("new value = %q, want prod", change.NewValue)
		}
		if change.Source == "" {
			t.Error("change has no source explaining why it was proposed")
		}
	}

	if plan.Summary.TotalChanges != 2 || plan.Summary.TagsUpdated != 2 {
		t.Errorf("summary = %+v, want 2 changes / 2 updates", plan.Summary)
	}
	if plan.Summary.TotalResources != 2 {
		t.Errorf("summary TotalResources = %d, want 2", plan.Summary.TotalResources)
	}
}

func TestNormalizePlan_SkipsTransitiveVariants(t *testing.T) {
	result := NewNormalizer(NormalizeOptions{MaxDistance: 1}).Normalize(transitiveResources())
	plan := NormalizePlan(result)

	if len(plan.Changes) != 2 {
		t.Fatalf("got %d changes, want 2 (the stagng resources only)", len(plan.Changes))
	}
	for _, change := range plan.Changes {
		if change.OldValue != "stagng" {
			t.Errorf("plan rewrites %q, want only stagng", change.OldValue)
		}
	}
	if got := result.AffectedResources(); got != len(plan.Changes) {
		t.Errorf("AffectedResources() = %d, want %d to match the plan", got, len(plan.Changes))
	}
}

func TestNormalizePlan_EmptyResult(t *testing.T) {
	plan := NormalizePlan(&types.NormalizeResult{})

	if !plan.IsEmpty() {
		t.Errorf("empty result produced %d changes", len(plan.Changes))
	}
	if plan.ID == "" {
		t.Error("plan has no ID")
	}
}

// --- Helpers ---

func TestNormalizeValue(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Prod", valueProd},
		{"PROD-1", "prod1"},
		{"prod_1", "prod1"},
		{"prod 1", "prod1"},
		{"  Prod  ", valueProd},
		{"team@example.com", "teamexamplecom"},
		{"---", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := normalizeValue(tt.in); got != tt.want {
				t.Errorf("normalizeValue(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestEditDistance(t *testing.T) {
	long := strings.Repeat("a", 200)

	tests := []struct {
		a, b  string
		limit int
		want  int
	}{
		{"", "", 0, 0},
		{valueProd, valueProd, 1, 0},
		{valueProd, "prd", 1, 1},
		{"staging", "stagign", 2, 2},
		{"staging", "stagign", 1, 2},
		{"kitten", "sitting", 3, 3},
		{"kitten", "sitting", 2, 3},
		{"kitten", "sitting", 0, 1},
		{"", "abc", 3, 3},
		{"abc", "", 1, 2},
		{"production", valueProd, 6, 6},
		{"production", valueProd, 5, 6},
		{"café", "cafe", 1, 1},
		{long, long[1:] + "b", 2, 1},
		{long, "b" + long[2:] + "c", 2, 2},
		{long, long + "bb", 1, 2},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%.12s/%.12s/%d", tt.a, tt.b, tt.limit), func(t *testing.T) {
			if got := editDistance([]rune(tt.a), []rune(tt.b), tt.limit); got != tt.want {
				t.Errorf("editDistance(%q, %q, %d) = %d, want %d", tt.a, tt.b, tt.limit, got, tt.want)
			}
			if got := editDistance([]rune(tt.b), []rune(tt.a), tt.limit); got != tt.want {
				t.Errorf("editDistance(%q, %q, %d) = %d, want %d (not symmetric)", tt.b, tt.a, tt.limit, got, tt.want)
			}
		})
	}
}

// levenshtein is the unbounded textbook distance editDistance is checked against.
func levenshtein(a, b []rune) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(current[j-1]+1, previous[j]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

func FuzzEditDistance(f *testing.F) {
	f.Add("staging", "stagign", uint8(1))
	f.Add("kitten", "sitting", uint8(3))
	f.Add("café", "cafe", uint8(0))
	f.Add("", "abc", uint8(2))

	f.Fuzz(func(t *testing.T, a, b string, limit uint8) {
		ra, rb := []rune(a), []rune(b)
		if len(ra) > 300 || len(rb) > 300 {
			t.Skip()
		}
		k := int(limit % 8)

		want := min(levenshtein(ra, rb), k+1)
		if got := editDistance(ra, rb, k); got != want {
			t.Fatalf("editDistance(%q, %q, %d) = %d, want %d", a, b, k, got, want)
		}
		if got := editDistance(rb, ra, k); got != want {
			t.Fatalf("editDistance(%q, %q, %d) = %d, want %d (not symmetric)", b, a, k, got, want)
		}
	})
}

func TestDigitsOf(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"useast1", "1"},
		{"build100", "100"},
		{"1v2", "12"},
		{"staging", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := digitsOf(tt.in); got != tt.want {
				t.Errorf("digitsOf(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsAbbreviation(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{valueProd, "production", true},
		{"production", valueProd, true},
		{"dev", "development", true},
		{"ab", "abcdef", false},       // too short to be meaningful
		{valueProd, valueProd, false}, // identical is not an abbreviation
		{valueProd, "staging", false},
		{"", "production", false},
	}

	for _, tt := range tests {
		t.Run(tt.a+"/"+tt.b, func(t *testing.T) {
			if got := isAbbreviation(tt.a, tt.b); got != tt.want {
				t.Errorf("isAbbreviation(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// Output order must not depend on map iteration order.
func TestNormalize_StableOrdering(t *testing.T) {
	resources := taggedResources(tagEnvironment, map[string]int{
		valueProd: 5, "Prod": 4, "PROD": 3,
	})
	resources = append(resources, taggedResources("team", map[string]int{
		"platform": 2, "Platform": 1,
	})...)

	normalizer := NewNormalizer(DefaultNormalizeOptions())

	first := normalizer.Normalize(resources)
	for i := 0; i < 5; i++ {
		again := normalizer.Normalize(resources)
		if len(again.Clusters) != len(first.Clusters) {
			t.Fatalf("run %d produced %d clusters, first run produced %d",
				i, len(again.Clusters), len(first.Clusters))
		}
		for j := range first.Clusters {
			if again.Clusters[j].Tag != first.Clusters[j].Tag ||
				again.Clusters[j].Canonical != first.Clusters[j].Canonical {
				t.Fatalf("run %d cluster %d = %s/%s, first run = %s/%s",
					i, j, again.Clusters[j].Tag, again.Clusters[j].Canonical,
					first.Clusters[j].Tag, first.Clusters[j].Canonical)
			}
		}
	}
}

// A tie must not leave a shouted value as the canonical one by accident.
func TestNormalize_TieBreakPrefersPlainerSpelling(t *testing.T) {
	resources := taggedResources(tagEnvironment, map[string]int{
		"PROD":    1,
		valueProd: 1,
	})

	cluster := findCluster(NewNormalizer(DefaultNormalizeOptions()).Normalize(resources), tagEnvironment)
	if cluster == nil {
		t.Fatal("no cluster for environment")
	}
	if cluster.Canonical != valueProd {
		t.Errorf("canonical = %q, want prod (lower case wins a tie)", cluster.Canonical)
	}
}

func TestPreferCanonical(t *testing.T) {
	tests := []struct {
		name                      string
		candidate, best           string
		candidateCount, bestCount int
		want                      bool
	}{
		{"more common wins", valueProduction, valueProd, 10, 3, true},
		{"less common loses", valueProduction, valueProd, 2, 9, false},
		{"tie prefers alphabetically earlier", "alpha", "beta", 5, 5, true},
		{"tie prefers lower case", valueProd, "PROD", 5, 5, true},
		{"tie keeps lower case", "PROD", valueProd, 5, 5, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := preferCanonical(tt.candidate, tt.best, tt.candidateCount, tt.bestCount)
			if got != tt.want {
				t.Errorf("preferCanonical(%q, %q, %d, %d) = %v, want %v",
					tt.candidate, tt.best, tt.candidateCount, tt.bestCount, got, tt.want)
			}
		})
	}
}

// highCardinalityResources gives each resource its own random lower-case value
// of 6 to 14 letters, the shape of a tag nobody added to ignore_tags.
func highCardinalityResources(count int) []types.Resource {
	rng := rand.New(rand.NewPCG(1, 2))
	resources := make([]types.Resource, count)

	for i := range resources {
		value := make([]byte, 6+rng.IntN(9))
		for j := range value {
			value[j] = byte('a' + rng.IntN(26))
		}
		resources[i] = types.Resource{
			ID:       "i-" + string(value),
			Provider: "aws",
			Account:  "111",
			Tags:     map[string]string{"cost-center": string(value)},
		}
	}

	return resources
}

func BenchmarkNormalize_HighCardinality(b *testing.B) {
	resources := highCardinalityResources(2000)
	normalizer := NewNormalizer(DefaultNormalizeOptions())

	b.ReportAllocs()
	for b.Loop() {
		normalizer.Normalize(resources)
	}
}

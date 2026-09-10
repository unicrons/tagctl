package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/engine"
	"github.com/unicrons/tagctl/internal/types"
)

func normalizeCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{Use: "normalize", RunE: func(*cobra.Command, []string) error { return nil }}
	cmd.Flags().Int("max-distance", -1, "")
	cmd.Flags().Bool("abbreviations", true, "")
	cmd.Flags().StringSlice("ignore-tag", nil, "")
	cmd.SetArgs(args)
	cmd.SetOut(os.NewFile(0, os.DevNull))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("parsing %v: %v", args, err)
	}
	return cmd
}

// A resource appears once per tag checked, so the scan must be de-duplicated.
func TestResourcesFromScan_Deduplicates(t *testing.T) {
	resource := types.Resource{
		ID: "i-1", Type: "aws_instance", Account: "111", Provider: "aws",
		Tags: map[string]string{"environment": "prod"},
	}
	other := types.Resource{
		ID: "i-2", Type: "aws_instance", Account: "111", Provider: "aws",
		Tags: map[string]string{"environment": "Prod"},
	}

	scan := &types.ScanResult{
		Findings: []types.Finding{
			{Resource: resource, Tag: "environment", Status: types.StatusPass},
			{Resource: resource, Tag: "owner", Status: types.StatusFailed},
			{Resource: other, Tag: "environment", Status: types.StatusPass},
		},
	}

	got := resourcesFromScan(scan)

	if len(got) != 2 {
		t.Fatalf("got %d resources, want 2 (i-1 appears in two findings)", len(got))
	}
	if got[0].ID != "i-1" || got[1].ID != "i-2" {
		t.Errorf("resources = %s, %s; want i-1, i-2", got[0].ID, got[1].ID)
	}
}

// The same resource ID in two accounts is two resources.
func TestResourcesFromScan_SeparatesAccounts(t *testing.T) {
	scan := &types.ScanResult{
		Findings: []types.Finding{
			{Resource: types.Resource{ID: "i-1", Account: "111", Provider: "aws"}, Tag: "owner"},
			{Resource: types.Resource{ID: "i-1", Account: "222", Provider: "aws"}, Tag: "owner"},
		},
	}

	if got := resourcesFromScan(scan); len(got) != 2 {
		t.Errorf("got %d resources, want 2", len(got))
	}
}

func TestResourcesFromScan_ReadsLegacyViolations(t *testing.T) {
	scan := &types.ScanResult{
		Violations: []types.Violation{
			{Resource: types.Resource{ID: "i-1", Account: "111", Provider: "aws"}, Tag: "owner"},
		},
	}

	if got := resourcesFromScan(scan); len(got) != 1 {
		t.Errorf("got %d resources from a legacy scan, want 1", len(got))
	}
}

func TestLoadNormalizeResources_RejectsBothSources(t *testing.T) {
	if _, _, err := loadNormalizeResources("scan.json", "resources.json"); err == nil {
		t.Error("passing both --scan and --resources returned nil error")
	}
}

func TestLoadNormalizeResources_FromResourcesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "resources.json")

	resources := []types.Resource{
		{ID: "i-1", Type: "aws_instance", Account: "111", Provider: "aws",
			Tags: map[string]string{"environment": "prod"}},
	}
	data, err := json.Marshal(resources)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if writeErr := os.WriteFile(path, data, 0o600); writeErr != nil {
		t.Fatalf("write: %v", writeErr)
	}

	got, source, err := loadNormalizeResources("", path)
	if err != nil {
		t.Fatalf("loadNormalizeResources() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != "i-1" {
		t.Errorf("resources = %+v, want one i-1", got)
	}
	if source != path {
		t.Errorf("source = %q, want %q", source, path)
	}
}

func TestLoadNormalizeResources_FromScanFile(t *testing.T) {
	dir := t.TempDir()
	scan := &types.ScanResult{
		Findings: []types.Finding{
			{
				Resource: types.Resource{ID: "i-1", Account: "111", Provider: "aws",
					Tags: map[string]string{"environment": "prod"}},
				Tag: "environment", Status: types.StatusPass,
			},
		},
	}
	path := writeScan(t, dir, "scan-20260201-100000.json", scan)

	got, source, err := loadNormalizeResources(path, "")
	if err != nil {
		t.Fatalf("loadNormalizeResources() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d resources, want 1", len(got))
	}
	if source != path {
		t.Errorf("source = %q, want %q", source, path)
	}
}

func TestNormalizeOptionsFrom_FlagOverrides(t *testing.T) {
	opts, err := normalizeOptionsFrom(normalizeCommand(t,
		"--max-distance", "3",
		"--abbreviations=false",
		"--ignore-tag", "build-id",
		"--ignore-tag", "commit",
	))
	if err != nil {
		t.Fatalf("normalizeOptionsFrom() error = %v", err)
	}

	if opts.MaxDistance != 3 {
		t.Errorf("MaxDistance = %d, want 3", opts.MaxDistance)
	}
	if opts.MatchAbbreviations {
		t.Error("MatchAbbreviations = true, want false")
	}
	if len(opts.IgnoreTags) != 2 {
		t.Errorf("IgnoreTags = %v, want two entries", opts.IgnoreTags)
	}
}

func TestNormalizeOptionsFrom_RejectsNegativeDistance(t *testing.T) {
	if _, err := normalizeOptionsFrom(normalizeCommand(t, "--max-distance", "-2")); err == nil {
		t.Error("a negative --max-distance returned nil error")
	}
}

// Untouched flags leave the defaults in place.
func TestNormalizeOptionsFrom_Defaults(t *testing.T) {
	opts, err := normalizeOptionsFrom(normalizeCommand(t))
	if err != nil {
		t.Fatalf("normalizeOptionsFrom() error = %v", err)
	}

	want := engine.DefaultNormalizeOptions()
	if opts.MaxDistance != want.MaxDistance {
		t.Errorf("MaxDistance = %d, want the default %d", opts.MaxDistance, want.MaxDistance)
	}
	if opts.MatchAbbreviations != want.MatchAbbreviations {
		t.Errorf("MatchAbbreviations = %v, want the default %v", opts.MatchAbbreviations, want.MatchAbbreviations)
	}
}

func TestApplyNormalizeConfig(t *testing.T) {
	distance := 4
	abbreviations := false

	cfg := &config.Config{
		Normalize: config.NormalizeConfig{
			MaxDistance:        &distance,
			MatchAbbreviations: &abbreviations,
			IgnoreTags:         []string{"build-id"},
		},
		Policy: config.PolicyConfig{
			Required: []config.TagRequirement{
				{Name: "environment", Values: []string{"production", "staging"}},
				{Name: "owner", Pattern: "^.+@.+$"}, // no Values, so no anchoring
			},
			Optional: []config.TagRequirement{
				{Name: "tier", Values: []string{"gold", "silver"}},
			},
		},
	}

	opts := engine.DefaultNormalizeOptions()
	applyNormalizeConfig(&opts, cfg)

	if opts.MaxDistance != 4 {
		t.Errorf("MaxDistance = %d, want 4 from config", opts.MaxDistance)
	}
	if opts.MatchAbbreviations {
		t.Error("MatchAbbreviations = true, want false from config")
	}
	if len(opts.IgnoreTags) != 1 || opts.IgnoreTags[0] != "build-id" {
		t.Errorf("IgnoreTags = %v, want [build-id]", opts.IgnoreTags)
	}

	// Required and optional tags with allowed values both anchor the canonical.
	if len(opts.AllowedValues) != 2 {
		t.Fatalf("AllowedValues = %v, want environment and tier", opts.AllowedValues)
	}
	if len(opts.AllowedValues["environment"]) != 2 {
		t.Errorf("environment values = %v, want two", opts.AllowedValues["environment"])
	}
	if _, ok := opts.AllowedValues["owner"]; ok {
		t.Error("a pattern-only tag should not contribute allowed values")
	}
}

func TestWriteNormalizePlan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")

	resources := []types.Resource{
		{ID: "i-1", Type: "aws_instance", Account: "111", Provider: "aws",
			Tags: map[string]string{"environment": "prod"}},
		{ID: "i-2", Type: "aws_instance", Account: "111", Provider: "aws",
			Tags: map[string]string{"environment": "PROD"}},
	}
	result := engine.NewNormalizer(engine.DefaultNormalizeOptions()).Normalize(resources)

	if err := writeNormalizePlan(result, path); err != nil {
		t.Fatalf("writeNormalizePlan() error = %v", err)
	}

	data, err := os.ReadFile(path) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var plan types.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		t.Fatalf("plan is not valid JSON: %v", err)
	}
	if len(plan.Changes) != 1 {
		t.Fatalf("got %d changes, want 1", len(plan.Changes))
	}
	if plan.Changes[0].Action != types.ActionUpdate {
		t.Errorf("action = %q, want update", plan.Changes[0].Action)
	}
	if plan.Changes[0].NewValue != valueProd {
		t.Errorf("new value = %q, want prod", plan.Changes[0].NewValue)
	}
}

func TestPluralResources(t *testing.T) {
	if got := pluralResources(1); got != "resource" {
		t.Errorf("pluralResources(1) = %q, want resource", got)
	}
	for _, n := range []int{0, 2, 17} {
		if got := pluralResources(n); got != "resources" {
			t.Errorf("pluralResources(%d) = %q, want resources", n, got)
		}
	}
}

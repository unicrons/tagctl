package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/engine"
	"github.com/unicrons/tagctl/internal/types"
)

var normalizeCmd = &cobra.Command{
	Use:   "normalize",
	Short: "Find tag values that are variants of one another",
	Long: `Normalize finds tag values that mean the same thing but are spelled
differently, and proposes one canonical spelling for each group.

"prod", "Production", "PROD" and "production" are four different strings to
every cost report that groups by that tag, so one inconsistency across a large
fleet silently splits your spend across four buckets. This is the failure mode
that a missing-tag check never catches.

Values are grouped by three signals:
  • casing        — they differ only in case, separators or spacing
  • typo          — they are within the configured edit distance, with at most
                    one edit per 3 letters or digits of the shorter value
                    ("prod" / "prd")
  • abbreviation  — one is a prefix of the other ("prod" / "production")

Values with different digits never match, so "us-east-1", "us-east-2" and
"us-east-10" stay apart. A value that joined a group through another variant,
without matching the canonical spelling itself, is reported as "transitive" and
left out of the plan.

A value your policy already allows is never rewritten, and is preferred as the
canonical spelling. Two values the policy both allows are never collapsed into
each other, so "dev" and "devops" are left alone.

The Name tag is always skipped: its values are unique by design.

Examples:
  # Report drift in the most recent scan
  tagctl normalize

  # Report drift in a specific scan or resource file
  tagctl normalize --scan output/scan-20260201-120000.json
  tagctl normalize --resources resources.json

  # Write a remediation plan, then apply it with 'tagctl apply'
  tagctl normalize --out output/normalize-plan.json

  # Only collapse exact case and separator differences
  tagctl normalize --max-distance 0 --abbreviations=false

  # Fail a pipeline when drift appears
  tagctl normalize --fail-on-drift`,
	RunE: runNormalize,
}

func init() {
	normalizeCmd.Flags().String("scan", "", "scan file to read resources from (default: the most recent scan)")
	normalizeCmd.Flags().String("resources", "", "JSON file with resources, as accepted by 'tagctl evaluate'")
	normalizeCmd.Flags().Int("max-distance", -1, "edit distance for typo matching, 0 disables it (default: from config, else 1)")
	normalizeCmd.Flags().Bool("abbreviations", true, "group a value with a longer one it is a prefix of")
	normalizeCmd.Flags().StringSlice("ignore-tag", nil, "tag key to leave alone (repeatable)")
	normalizeCmd.Flags().String("out", "", "write a remediation plan to this path")
	normalizeCmd.Flags().Bool("fail-on-drift", false, "exit 1 if any drift is found")
}

func runNormalize(cmd *cobra.Command, args []string) error {
	scanPath, _ := cmd.Flags().GetString("scan")
	resourcesPath, _ := cmd.Flags().GetString("resources")
	outPath, _ := cmd.Flags().GetString("out")
	failOnDrift, _ := cmd.Flags().GetBool("fail-on-drift")

	resources, source, err := loadNormalizeResources(scanPath, resourcesPath)
	if err != nil {
		return err
	}

	opts, err := normalizeOptionsFrom(cmd)
	if err != nil {
		return err
	}

	result := engine.NewNormalizer(opts).Normalize(resources)

	if strings.ToLower(outputFormat) == formatJSON {
		if err := printJSON(result); err != nil {
			return err
		}
	} else {
		printNormalizeTable(result, source)
	}

	if outPath != "" {
		if err := writeNormalizePlan(result, outPath); err != nil {
			return err
		}
	}

	if failOnDrift && !result.IsEmpty() {
		return gateFailed(fmt.Errorf("%d tag value cluster(s) need normalizing, affecting %d resource(s)",
			len(result.Clusters), result.AffectedResources()))
	}

	return nil
}

// loadNormalizeResources reads the resources to analyse, from an explicit
// resource file, a named scan, or the most recent scan on disk.
func loadNormalizeResources(scanPath, resourcesPath string) ([]types.Resource, string, error) {
	if resourcesPath != "" && scanPath != "" {
		return nil, "", fmt.Errorf("use --scan or --resources, not both")
	}

	if resourcesPath != "" {
		resources, err := loadResourcesFromJSON(resourcesPath)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load resources: %w", err)
		}
		return resources, resourcesPath, nil
	}

	if scanPath == "" {
		latest, err := findLatestScan()
		if err != nil {
			return nil, "", err
		}
		scanPath = latest
	}

	scan, err := LoadScanFile(scanPath)
	if err != nil {
		return nil, "", err
	}

	return resourcesFromScan(scan), scanPath, nil
}

// resourcesFromScan pulls the distinct resources out of a scan's findings.
// A resource appears once per tag checked, so duplicates are collapsed.
func resourcesFromScan(scan *types.ScanResult) []types.Resource {
	findings := scan.Findings
	if len(findings) == 0 && len(scan.Violations) > 0 {
		findings = types.ViolationsToFindings(scan.Violations)
	}

	seen := make(map[string]bool, len(findings))
	resources := make([]types.Resource, 0, len(findings))

	for _, finding := range findings {
		key := finding.Resource.Identity()
		if seen[key] {
			continue
		}
		seen[key] = true
		resources = append(resources, finding.Resource)
	}

	return resources
}

// normalizeOptionsFrom builds the options from config, with flags overriding.
func normalizeOptionsFrom(cmd *cobra.Command) (engine.NormalizeOptions, error) {
	opts := engine.DefaultNormalizeOptions()

	// The config is optional here: normalize works without a policy, it just
	// loses the allowed-value anchoring.
	cfg, err := loadConfig()
	if err == nil && cfg != nil {
		applyNormalizeConfig(&opts, cfg)
	}

	if cmd.Flags().Changed("max-distance") {
		distance, _ := cmd.Flags().GetInt("max-distance")
		if distance < 0 {
			return opts, fmt.Errorf("--max-distance must be 0 or greater")
		}
		opts.MaxDistance = distance
	}
	if cmd.Flags().Changed("abbreviations") {
		opts.MatchAbbreviations, _ = cmd.Flags().GetBool("abbreviations")
	}
	if ignore, _ := cmd.Flags().GetStringSlice("ignore-tag"); len(ignore) > 0 {
		opts.IgnoreTags = append(opts.IgnoreTags, ignore...)
	}

	return opts, nil
}

// applyNormalizeConfig folds the tagctl.yaml settings into the options.
func applyNormalizeConfig(opts *engine.NormalizeOptions, cfg *config.Config) {
	if cfg.Normalize.MaxDistance != nil {
		opts.MaxDistance = *cfg.Normalize.MaxDistance
	}
	if cfg.Normalize.MatchAbbreviations != nil {
		opts.MatchAbbreviations = *cfg.Normalize.MatchAbbreviations
	}
	opts.IgnoreTags = append(opts.IgnoreTags, cfg.Normalize.IgnoreTags...)

	// The policy's allowed values anchor the canonical spelling.
	allowed := make(map[string][]string)
	for _, req := range cfg.Policy.Required {
		if len(req.Values) > 0 {
			allowed[req.Name] = req.Values
		}
	}
	for _, req := range cfg.Policy.Optional {
		if len(req.Values) > 0 {
			allowed[req.Name] = req.Values
		}
	}
	if len(allowed) > 0 {
		opts.AllowedValues = allowed
	}
}

// writeNormalizePlan writes a remediation plan the apply command can consume.
func writeNormalizePlan(result *types.NormalizeResult, path string) error {
	plan := engine.NormalizePlan(result)

	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode plan: %w", err)
	}

	if err := os.WriteFile(filepath.Clean(path), data, 0o600); err != nil {
		return fmt.Errorf("failed to write plan file: %w", err)
	}

	fmt.Printf("\nPlan written to %s (%d change(s))\n", path, len(plan.Changes))
	fmt.Println("Review it, then run: tagctl apply --plan " + path)

	return nil
}

func printNormalizeTable(result *types.NormalizeResult, source string) {
	fmt.Printf("Source: %s\n", source)
	fmt.Printf("Scanned %d resources across %d tag(s)\n\n", result.ResourcesScanned, result.TagsScanned)

	if result.IsEmpty() {
		fmt.Println("No tag value drift found: every value is spelled consistently.")
		return
	}

	fmt.Printf("Found %d cluster(s) of inconsistent values, affecting %d resource(s):\n\n",
		len(result.Clusters), result.AffectedResources())

	for _, cluster := range result.Clusters {
		anchor := "most common"
		if cluster.CanonicalFromPolicy {
			anchor = "allowed by policy"
		}
		fmt.Printf("  %s → %q  (%s, %d %s)\n",
			cluster.Tag, cluster.Canonical, anchor, cluster.CanonicalCount, pluralResources(cluster.CanonicalCount))

		for _, variant := range cluster.Variants {
			fmt.Printf("      %-24q %3d %-9s [%s]\n",
				variant.Value, variant.Count, pluralResources(variant.Count), variant.Match)
		}
		fmt.Println()
	}

	writeNormalizeExamples(os.Stdout, result)

	fmt.Println("Run with --out <file> to write a plan, then 'tagctl apply' to fix them.")
}

// writeNormalizeExamples names a few affected resources, so the report can be
// sanity-checked without opening the scan file.
func writeNormalizeExamples(w io.Writer, result *types.NormalizeResult) {
	const maxExamples = 3

	for _, cluster := range result.Clusters {
		for _, variant := range cluster.Variants {
			if len(variant.Resources) == 0 {
				continue
			}

			ids := make([]string, 0, len(variant.Resources))
			for _, resource := range variant.Resources {
				ids = append(ids, resource.ID)
			}
			sort.Strings(ids)

			shown := ids
			suffix := ""
			if len(shown) > maxExamples {
				shown = shown[:maxExamples]
				suffix = fmt.Sprintf(" and %d more", len(ids)-maxExamples)
			}

			note := ""
			if variant.Match == types.MatchTransitive {
				note = " (transitive, not in the plan)"
			}

			fmt.Fprintf(w, "  %s=%q%s: %s%s\n", cluster.Tag, variant.Value, note, strings.Join(shown, ", "), suffix)
		}
	}

	fmt.Fprintf(w, "\n")
}

// pluralResources agrees the noun with the count.
func pluralResources(count int) string {
	if count == 1 {
		return "resource"
	}
	return "resources"
}

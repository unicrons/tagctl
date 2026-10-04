package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/engine"
	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/types"
)

var evaluateCmd = &cobra.Command{
	Use:   "evaluate",
	Short: "Evaluate resources from JSON input",
	Long: `Evaluate resources against the tag policy without cloud discovery.

This command is designed for integration with other tools like Prowler.
Resources are provided as JSON via file or stdin.

Examples:
  # Evaluate from file
  tagctl evaluate --resources resources.json

  # Evaluate from stdin (pipe)
  cat resources.json | tagctl evaluate

  # With specific policy file
  tagctl evaluate --resources resources.json --policy custom.yaml`,
	RunE: runEvaluate,
}

func init() {
	evaluateCmd.Flags().String("resources", "-", "JSON file with resources (- for stdin)")
	evaluateCmd.Flags().String("policy", "", "Path to tagctl.yaml (auto-discover if empty)")
	addGateFlags(evaluateCmd)
}

func runEvaluate(cmd *cobra.Command, args []string) error {
	resourcesPath, _ := cmd.Flags().GetString("resources")
	policyPath, _ := cmd.Flags().GetString("policy")
	gateOpts := readGateFlags(cmd)
	format, err := gateOpts.stdoutFormat(cmd, formatJSON)
	if err != nil {
		return err
	}

	// 1. Load resources from JSON
	resources, err := loadResourcesFromJSON(resourcesPath)
	if err != nil {
		return fmt.Errorf("failed to load resources: %w", err)
	}

	log.Debug("Evaluate: Loaded %d resources from %s", len(resources), resourcesPath)

	// 2. Load configuration/policy
	cfg, policyPath, err := loadConfigFromPath(policyPath)
	if err != nil {
		return fmt.Errorf("failed to load policy: %w", err)
	}

	// 3. Create evaluator and evaluate
	evaluator, err := engine.NewEvaluator(cfg.Policy)
	if err != nil {
		return fmt.Errorf("failed to create evaluator: %w", err)
	}

	result := evaluator.EvaluateResources(resources)

	// 4. Build output with simplified resource info
	findings := make([]EvaluateFinding, 0, len(result.Findings))
	for _, f := range result.Findings {
		findings = append(findings, EvaluateFinding{
			Resource: EvaluateResource{
				ID:   f.Resource.ID,
				ARN:  f.Resource.ARN,
				Type: f.Resource.Type,
				Name: f.Resource.Name,
				Tags: f.Resource.Tags,
			},
			Tag:      f.Tag,
			Status:   string(f.Status),
			Reason:   string(f.Reason),
			Expected: f.Expected,
			Actual:   f.Actual,
		})
	}

	output := EvaluateOutput{
		Findings: findings,
		Summary: EvaluateSummary{
			TotalResources: result.TotalResources,
			TotalFindings:  len(result.Findings),
			Passed:         result.CompliantCount,
			Failed:         result.ViolationCount,
		},
	}

	// 5. Output JSON, unless a report was sent to stdout instead
	if format == formatJSON {
		if encodeErr := printJSON(output); encodeErr != nil {
			return encodeErr
		}
	}

	// 6. CI reports and gate. Reports come first so a failing build still
	// uploads its findings.
	if reportErr := gateOpts.writeReports(result, policyPath); reportErr != nil {
		return reportErr
	}

	return gateOpts.check(result)
}

// loadResourcesFromJSON loads resources from a JSON file or stdin.
func loadResourcesFromJSON(path string) ([]types.Resource, error) {
	var reader io.Reader

	if path == "-" || path == "" {
		// Check if stdin has data
		stat, err := os.Stdin.Stat()
		if err != nil {
			return nil, fmt.Errorf("failed to stat stdin: %w", err)
		}
		if (stat.Mode() & os.ModeCharDevice) != 0 {
			return nil, fmt.Errorf("no input: provide --resources file or pipe JSON via stdin")
		}
		reader = os.Stdin
	} else {
		// #nosec G304 -- the input path is supplied by the user running the CLI.
		f, err := os.Open(filepath.Clean(path))
		if err != nil {
			return nil, fmt.Errorf("failed to open file %s: %w", path, err)
		}
		defer f.Close()
		reader = f
	}

	var resources []types.Resource
	if err := json.NewDecoder(reader).Decode(&resources); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return resources, nil
}

// loadConfigFromPath loads and validates the policy at path, or the
// auto-discovered config file when path is empty, and returns the path read.
func loadConfigFromPath(path string) (*config.Config, string, error) {
	if path == "" {
		path = viper.ConfigFileUsed()
	}
	if path == "" {
		return nil, "", fmt.Errorf("no policy file found. Specify --policy or run from a directory with tagctl.yaml")
	}

	log.Debug("Evaluate: Loading policy from %s", path)
	cfg, err := config.Load(path)
	if err != nil {
		return nil, "", err
	}

	// Validate that the policy has at least some tags defined
	if len(cfg.Policy.Required) == 0 && len(cfg.Policy.Optional) == 0 {
		return nil, "", fmt.Errorf("policy has no required or optional tags defined")
	}

	return cfg, path, nil
}

// EvaluateOutput is the JSON output format for the evaluate command.
type EvaluateOutput struct {
	Findings []EvaluateFinding `json:"findings"`
	Summary  EvaluateSummary   `json:"summary"`
}

// EvaluateFinding represents a single compliance finding in the output.
type EvaluateFinding struct {
	Resource EvaluateResource `json:"resource"`
	Tag      string           `json:"tag"`
	Status   string           `json:"status"`
	Reason   string           `json:"reason"`
	Expected string           `json:"expected,omitempty"`
	Actual   string           `json:"actual,omitempty"`
}

// EvaluateResource is a simplified resource representation for output.
type EvaluateResource struct {
	ID   string            `json:"id"`
	ARN  string            `json:"arn,omitempty"`
	Type string            `json:"type"`
	Name string            `json:"name"`
	Tags map[string]string `json:"tags,omitempty"`
}

// EvaluateSummary contains aggregate statistics for the evaluation.
type EvaluateSummary struct {
	TotalResources int `json:"total_resources"`
	TotalFindings  int `json:"total_findings"`
	Passed         int `json:"passed"`
	Failed         int `json:"failed"`
}

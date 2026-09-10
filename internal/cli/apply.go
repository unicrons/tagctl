package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/engine"
	"github.com/unicrons/tagctl/internal/types"
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply planned tag changes",
	Long: `Apply executes the changes from a previously generated plan.

Before applying, it will:
  • Load the plan file
  • Show a summary of changes
  • Ask for confirmation (unless --auto-approve is set)

Examples:
  # Apply the latest plan
  tagctl apply

  # Apply a specific plan file
  tagctl apply --plan output/plan-20240201-143052.json

  # Apply without confirmation
  tagctl apply --auto-approve

  # Apply with a different profile or an assumed role
  tagctl apply --profile production
  tagctl apply --role arn:aws:iam::123456789012:role/TagWriter`,
	RunE: runApply,
}

func init() {
	applyCmd.Flags().String("plan", "", "plan file to apply (default: latest in output/)")
	applyCmd.Flags().Bool("auto-approve", false, "skip confirmation prompt")
	applyCmd.Flags().Bool("mock", false, "use mock applier for demonstration")
	addAWSAuthFlags(applyCmd)
}

func runApply(cmd *cobra.Command, args []string) error {
	planFile, _ := cmd.Flags().GetString("plan")
	autoApprove, _ := cmd.Flags().GetBool("auto-approve")
	useMock, _ := cmd.Flags().GetBool("mock")

	ctx := context.Background()

	printBanner()

	// Find plan file
	if planFile == "" {
		var err error
		planFile, err = findLatestPlan()
		if err != nil {
			return err
		}
	}

	// Load plan with spinner
	spinner := NewSpinner("Loading plan...")
	spinner.Start()

	plan, err := loadPlan(planFile)
	if err != nil {
		spinner.Fail("Failed to load plan")
		return fmt.Errorf("failed to load plan: %w", err)
	}
	spinner.Success(fmt.Sprintf("Loaded plan with %d changes", len(plan.Changes)))

	if plan.IsEmpty() {
		fmt.Println("Plan has no changes to apply.")
		return nil
	}

	printPlanSummary(planFile, plan)

	if !autoApprove {
		confirmed, confirmErr := confirmApply()
		if confirmErr != nil {
			return confirmErr
		}
		if !confirmed {
			fmt.Println("Apply cancelled.")
			return nil
		}
	}

	fmt.Println()
	fmt.Println("Applying changes...")
	fmt.Println()

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if err = applyAWSAuthFlags(cfg, readAWSAuthFlags(cmd)); err != nil {
		return err
	}

	// A mock apply is used when asked for, or when no provider is configured.
	simulated := useMock || !hasConfiguredProviders(cfg)

	applier, err := buildApplier(ctx, cfg, plan, simulated)
	if err != nil {
		return err
	}

	result, err := applier.Apply(ctx, plan)
	if err != nil {
		return fmt.Errorf("apply failed: %w", err)
	}

	// The mock applier has no callback, so its progress is printed afterwards.
	if simulated {
		for i, change := range plan.Changes {
			fmt.Printf("  [%d/%d] %s.%s (%s: %s) ✓\n",
				i+1, len(plan.Changes),
				change.Resource.Type, change.Resource.ID,
				change.Tag, change.NewValue)
		}
	}

	printApplyResult(result)
	if result.ErrorCount > 0 {
		return fmt.Errorf("%d of %d changes failed", result.ErrorCount, result.TotalChanges)
	}
	return nil
}

// printPlanSummary describes the plan about to be applied.
func printPlanSummary(planFile string, plan *types.Plan) {
	fmt.Printf("Applying plan from %s\n", planFile)
	fmt.Printf("Plan created at: %s\n", plan.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Println()
	fmt.Printf("Changes to apply:\n")
	fmt.Printf("  • %d resources will be modified\n", plan.Summary.TotalResources)
	fmt.Printf("  • %d tags will be added\n", plan.Summary.TagsAdded)
	fmt.Printf("  • %d tags will be updated\n", plan.Summary.TagsUpdated)
	fmt.Printf("  • %d tags will be removed\n", plan.Summary.TagsRemoved)
	fmt.Println()
}

// confirmApply asks the operator to confirm before any tag is written.
func confirmApply() (bool, error) {
	fmt.Print("Do you want to apply these changes? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read response: %w", err)
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes", nil
}

// buildApplier returns the applier to run the plan with: a mock one when
// simulating, otherwise a real applier wired to the configured providers.
func buildApplier(ctx context.Context, cfg *config.Config, plan *types.Plan, simulated bool) (engine.Applier, error) {
	if simulated {
		return engine.NewMockApplier(), nil
	}

	spinner := NewSpinner("Initializing cloud providers...")
	spinner.Start()

	providers, err := initProviders(ctx, cfg, nil)
	if err != nil {
		spinner.Fail("Failed to initialize providers")
		return nil, err
	}
	spinner.Success(fmt.Sprintf("Initialized %d provider(s)", len(providers)))

	applier := engine.NewApplier(providers)

	// Report each change as it is applied.
	changeIndex := 0
	applier.SetCallback(func(change types.TagChange, success bool, err error) {
		changeIndex++
		status := "✓"
		if !success {
			status = "✗"
		}
		fmt.Printf("  [%d/%d] %s.%s (%s: %s) %s\n",
			changeIndex, len(plan.Changes),
			change.Resource.Type, change.Resource.ID,
			change.Tag, change.NewValue, status)
		if err != nil {
			fmt.Printf("         Error: %v\n", err)
		}
	})

	return applier, nil
}

// printApplyResult prints the totals and any failures from an apply run.
func printApplyResult(result *engine.ApplyResult) {
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Applied successfully: %d changes\n", result.SuccessCount)
	fmt.Printf("Errors: %d\n", result.ErrorCount)
	fmt.Printf("Duration: %s\n", result.Duration.Round(time.Millisecond))

	if result.ErrorCount > 0 {
		fmt.Println()
		fmt.Println("Failed changes:")
		for _, e := range result.Errors {
			fmt.Printf("  • %s.%s: %s - %s\n",
				e.Change.Resource.Type, e.Change.Resource.ID,
				e.Change.Tag, e.Error)
		}
	}

	fmt.Println()
	fmt.Println("Run 'tagctl scan' to verify compliance.")
}

func findLatestPlan() (string, error) {
	return FindLatestPlanInDir()
}

func loadPlan(path string) (*types.Plan, error) {
	// #nosec G304 -- the plan path is supplied by the user running the CLI.
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	var plan types.Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, err
	}

	return &plan, nil
}

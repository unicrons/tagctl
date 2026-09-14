package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
  • Load the plan file and reject any change other than add or update
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

	if _, err := outputFormatFor(cmd, formatTable); err != nil {
		return err
	}

	ctx, stop := signalContext()
	defer stop()

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
		confirmed, confirmErr := confirmApply(ctx, os.Stdin)
		if confirmErr != nil {
			return confirmErr
		}
		if !confirmed {
			fmt.Fprintln(os.Stderr, "Apply cancelled.")
			return nil
		}
	}

	fmt.Fprint(os.Stderr, "\nApplying changes...\n\n")

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

// printPlanSummary describes the plan about to be applied on stderr.
func printPlanSummary(planFile string, plan *types.Plan) {
	summary := plan.Summarize()
	fmt.Fprintf(os.Stderr, "Applying plan from %s\n", planFile)
	fmt.Fprintf(os.Stderr, "Plan created at: %s\n\n", plan.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(os.Stderr, "Changes to apply:\n")
	fmt.Fprintf(os.Stderr, "  • %d resources will be modified\n", summary.TotalResources)
	fmt.Fprintf(os.Stderr, "  • %d tags will be added\n", summary.TagsAdded)
	fmt.Fprintf(os.Stderr, "  • %d tags will be updated\n\n", summary.TagsUpdated)
}

// confirmApply asks the operator to confirm before any tag is written. Ctrl-C
// at the prompt cancels ctx, which returns at once instead of waiting for Enter.
func confirmApply(ctx context.Context, in io.Reader) (bool, error) {
	fmt.Fprint(os.Stderr, "Do you want to apply these changes? [y/N]: ")

	type answer struct {
		line string
		err  error
	}
	answers := make(chan answer, 1)
	go func() {
		line, err := bufio.NewReader(in).ReadString('\n')
		answers <- answer{line: line, err: err}
	}()

	select {
	case <-ctx.Done():
		return false, fmt.Errorf("apply cancelled: %w", ctx.Err())
	case got := <-answers:
		if got.err != nil {
			return false, fmt.Errorf("failed to read response: %w", got.err)
		}
		response := strings.TrimSpace(strings.ToLower(got.line))
		return response == "y" || response == "yes", nil
	}
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
	if err := engine.ValidatePlan(&plan); err != nil {
		return nil, err
	}

	return &plan, nil
}

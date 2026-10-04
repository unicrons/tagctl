package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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
  • Load the plan file and reject any change it cannot perform
  • Show a summary of changes, with every tag it will remove listed apart
  • Ask for confirmation (unless --auto-approve is set), or about each
    resource with --interactive

Tags are only removed with --allow-removals. Without it the additions and
updates are applied and every removal in the plan is skipped: a rename then
adds the new key and keeps the old one.

Examples:
  # Apply the latest plan
  tagctl apply

  # Apply a specific plan file
  tagctl apply --plan output/plan-20240201-143052.json

  # Apply the latest plan of another directory
  tagctl apply --output-dir reports

  # Apply without confirmation
  tagctl apply --auto-approve

  # Decide resource by resource
  tagctl apply --interactive

  # Also perform the removals of the plan (renamed and forbidden tags)
  tagctl apply --allow-removals

  # Apply with a different profile or an assumed role
  tagctl apply --profile production
  tagctl apply --role arn:aws:iam::123456789012:role/TagWriter`,
	RunE: runApply,
}

func init() {
	applyCmd.Flags().String("plan", "", "plan file to apply (default: latest in --output-dir)")
	addOutputDirFlag(applyCmd, "directory to look for the latest plan in")
	applyCmd.Flags().Bool("auto-approve", false, "skip confirmation prompt")
	applyCmd.Flags().BoolP("interactive", "i", false, "review the plan resource by resource and apply only the approved changes")
	applyCmd.Flags().Bool(allowRemovalsFlag, false, "remove the tags the plan removes (skipped by default)")
	applyCmd.Flags().Bool("mock", false, "use mock applier for demonstration")
	addAWSAuthFlags(applyCmd)
}

func runApply(cmd *cobra.Command, args []string) error {
	planFile, _ := cmd.Flags().GetString("plan")
	autoApprove, _ := cmd.Flags().GetBool("auto-approve")
	useMock, _ := cmd.Flags().GetBool("mock")
	interactive, _ := cmd.Flags().GetBool("interactive")
	allowRemovals, _ := cmd.Flags().GetBool(allowRemovalsFlag)

	if _, err := outputFormatFor(cmd, formatTable); err != nil {
		return err
	}
	if err := checkInteractive(interactive, autoApprove); err != nil {
		return err
	}

	outputDir, err := outputDirFor(cmd)
	if err != nil {
		return err
	}

	ctx, stop := signalContext()
	defer stop()

	printBanner()

	// Find plan file
	if planFile == "" {
		planFile, err = FindLatestPlanInDir(outputDir)
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

	printPlanSummary(os.Stderr, planFile, plan, allowRemovals)

	plan, skippedRemovals := withoutRemovals(plan, allowRemovals)
	if plan.IsEmpty() {
		printSkippedRemovals(os.Stdout, skippedRemovals)
		return nil
	}

	skipped := 0
	switch {
	case interactive:
		plan, skipped, err = approveInteractively(ctx, plan)
	case !autoApprove:
		plan, err = approveWhole(ctx, plan)
	}
	if err != nil {
		return err
	}
	if plan.IsEmpty() {
		return nil
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
			fmt.Printf("  [%d/%d] %s.%s (%s) ✓\n",
				i+1, len(plan.Changes),
				change.Resource.Type, change.Resource.ID,
				describeChange(change))
		}
	}

	printApplyResult(result, skipped)
	printSkippedRemovals(os.Stdout, skippedRemovals)
	if result.ErrorCount > 0 {
		return fmt.Errorf("%d of %d changes failed", result.ErrorCount, result.TotalChanges)
	}
	return nil
}

func checkInteractive(interactive, autoApprove bool) error {
	if !interactive {
		return nil
	}
	if autoApprove {
		return errors.New("--interactive and --auto-approve cannot be used together")
	}
	if !stdinIsTerminal() {
		return errors.New("--interactive needs a terminal on stdin; use --auto-approve to apply without prompts")
	}
	return nil
}

// approveInteractively returns the plan reduced to the changes approved in the
// review, empty when there are none, and how many changes were skipped.
func approveInteractively(ctx context.Context, plan *types.Plan) (*types.Plan, int, error) {
	review, err := reviewChanges(ctx, os.Stderr, applyInput, plan)
	if err != nil {
		return nil, 0, err
	}
	printReviewSummary(os.Stderr, review)

	approved := review.approvedPlan(plan)
	if err = engine.ValidatePlan(approved); err != nil {
		return nil, 0, err
	}
	if approved.IsEmpty() {
		fmt.Fprintln(os.Stderr, "No changes approved, nothing applied.")
	}
	return approved, review.skippedChanges(), nil
}

// approveWhole returns the plan when the operator confirms it and an empty
// one otherwise.
func approveWhole(ctx context.Context, plan *types.Plan) (*types.Plan, error) {
	confirmed, err := confirmApply(ctx, applyInput)
	if err != nil {
		return nil, err
	}
	if !confirmed {
		fmt.Fprintln(os.Stderr, "Apply cancelled.")
		return &types.Plan{}, nil
	}
	return plan, nil
}

const (
	allowRemovalsFlag = "allow-removals"

	// maxListedRemovals caps the removals spelled out before the prompt.
	maxListedRemovals = 20
)

// withoutRemovals returns the plan apply will run and how many removals it
// leaves out: all of them unless removals are allowed.
func withoutRemovals(plan *types.Plan, allowRemovals bool) (*types.Plan, int) {
	if allowRemovals {
		return plan, 0
	}
	kept := *plan
	kept.Changes = make([]types.TagChange, 0, len(plan.Changes))
	for _, c := range plan.Changes {
		if c.Action != types.ActionRemove {
			kept.Changes = append(kept.Changes, c)
		}
	}
	return &kept, len(plan.Changes) - len(kept.Changes)
}

// printSkippedRemovals reports the removals apply left out and how to run them.
func printSkippedRemovals(w io.Writer, skipped int) {
	if skipped == 0 {
		return
	}
	fmt.Fprintf(w, "Skipped: %d removal(s), no tag was removed. Run again with --%s to perform them.\n", skipped, allowRemovalsFlag)
}

// printPlanSummary describes what apply is about to do with the plan. It goes
// to stderr, next to the confirmation prompt, so both stay visible when stdout
// is redirected. Removals cannot be undone from the plan, so they are listed.
func printPlanSummary(w io.Writer, planFile string, plan *types.Plan, allowRemovals bool) {
	applied, skippedRemovals := withoutRemovals(plan, allowRemovals)
	summary := applied.Summarize()
	fmt.Fprintf(w, "Applying plan from %s\n", planFile)
	fmt.Fprintf(w, "Plan created at: %s\n\n", plan.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "Changes to apply:\n")
	fmt.Fprintf(w, "  • %d resources will be modified\n", summary.TotalResources)
	fmt.Fprintf(w, "  • %d tags will be added\n", summary.TagsAdded)
	fmt.Fprintf(w, "  • %d tags will be updated\n", summary.TagsUpdated)
	if skippedRemovals > 0 {
		fmt.Fprintf(w, "  • %d removals will be SKIPPED (pass --%s to perform them)\n", skippedRemovals, allowRemovalsFlag)
	}
	if summary.TagsRemoved == 0 {
		fmt.Fprintln(w)
		return
	}

	fmt.Fprintf(w, "  • %d tags will be REMOVED:\n", summary.TagsRemoved)
	listed := 0
	for _, c := range plan.Changes {
		if c.Action != types.ActionRemove {
			continue
		}
		if listed == maxListedRemovals {
			fmt.Fprintf(w, "      ... and %d more, see %s\n", summary.TagsRemoved-listed, planFile)
			break
		}
		fmt.Fprintf(w, "      - %s=%q on %s %s (%s)\n", printable(c.Tag), printable(c.OldValue),
			printable(c.Resource.Type), printable(c.Resource.ID), printable(string(c.Reason)))
		listed++
	}
	fmt.Fprintln(w)
}

// describeChange names the tag and what happens to it in a progress line.
func describeChange(c types.TagChange) string {
	if c.Action == types.ActionRemove {
		return "remove " + printable(c.Tag)
	}
	return printable(c.Tag) + ": " + printable(c.NewValue)
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
		fmt.Printf("  [%d/%d] %s.%s (%s) %s\n",
			changeIndex, len(plan.Changes),
			change.Resource.Type, change.Resource.ID,
			describeChange(change), status)
		if err != nil {
			fmt.Printf("         Error: %v\n", err)
		}
	})

	return applier, nil
}

// printApplyResult prints the totals and any failures from an apply run.
// skipped counts the changes declined in an interactive review.
func printApplyResult(result *engine.ApplyResult, skipped int) {
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Applied successfully: %d changes\n", result.SuccessCount)
	if skipped > 0 {
		fmt.Printf("Skipped: %d changes\n", skipped)
	}
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

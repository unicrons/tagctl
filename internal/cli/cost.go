package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/unicrons/tagctl/internal/config"
	awsprovider "github.com/unicrons/tagctl/internal/provider/aws"
	"github.com/unicrons/tagctl/internal/types"
)

var costCmd = &cobra.Command{
	Use:   "cost",
	Short: "Report how much spend your tags fail to account for",
	Long: `Cost joins your tag policy with AWS Cost Explorer and answers the question
that gets tagging work funded: how much of this bill cannot be attributed to
anyone?

Cost allocation tags are not retroactive. Spend that landed on an untagged
resource can never be assigned to a team, however diligently you tag afterwards,
so the number this reports is a permanent loss rather than a backlog item.

Reports, per required tag, the spend it does and does not account for, and the
biggest spenders behind each value.

Note that a tag must be activated as a cost allocation tag in the Billing
console before Cost Explorer will group by it. A tag that has never been
activated reports as accounting for nothing at all.

Examples:
  # The last 30 days, for the tags your policy requires
  tagctl cost

  # A specific window
  tagctl cost --days 90

  # Specific tags, whatever the policy says
  tagctl cost --tag owner --tag cost-center

  # Another account, by profile or by role
  tagctl cost --profile billing
  tagctl cost --role arn:aws:iam::123456789012:role/CostReader

  # Fail when attribution is too low
  tagctl cost --fail-under 80`,
	RunE: runCost,
}

func init() {
	costCmd.Flags().Int("days", 30, "how many days back to report on")
	costCmd.Flags().StringSlice("tag", nil, "tag to report on (repeatable; defaults to the policy's required tags)")
	costCmd.Flags().Float64("fail-under", 0, "exit 1 if any tag accounts for less than this percentage of spend")
	addAWSAuthFlags(costCmd)
}

func runCost(cmd *cobra.Command, args []string) error {
	days, _ := cmd.Flags().GetInt("days")
	tagFlags, _ := cmd.Flags().GetStringSlice("tag")
	failUnder, _ := cmd.Flags().GetFloat64("fail-under")

	format, err := outputFormatFor(cmd, formatTable, formatJSON)
	if err != nil {
		return err
	}

	if days < 1 {
		return fmt.Errorf("--days must be at least 1")
	}

	ctx, stop := signalContext()
	defer stop()

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	tags := tagFlags
	if len(tags) == 0 {
		tags = requiredTagNames(cfg)
	}
	if len(tags) == 0 {
		return fmt.Errorf("no tags to report on: add required tags to your policy or pass --tag")
	}

	if err = applyAWSAuthFlags(cfg, readAWSAuthFlags(cmd)); err != nil {
		return err
	}
	account, err := costAccount(cfg)
	if err != nil {
		return err
	}

	provider, err := awsprovider.New(ctx, account)
	if err != nil {
		return fmt.Errorf("failed to initialize AWS provider: %w", err)
	}

	// Cost Explorer works on whole days, and today is still accruing.
	end := time.Now().UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -days)

	report, err := provider.CostReport(ctx, tags, start, end)
	if err != nil {
		return err
	}

	if format == formatJSON {
		if err := printJSON(report); err != nil {
			return err
		}
	} else {
		printCostReport(report)
	}

	if failUnder > 0 {
		return checkCostCoverage(report, failUnder)
	}

	return nil
}

// requiredTagNames lists the tags the policy requires.
func requiredTagNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Policy.Required))
	for _, req := range cfg.Policy.Required {
		names = append(names, req.Name)
	}
	return names
}

// costAccount picks the AWS account to bill against. Cost Explorer reports for
// the whole payer account, so one is enough.
func costAccount(cfg *config.Config) (config.AWSAccount, error) {
	if len(cfg.Clouds.AWS) == 0 {
		return config.AWSAccount{}, fmt.Errorf("no AWS account configured. Add one to tagctl.yaml or pass --profile")
	}

	account := cfg.Clouds.AWS[0]
	// Cost Explorer is global; a single region keeps provider start-up cheap.
	account.Regions = []string{"us-east-1"}

	return account, nil
}

// checkCostCoverage fails when a tag accounts for too little spend.
func checkCostCoverage(report *types.CostReport, failUnder float64) error {
	var failing []string

	for _, tag := range sortedTagCosts(report) {
		if tag.CoveragePct() < failUnder {
			failing = append(failing, fmt.Sprintf("%s accounts for %.1f%% of spend", tag.Tag, tag.CoveragePct()))
		}
	}

	if len(failing) > 0 {
		return gateFailed(fmt.Errorf("cost attribution below the required %.1f%%: %s",
			failUnder, strings.Join(failing, "; ")))
	}

	return nil
}

// sortedTagCosts orders the report's tags by coverage, worst first, so output
// leads with the tag worth fixing.
func sortedTagCosts(report *types.CostReport) []*types.TagCost {
	tags := make([]*types.TagCost, 0, len(report.Tags))
	for _, tag := range report.Tags {
		tags = append(tags, tag)
	}

	sort.Slice(tags, func(i, j int) bool {
		if tags[i].CoveragePct() != tags[j].CoveragePct() {
			return tags[i].CoveragePct() < tags[j].CoveragePct()
		}
		return tags[i].Tag < tags[j].Tag
	})

	return tags
}

func printCostReport(report *types.CostReport) {
	fmt.Printf("Period: %s to %s (%d days)\n",
		report.Start.Format("2006-01-02"), report.End.Format("2006-01-02"), report.Days())
	fmt.Printf("Total spend: %s\n\n", money(report.Total, report.Currency))

	if report.Total == 0 {
		fmt.Println("No spend recorded for this period.")
		return
	}

	for _, tag := range sortedTagCosts(report) {
		fmt.Printf("  %s\n", tag.Tag)
		fmt.Printf("      attributed:   %14s  (%.1f%%)\n",
			money(tag.Attributed, report.Currency), tag.CoveragePct())
		fmt.Printf("      unattributed: %14s  (%.1f%%)\n",
			money(tag.Unattributed, report.Currency), 100-tag.CoveragePct())

		printTopValues(tag, report.Currency)
		fmt.Println()
	}

	printCostSummary(report)
}

// topValueCount is how many tag values are listed per tag.
const topValueCount = 5

// printTopValues lists the biggest spenders for a tag, skipping the
// unattributed bucket, which is already reported above.
func printTopValues(tag *types.TagCost, currency string) {
	shown := 0

	for _, value := range tag.Values {
		if value.Value == "" {
			continue
		}
		if shown == 0 {
			fmt.Printf("      top values:\n")
		}
		fmt.Printf("        %-28s %14s\n", truncate(value.Value, 28), money(value.Amount, currency))
		shown++
		if shown == topValueCount {
			break
		}
	}
}

func printCostSummary(report *types.CostReport) {
	worst := report.WorstCoverage()
	if worst == nil {
		return
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Worst attribution: %s, leaving %s unattributable over %d days.\n",
		worst.Tag, money(worst.Unattributed, report.Currency), report.Days())

	if report.Days() > 0 {
		annual := worst.Unattributed / float64(report.Days()) * 365
		fmt.Printf("At this rate that is %s a year of spend nobody can be billed for.\n",
			money(annual, report.Currency))
	}

	fmt.Println()
	fmt.Println("Cost allocation tags are not retroactive: this spend cannot be")
	fmt.Println("attributed later. Run 'tagctl plan' to start tagging what is live.")
}

// money formats an amount with its currency.
func money(amount float64, currency string) string {
	if currency == "" {
		currency = "USD"
	}
	return fmt.Sprintf("%.2f %s", amount, currency)
}

// truncate shortens a value so the columns stay aligned.
func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	if max <= 1 {
		return value[:max]
	}
	return value[:max-1] + "…"
}

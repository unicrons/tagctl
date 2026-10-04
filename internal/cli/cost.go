package cli

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
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
biggest spenders behind each value. With --trend the window is also split into
periods (daily up to 14 days, weekly beyond), with the change between the first
and last full period and a linear estimate of the next one.

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

  # Is the unattributed spend growing? Per period, with an estimate of the next
  tagctl cost --trend

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
	costCmd.Flags().Bool("trend", false, "split the window into periods (daily up to 14 days, weekly beyond) and estimate the next one")
	costCmd.Flags().Float64("fail-under", 0, "exit 1 if any tag accounts for less than this percentage of spend")
	addAWSAuthFlags(costCmd)
}

func runCost(cmd *cobra.Command, args []string) error {
	days, _ := cmd.Flags().GetInt("days")
	tagFlags, _ := cmd.Flags().GetStringSlice("tag")
	failUnder, _ := cmd.Flags().GetFloat64("fail-under")
	trend, _ := cmd.Flags().GetBool("trend")

	format, err := outputFormatFor(cmd, formatTable, formatJSON, formatCSV)
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

	var granularity types.CostGranularity
	if trend {
		granularity = trendGranularity(days)
	}

	report, err := provider.CostReport(ctx, tags, start, end, granularity)
	if err != nil {
		return err
	}

	switch format {
	case formatJSON:
		err = printJSON(report)
	case formatCSV:
		err = writeCostCSV(os.Stdout, report)
	default:
		printCostReport(os.Stdout, report)
	}
	if err != nil {
		return err
	}

	if failUnder > 0 {
		return checkCostCoverage(report, failUnder)
	}

	return nil
}

// maxDailyTrendDays is the longest window still reported day by day.
const maxDailyTrendDays = 14

// trendGranularity picks the period length that keeps a trend readable.
func trendGranularity(days int) types.CostGranularity {
	if days <= maxDailyTrendDays {
		return types.CostDaily
	}
	return types.CostWeekly
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

const costDateLayout = "2006-01-02"

func printCostReport(w io.Writer, report *types.CostReport) {
	fmt.Fprintf(w, "Period: %s to %s (%d days)\n",
		report.Start.Format(costDateLayout), report.End.Format(costDateLayout), report.Days())
	fmt.Fprintf(w, "Total spend: %s\n\n", money(report.Total, report.Currency))

	if report.Total == 0 {
		fmt.Fprintln(w, "No spend recorded for this period.")
		return
	}

	for _, tag := range sortedTagCosts(report) {
		fmt.Fprintf(w, "  %s\n", tag.Tag)
		fmt.Fprintf(w, "      attributed:   %14s  (%.1f%%)\n",
			money(tag.Attributed, report.Currency), tag.CoveragePct())
		fmt.Fprintf(w, "      unattributed: %14s  (%.1f%%)\n",
			money(tag.Unattributed, report.Currency), 100-tag.CoveragePct())

		printTopValues(w, tag, report.Currency)
		printCostTrend(w, tag.Trend, report.Currency)
		fmt.Fprintln(w)
	}

	printCostSummary(w, report)
}

// topValueCount is how many tag values are listed per tag.
const topValueCount = 5

// printTopValues lists the biggest spenders for a tag, skipping the
// unattributed bucket, which is already reported above.
func printTopValues(w io.Writer, tag *types.TagCost, currency string) {
	shown := 0

	for _, value := range tag.Values {
		if value.Value == "" {
			continue
		}
		if shown == 0 {
			fmt.Fprintf(w, "      top values:\n")
		}
		fmt.Fprintf(w, "        %-28s %14s\n", truncate(value.Value, 28), money(value.Amount, currency))
		shown++
		if shown == topValueCount {
			break
		}
	}
}

// printCostTrend lists a tag's periods, the change across them and the
// estimate for the next one.
func printCostTrend(w io.Writer, trend *types.CostTrend, currency string) {
	if trend == nil {
		return
	}

	fmt.Fprintf(w, "      trend (%s):\n", trend.Granularity)
	fmt.Fprintf(w, "        %-10s  %4s  %14s  %14s  %8s\n", "from", "days", "attributed", "unattributed", "coverage")
	for _, period := range trend.Periods {
		note := ""
		if period.Partial {
			note = "  partial"
		}
		fmt.Fprintf(w, "        %-10s  %4d  %14s  %14s  %7.1f%%%s\n",
			period.Start.Format(costDateLayout), period.Days(),
			money(period.Attributed, currency), money(period.Unattributed, currency),
			period.CoveragePct(), note)
	}

	if trend.Change == nil || trend.Projection == nil {
		fmt.Fprintln(w, "        Too few full periods for a change or an estimate.")
		return
	}

	fmt.Fprintf(w, "        change, first to last full period: unattributed %s, coverage %+.1f points\n",
		signedMoney(trend.Change.Unattributed, currency), trend.Change.CoveragePoints)
	fmt.Fprintf(w, "        estimate for the %d days from %s: %s unattributed (%s projection, not billed spend)\n",
		int(trend.Projection.End.Sub(trend.Projection.Start).Hours()/24),
		trend.Projection.Start.Format(costDateLayout),
		money(trend.Projection.Unattributed, currency), trend.Projection.Method)
}

func printCostSummary(w io.Writer, report *types.CostReport) {
	worst := report.WorstCoverage()
	if worst == nil {
		return
	}

	fmt.Fprintln(w, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Fprintf(w, "Worst attribution: %s, leaving %s unattributable over %d days.\n",
		worst.Tag, money(worst.Unattributed, report.Currency), report.Days())

	if report.Days() > 0 {
		annual := worst.Unattributed / float64(report.Days()) * 365
		fmt.Fprintf(w, "At this rate that is %s a year of spend nobody can be billed for.\n",
			money(annual, report.Currency))
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Cost allocation tags are not retroactive: this spend cannot be")
	fmt.Fprintln(w, "attributed later. Run 'tagctl plan' to start tagging what is live.")
}

// Values of the kind column in the trend CSV.
const (
	periodActual   = "actual"
	periodPartial  = "partial"
	periodEstimate = "estimate"
)

const costCSVTagColumn = "tag"

var (
	costCSVHeader      = []string{costCSVTagColumn, "attributed", "unattributed", "coverage_percent", "currency"}
	costTrendCSVHeader = []string{costCSVTagColumn, "period_start", "period_end", "kind", "attributed", "unattributed", "coverage_percent", "currency"}
)

// writeCostCSV writes one row per tag, or one per tag and period when the
// report carries a trend; the projection is the row of kind "estimate".
func writeCostCSV(w io.Writer, report *types.CostReport) error {
	if err := csv.NewWriter(w).WriteAll(costCSVRows(report)); err != nil {
		return fmt.Errorf("failed to write cost CSV: %w", err)
	}
	return nil
}

func costCSVRows(report *types.CostReport) [][]string {
	tags := sortedTagCosts(report)

	if hasTrend(tags) {
		rows := make([][]string, 0, len(tags)+1)
		rows = append(rows, costTrendCSVHeader)
		for _, tag := range tags {
			rows = append(rows, trendCSVRows(tag, report.Currency)...)
		}
		return rows
	}

	rows := make([][]string, 0, len(tags)+1)
	rows = append(rows, costCSVHeader)
	for _, tag := range tags {
		rows = append(rows, []string{
			csvSafe(tag.Tag), csvAmount(tag.Attributed), csvAmount(tag.Unattributed),
			csvPercent(tag.CoveragePct()), report.Currency,
		})
	}
	return rows
}

func hasTrend(tags []*types.TagCost) bool {
	for _, tag := range tags {
		if tag.Trend != nil {
			return true
		}
	}
	return false
}

func trendCSVRows(tag *types.TagCost, currency string) [][]string {
	if tag.Trend == nil {
		return nil
	}
	name := csvSafe(tag.Tag)

	rows := make([][]string, 0, len(tag.Trend.Periods)+1)
	for _, period := range tag.Trend.Periods {
		kind := periodActual
		if period.Partial {
			kind = periodPartial
		}
		rows = append(rows, []string{
			name, period.Start.Format(costDateLayout), period.End.Format(costDateLayout), kind,
			csvAmount(period.Attributed), csvAmount(period.Unattributed), csvPercent(period.CoveragePct()), currency,
		})
	}

	if projection := tag.Trend.Projection; projection != nil {
		rows = append(rows, []string{
			name, projection.Start.Format(costDateLayout), projection.End.Format(costDateLayout), periodEstimate,
			"", csvAmount(projection.Unattributed), "", currency,
		})
	}
	return rows
}

func csvAmount(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}

func csvPercent(value float64) string {
	return strconv.FormatFloat(value, 'f', 1, 64)
}

// money formats an amount with its currency.
func money(amount float64, currency string) string {
	if currency == "" {
		currency = "USD"
	}
	return fmt.Sprintf("%.2f %s", amount, currency)
}

// signedMoney formats a change with an explicit sign.
func signedMoney(amount float64, currency string) string {
	if currency == "" {
		currency = "USD"
	}
	return fmt.Sprintf("%+.2f %s", amount, currency)
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

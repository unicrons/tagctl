package cli

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/unicrons/tagctl/internal/types"
)

func trendReport(days int, granularity types.CostGranularity, unattributed ...float64) *types.CostReport {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, days)

	periods := types.CostPeriods(start, end, granularity)
	owner := &types.TagCost{Tag: "owner"}
	for i := range periods {
		periods[i].Unattributed = unattributed[i]
		periods[i].Attributed = 1000 - unattributed[i]
		owner.Unattributed += periods[i].Unattributed
		owner.Attributed += periods[i].Attributed
	}
	owner.Trend = types.NewCostTrend(granularity, periods)

	return &types.CostReport{
		Start:    start,
		End:      end,
		Currency: "USD",
		Total:    owner.Total(),
		Tags:     map[string]*types.TagCost{"owner": owner},
	}
}

func TestTrendGranularity(t *testing.T) {
	for days, want := range map[int]types.CostGranularity{
		1:  types.CostDaily,
		14: types.CostDaily,
		15: types.CostWeekly,
		90: types.CostWeekly,
	} {
		if got := trendGranularity(days); got != want {
			t.Errorf("trendGranularity(%d) = %q, want %q", days, got, want)
		}
	}
}

func TestCostCommand_AcceptsCSVAndTrend(t *testing.T) {
	if costCmd.Flags().Lookup("trend") == nil {
		t.Fatal("cost has no --trend flag")
	}
	policy := writeFixture(t, t.TempDir(), "tagctl.yaml", "policy:\n  required:\n    - name: owner\n")

	run := execute(t, "-c", policy, "cost", "-o", "csv", "--trend", "--days", "0")

	if run.err == nil || !strings.Contains(run.err.Error(), "--days must be at least 1") {
		t.Errorf("cost -o csv --trend --days 0 error = %v, want the window error, not a flag or format error", run.err)
	}
}

func TestPrintCostReport_TrendListsPeriodsChangeAndLabelledEstimate(t *testing.T) {
	var buf bytes.Buffer
	printCostReport(&buf, trendReport(16, types.CostWeekly, 50, 100, 150))
	out := buf.String()

	for _, want := range []string{
		"trend (weekly):",
		"2026-01-01     2      950.00 USD       50.00 USD     95.0%  partial",
		"2026-01-03     7      900.00 USD      100.00 USD     90.0%\n",
		"2026-01-10     7      850.00 USD      150.00 USD     85.0%\n",
		"change, first to last full period: unattributed +50.00 USD, coverage -5.0 points",
		"estimate for the 7 days from 2026-01-17: 200.00 USD unattributed (linear projection, not billed spend)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPrintCostReport_TrendWithOnePeriodSaysWhyThereIsNoEstimate(t *testing.T) {
	var buf bytes.Buffer
	printCostReport(&buf, trendReport(1, types.CostDaily, 50))
	out := buf.String()

	if !strings.Contains(out, "Too few full periods for a change or an estimate.") {
		t.Errorf("output does not explain the missing estimate:\n%s", out)
	}
	if strings.Contains(out, "estimate for the") {
		t.Errorf("output estimates from a single period:\n%s", out)
	}
}

func TestPrintCostReport_NoTrendSectionUnlessRequested(t *testing.T) {
	report := costReport(map[string][2]float64{"owner": {9000, 1000}})
	report.Total = 10000

	var buf bytes.Buffer
	printCostReport(&buf, report)

	if strings.Contains(buf.String(), "trend") {
		t.Errorf("output has a trend section without --trend:\n%s", buf.String())
	}
}

func TestWriteCostCSV_OneRowPerTagWorstFirst(t *testing.T) {
	report := costReport(map[string][2]float64{
		"owner":       {9000, 1000},
		"cost-center": {2000, 8000},
	})

	var buf bytes.Buffer
	if err := writeCostCSV(&buf, report); err != nil {
		t.Fatalf("writeCostCSV: %v", err)
	}

	want := "tag,attributed,unattributed,coverage_percent,currency\n" +
		"cost-center,2000.00,8000.00,20.0,USD\n" +
		"owner,9000.00,1000.00,90.0,USD\n"
	if buf.String() != want {
		t.Errorf("CSV =\n%s\nwant\n%s", buf.String(), want)
	}
}

func TestWriteCostCSV_TrendHasOneRowPerPeriodAndAnEstimateRow(t *testing.T) {
	var buf bytes.Buffer
	if err := writeCostCSV(&buf, trendReport(16, types.CostWeekly, 50, 100, 150)); err != nil {
		t.Fatalf("writeCostCSV: %v", err)
	}

	want := "tag,period_start,period_end,kind,attributed,unattributed,coverage_percent,currency\n" +
		"owner,2026-01-01,2026-01-03,partial,950.00,50.00,95.0,USD\n" +
		"owner,2026-01-03,2026-01-10,actual,900.00,100.00,90.0,USD\n" +
		"owner,2026-01-10,2026-01-17,actual,850.00,150.00,85.0,USD\n" +
		"owner,2026-01-17,2026-01-24,estimate,,200.00,,USD\n"
	if buf.String() != want {
		t.Errorf("CSV =\n%s\nwant\n%s", buf.String(), want)
	}
}

func TestWriteCostCSV_NeutralisesFormulaTagNames(t *testing.T) {
	report := costReport(map[string][2]float64{"=cmd()": {1, 1}})

	var buf bytes.Buffer
	if err := writeCostCSV(&buf, report); err != nil {
		t.Fatalf("writeCostCSV: %v", err)
	}

	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if rows[1][0] != "'=cmd()" {
		t.Errorf("tag cell = %q, want it prefixed with a quote", rows[1][0])
	}
}

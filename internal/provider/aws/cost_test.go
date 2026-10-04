package aws

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"

	"github.com/unicrons/tagctl/internal/types"
)

// mockCostExplorer serves GetCostAndUsage pages per tag key.
type mockCostExplorer struct {
	// pages maps a tag key to the pages of groups returned for it.
	pages map[string][][]cetypes.Group
	err   error
	calls int
	// perTagCalls counts how many pages were served for each tag.
	perTagCalls map[string]int
	// daily maps a tag key to per-day results, served in one page.
	daily         map[string][]cetypes.ResultByTime
	granularities []cetypes.Granularity
}

// dayResult builds one day of a daily Cost Explorer answer for a tag.
func dayResult(start time.Time, offset int, tag, attributed, unattributed string) cetypes.ResultByTime {
	day := start.AddDate(0, 0, offset)
	return cetypes.ResultByTime{
		TimePeriod: &cetypes.DateInterval{
			Start: aws.String(day.Format(costDateLayout)),
			End:   aws.String(day.AddDate(0, 0, 1).Format(costDateLayout)),
		},
		Groups: []cetypes.Group{
			group(tag, "team-a@example.com", attributed),
			group(tag, "", unattributed),
		},
	}
}

func (m *mockCostExplorer) GetCostAndUsage(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.calls++
	m.granularities = append(m.granularities, params.Granularity)

	tag := aws.ToString(params.GroupBy[0].Key)
	if daily, ok := m.daily[tag]; ok {
		return &costexplorer.GetCostAndUsageOutput{ResultsByTime: daily}, nil
	}
	if m.perTagCalls == nil {
		m.perTagCalls = make(map[string]int)
	}
	page := m.perTagCalls[tag]
	m.perTagCalls[tag]++

	groups := m.pages[tag]
	out := &costexplorer.GetCostAndUsageOutput{
		ResultsByTime: []cetypes.ResultByTime{{Groups: groups[page]}},
	}
	if page < len(groups)-1 {
		out.NextPageToken = aws.String("next")
	}
	return out, nil
}

// group builds a Cost Explorer group for a tag value and amount.
func group(tag, value, amount string) cetypes.Group {
	return cetypes.Group{
		Keys: []string{tag + "$" + value},
		Metrics: map[string]cetypes.MetricValue{
			"UnblendedCost": {Amount: aws.String(amount), Unit: aws.String("USD")},
		},
	}
}

func costProvider() *Provider {
	return &Provider{accountID: "123456789012", regions: []string{"us-east-1"}}
}

func period() (time.Time, time.Time) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0)
}

func TestCostReport_SplitsAttributedAndUnattributed(t *testing.T) {
	mock := &mockCostExplorer{
		pages: map[string][][]cetypes.Group{
			"owner": {{
				group("owner", "team-a@example.com", "4000.00"),
				group("owner", "team-b@example.com", "1000.00"),
				group("owner", "", "5000.00"), // untagged spend
			}},
		},
	}

	start, end := period()
	report, err := costProvider().costReportFrom(context.Background(), mock, []string{"owner"}, start, end, "")
	if err != nil {
		t.Fatalf("costReportFrom() error = %v", err)
	}

	owner := report.Tags["owner"]
	if owner == nil {
		t.Fatal("no cost entry for the owner tag")
	}
	if owner.Attributed != 5000 {
		t.Errorf("Attributed = %v, want 5000", owner.Attributed)
	}
	if owner.Unattributed != 5000 {
		t.Errorf("Unattributed = %v, want 5000 (the empty group)", owner.Unattributed)
	}
	if owner.Total() != 10000 {
		t.Errorf("Total() = %v, want 10000", owner.Total())
	}
	if owner.CoveragePct() != 50 {
		t.Errorf("CoveragePct() = %v, want 50", owner.CoveragePct())
	}
	if report.Currency != "USD" {
		t.Errorf("Currency = %q, want USD", report.Currency)
	}
	if report.Total != 10000 {
		t.Errorf("Total = %v, want 10000", report.Total)
	}
}

// Values are listed most expensive first, so the report leads with the spend
// worth chasing.
func TestCostReport_ValuesSortedByAmount(t *testing.T) {
	mock := &mockCostExplorer{
		pages: map[string][][]cetypes.Group{
			"environment": {{
				group("environment", "dev", "100.00"),
				group("environment", "prod", "9000.00"),
				group("environment", "staging", "500.00"),
			}},
		},
	}

	start, end := period()
	report, err := costProvider().costReportFrom(context.Background(), mock, []string{"environment"}, start, end, "")
	if err != nil {
		t.Fatalf("costReportFrom() error = %v", err)
	}

	values := report.Tags["environment"].Values
	if len(values) != 3 {
		t.Fatalf("got %d values, want 3", len(values))
	}
	if values[0].Value != "prod" || values[0].Amount != 9000 {
		t.Errorf("first value = %+v, want prod at 9000", values[0])
	}
	if values[2].Value != "dev" {
		t.Errorf("last value = %q, want dev", values[2].Value)
	}
}

func TestCostReport_Paginates(t *testing.T) {
	mock := &mockCostExplorer{
		pages: map[string][][]cetypes.Group{
			"owner": {
				{group("owner", "team-a@example.com", "1000.00")},
				{group("owner", "team-b@example.com", "2000.00")},
				{group("owner", "", "3000.00")},
			},
		},
	}

	start, end := period()
	report, err := costProvider().costReportFrom(context.Background(), mock, []string{"owner"}, start, end, "")
	if err != nil {
		t.Fatalf("costReportFrom() error = %v", err)
	}

	if mock.calls != 3 {
		t.Errorf("made %d calls, want 3 (one per page)", mock.calls)
	}
	owner := report.Tags["owner"]
	if owner.Attributed != 3000 {
		t.Errorf("Attributed = %v, want 3000 across pages", owner.Attributed)
	}
	if owner.Unattributed != 3000 {
		t.Errorf("Unattributed = %v, want 3000", owner.Unattributed)
	}
}

func TestCostReport_MultipleTags(t *testing.T) {
	mock := &mockCostExplorer{
		pages: map[string][][]cetypes.Group{
			"owner": {{
				group("owner", "team-a@example.com", "9000.00"),
				group("owner", "", "1000.00"),
			}},
			"cost-center": {{
				group("cost-center", "cc-1", "2000.00"),
				group("cost-center", "", "8000.00"),
			}},
		},
	}

	start, end := period()
	report, err := costProvider().costReportFrom(context.Background(), mock, []string{"owner", "cost-center"}, start, end, "")
	if err != nil {
		t.Fatalf("costReportFrom() error = %v", err)
	}

	if len(report.Tags) != 2 {
		t.Fatalf("got %d tags, want 2", len(report.Tags))
	}
	if report.Tags["owner"].CoveragePct() != 90 {
		t.Errorf("owner coverage = %v, want 90", report.Tags["owner"].CoveragePct())
	}
	if report.Tags["cost-center"].CoveragePct() != 20 {
		t.Errorf("cost-center coverage = %v, want 20", report.Tags["cost-center"].CoveragePct())
	}

	// Every tag measures the same spend, so the total is not summed across them.
	if report.Total != 10000 {
		t.Errorf("Total = %v, want 10000 (not the sum across tags)", report.Total)
	}

	worst := report.WorstCoverage()
	if worst == nil || worst.Tag != "cost-center" {
		t.Errorf("WorstCoverage() = %+v, want cost-center", worst)
	}
}

func TestCostReport_WithoutTrendAsksMonthlyAndSetsNoTrend(t *testing.T) {
	mock := &mockCostExplorer{
		pages: map[string][][]cetypes.Group{"owner": {{group("owner", "", "10.00")}}},
	}

	start, end := period()
	report, err := costProvider().costReportFrom(context.Background(), mock, []string{"owner"}, start, end, "")
	if err != nil {
		t.Fatalf("costReportFrom() error = %v", err)
	}

	if len(mock.granularities) != 1 || mock.granularities[0] != cetypes.GranularityMonthly {
		t.Errorf("granularities = %v, want one MONTHLY request", mock.granularities)
	}
	if report.Tags["owner"].Trend != nil {
		t.Errorf("Trend = %+v, want none when no trend was requested", report.Tags["owner"].Trend)
	}
}

func TestCostReport_TrendBucketsDaysIntoWeeksInOneRequestPerTag(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 16)

	daily := map[string][]cetypes.ResultByTime{}
	for _, tag := range []string{"owner", "team"} {
		for offset := range 16 {
			attributed, unattributed := "90.00", "10.00"
			if offset >= 9 {
				attributed, unattributed = "80.00", "20.00"
			}
			daily[tag] = append(daily[tag], dayResult(start, offset, tag, attributed, unattributed))
		}
	}
	mock := &mockCostExplorer{daily: daily}

	report, err := costProvider().costReportFrom(context.Background(), mock, []string{"owner", "team"}, start, end, types.CostWeekly)
	if err != nil {
		t.Fatalf("costReportFrom() error = %v", err)
	}

	if mock.calls != 2 {
		t.Errorf("made %d requests, want 2 (one per tag, not one per period)", mock.calls)
	}
	for _, granularity := range mock.granularities {
		if granularity != cetypes.GranularityDaily {
			t.Errorf("granularity = %v, want DAILY for a trend", granularity)
		}
	}

	owner := report.Tags["owner"]
	if owner.Attributed != 1370 || owner.Unattributed != 230 {
		t.Errorf("totals = %v attributed, %v unattributed, want 1370 and 230", owner.Attributed, owner.Unattributed)
	}

	trend := owner.Trend
	if trend == nil {
		t.Fatal("no trend on a report that requested one")
	}
	if trend.Granularity != types.CostWeekly || len(trend.Periods) != 3 {
		t.Fatalf("trend = %+v, want three weekly periods", trend)
	}

	want := []struct {
		attributed, unattributed float64
		partial                  bool
	}{
		{180, 20, true},
		{630, 70, false},
		{560, 140, false},
	}
	for i, w := range want {
		got := trend.Periods[i]
		if got.Attributed != w.attributed || got.Unattributed != w.unattributed || got.Partial != w.partial {
			t.Errorf("period %d = %+v, want %+v", i, got, w)
		}
	}

	if trend.Change == nil || trend.Change.Unattributed != 70 {
		t.Errorf("change = %+v, want unattributed up by 70", trend.Change)
	}
	if trend.Projection == nil || trend.Projection.Unattributed != 210 || !trend.Projection.Estimate {
		t.Errorf("projection = %+v, want an estimate of 210", trend.Projection)
	}
}

func TestCostReport_TrendAccumulatesAcrossPages(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 2)

	mock := &pagedDailyCostExplorer{pages: [][]cetypes.ResultByTime{
		{dayResult(start, 0, "owner", "5.00", "1.00")},
		{dayResult(start, 0, "owner", "5.00", "1.00"), dayResult(start, 1, "owner", "7.00", "3.00")},
	}}

	report, err := costProvider().costReportFrom(context.Background(), mock, []string{"owner"}, start, end, types.CostDaily)
	if err != nil {
		t.Fatalf("costReportFrom() error = %v", err)
	}

	periods := report.Tags["owner"].Trend.Periods
	if len(periods) != 2 {
		t.Fatalf("got %d periods, want 2", len(periods))
	}
	if periods[0].Attributed != 10 || periods[0].Unattributed != 2 {
		t.Errorf("first day = %+v, want 10 attributed and 2 unattributed across both pages", periods[0])
	}
	if periods[1].Attributed != 7 || periods[1].Unattributed != 3 {
		t.Errorf("second day = %+v, want 7 attributed and 3 unattributed", periods[1])
	}
}

func TestPeriodOf(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	periods := types.CostPeriods(start, start.AddDate(0, 0, 14), types.CostWeekly)

	interval := func(day string) *cetypes.DateInterval {
		return &cetypes.DateInterval{Start: aws.String(day)}
	}

	if got := periodOf(periods, interval("2026-01-08")); got != &periods[1] {
		t.Errorf("periodOf(2026-01-08) = %+v, want the second week", got)
	}
	for name, in := range map[string]*cetypes.DateInterval{
		"outside the window": interval("2026-02-01"),
		"unparsable day":     interval("yesterday"),
		"no interval":        nil,
	} {
		if got := periodOf(periods, in); got != nil {
			t.Errorf("periodOf(%s) = %+v, want nil", name, got)
		}
	}
	if got := periodOf(nil, interval("2026-01-08")); got != nil {
		t.Errorf("periodOf with no periods = %+v, want nil", got)
	}
}

// pagedDailyCostExplorer serves daily results one page per request.
type pagedDailyCostExplorer struct {
	pages [][]cetypes.ResultByTime
	next  int
}

func (m *pagedDailyCostExplorer) GetCostAndUsage(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
	out := &costexplorer.GetCostAndUsageOutput{ResultsByTime: m.pages[m.next]}
	m.next++
	if m.next < len(m.pages) {
		out.NextPageToken = aws.String("next")
	}
	return out, nil
}

func TestCostReport_RequiresTags(t *testing.T) {
	start, end := period()

	_, err := costProvider().costReportFrom(context.Background(), &mockCostExplorer{}, nil, start, end, "")
	if err == nil {
		t.Fatal("costReportFrom() with no tags returned nil error")
	}
}

func TestCostReport_APIError(t *testing.T) {
	mock := &mockCostExplorer{err: errors.New("access denied")}

	start, end := period()
	if _, err := costProvider().costReportFrom(context.Background(), mock, []string{"owner"}, start, end, ""); err == nil {
		t.Fatal("costReportFrom() returned nil error when the API failed")
	}
}

// A group with no parsable amount is skipped rather than failing the report.
func TestCostReport_SkipsUnparsableAmounts(t *testing.T) {
	mock := &mockCostExplorer{
		pages: map[string][][]cetypes.Group{
			"owner": {{
				group("owner", "team-a@example.com", "1000.00"),
				{
					Keys: []string{"owner$broken"},
					Metrics: map[string]cetypes.MetricValue{
						"UnblendedCost": {Amount: aws.String("not-a-number"), Unit: aws.String("USD")},
					},
				},
				{Keys: []string{"owner$no-metric"}, Metrics: map[string]cetypes.MetricValue{}},
			}},
		},
	}

	start, end := period()
	report, err := costProvider().costReportFrom(context.Background(), mock, []string{"owner"}, start, end, "")
	if err != nil {
		t.Fatalf("costReportFrom() error = %v", err)
	}
	if report.Tags["owner"].Attributed != 1000 {
		t.Errorf("Attributed = %v, want 1000 with the broken groups skipped",
			report.Tags["owner"].Attributed)
	}
}

func TestTagValueOf(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		tag  string
		want string
	}{
		{"tagged", []string{"owner$team@example.com"}, "owner", "team@example.com"},
		{"untagged", []string{"owner$"}, "owner", ""},
		{"value containing the separator", []string{"owner$a$b"}, "owner", "a$b"},
		{"no keys", nil, "owner", ""},
		{"key without the expected prefix", []string{"something-else"}, "owner", "something-else"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tagValueOf(tt.keys, tt.tag); got != tt.want {
				t.Errorf("tagValueOf(%v, %q) = %q, want %q", tt.keys, tt.tag, got, tt.want)
			}
		})
	}
}

func TestAmountOf(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		amount, unit, err := amountOf(map[string]cetypes.MetricValue{
			"UnblendedCost": {Amount: aws.String("1234.56"), Unit: aws.String("USD")},
		})
		if err != nil {
			t.Fatalf("amountOf() error = %v", err)
		}
		if amount != 1234.56 {
			t.Errorf("amount = %v, want 1234.56", amount)
		}
		if unit != "USD" {
			t.Errorf("unit = %q, want USD", unit)
		}
	})

	t.Run("missing metric", func(t *testing.T) {
		if _, _, err := amountOf(map[string]cetypes.MetricValue{}); err == nil {
			t.Error("amountOf() with no UnblendedCost returned nil error")
		}
	})

	t.Run("unparsable amount", func(t *testing.T) {
		_, _, err := amountOf(map[string]cetypes.MetricValue{
			"UnblendedCost": {Amount: aws.String("abc")},
		})
		if err == nil {
			t.Error("amountOf() with a non-numeric amount returned nil error")
		}
	})
}

func TestCostReport_ZeroSpend(t *testing.T) {
	mock := &mockCostExplorer{
		pages: map[string][][]cetypes.Group{"owner": {{}}},
	}

	start, end := period()
	report, err := costProvider().costReportFrom(context.Background(), mock, []string{"owner"}, start, end, "")
	if err != nil {
		t.Fatalf("costReportFrom() error = %v", err)
	}

	// No spend must not divide by zero.
	if got := report.Tags["owner"].CoveragePct(); got != 0 {
		t.Errorf("CoveragePct() = %v for zero spend, want 0", got)
	}
	if report.Total != 0 {
		t.Errorf("Total = %v, want 0", report.Total)
	}
}

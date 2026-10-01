package types

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func day(n int) time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n)
}

func periodsWithUnattributed(amounts ...float64) []CostPeriod {
	periods := CostPeriods(day(0), day(len(amounts)), CostDaily)
	for i, amount := range amounts {
		periods[i].Unattributed = amount
		periods[i].Attributed = 100 - amount
	}
	return periods
}

func TestCostPeriods_DailyCoversEveryDay(t *testing.T) {
	periods := CostPeriods(day(0), day(3), CostDaily)

	if len(periods) != 3 {
		t.Fatalf("got %d periods, want 3", len(periods))
	}
	for i, period := range periods {
		if !period.Start.Equal(day(i)) || !period.End.Equal(day(i+1)) || period.Partial {
			t.Errorf("period %d = %+v, want the full day %d", i, period, i)
		}
	}
}

func TestCostPeriods_WeeklyPutsTheRemainderFirst(t *testing.T) {
	periods := CostPeriods(day(0), day(30), CostWeekly)

	if len(periods) != 5 {
		t.Fatalf("got %d periods, want 5 (2 days + 4 weeks)", len(periods))
	}
	if first := periods[0]; !first.Partial || first.Days() != 2 {
		t.Errorf("first period = %+v, want a partial one of 2 days", first)
	}
	for _, period := range periods[1:] {
		if period.Partial || period.Days() != 7 {
			t.Errorf("period = %+v, want a full week", period)
		}
	}
	if last := periods[4]; !last.End.Equal(day(30)) {
		t.Errorf("last period ends %v, want the end of the window", last.End)
	}
}

func TestCostPeriods_WholeWeeksHaveNoPartial(t *testing.T) {
	for _, period := range CostPeriods(day(0), day(28), CostWeekly) {
		if period.Partial {
			t.Errorf("period %+v is partial in a window of whole weeks", period)
		}
	}
}

func TestCostPeriods_EmptyWindow(t *testing.T) {
	if periods := CostPeriods(day(3), day(3), CostDaily); len(periods) != 0 {
		t.Errorf("got %d periods for an empty window, want none", len(periods))
	}
}

func TestCostPeriod_ContainsIsHalfOpen(t *testing.T) {
	period := CostPeriod{Start: day(7), End: day(14)}

	for n, want := range map[int]bool{6: false, 7: true, 13: true, 14: false} {
		if got := period.Contains(day(n)); got != want {
			t.Errorf("Contains(day %d) = %v, want %v", n, got, want)
		}
	}
}

func TestNewCostTrend_ChangeComparesFirstAndLastPeriod(t *testing.T) {
	trend := NewCostTrend(CostDaily, periodsWithUnattributed(10, 40, 30))

	if trend.Change == nil {
		t.Fatal("no change for three full periods")
	}
	if trend.Change.Unattributed != 20 {
		t.Errorf("unattributed change = %v, want 20", trend.Change.Unattributed)
	}
	if trend.Change.CoveragePoints != -20 {
		t.Errorf("coverage change = %v points, want -20", trend.Change.CoveragePoints)
	}
}

func TestNewCostTrend_ProjectsTheLineOnePeriodAhead(t *testing.T) {
	trend := NewCostTrend(CostDaily, periodsWithUnattributed(10, 20, 30))

	projection := trend.Projection
	if projection == nil {
		t.Fatal("no projection for three full periods")
	}
	if math.Abs(projection.Unattributed-40) > 1e-9 {
		t.Errorf("projected unattributed = %v, want 40", projection.Unattributed)
	}
	if !projection.Estimate || projection.Method != ProjectionLinear {
		t.Errorf("projection = %+v, want it marked as a linear estimate", projection)
	}
	if !projection.Start.Equal(day(3)) || !projection.End.Equal(day(4)) {
		t.Errorf("projection covers %v to %v, want the day after the window", projection.Start, projection.End)
	}
}

func TestNewCostTrend_ProjectionFitsNoisyPeriods(t *testing.T) {
	trend := NewCostTrend(CostDaily, periodsWithUnattributed(10, 30, 20, 40))

	// Least squares over (0,10) (1,30) (2,20) (3,40): slope 8, intercept 13.
	if got := trend.Projection.Unattributed; math.Abs(got-45) > 1e-9 {
		t.Errorf("projected unattributed = %v, want 45", got)
	}
}

func TestNewCostTrend_ProjectionNeverGoesNegative(t *testing.T) {
	trend := NewCostTrend(CostDaily, periodsWithUnattributed(30, 10, 0))

	if got := trend.Projection.Unattributed; got != 0 {
		t.Errorf("projected unattributed = %v, want 0", got)
	}
}

func TestNewCostTrend_FlatSpendProjectsTheSame(t *testing.T) {
	trend := NewCostTrend(CostDaily, periodsWithUnattributed(25, 25))

	if got := trend.Projection.Unattributed; got != 25 {
		t.Errorf("projected unattributed = %v, want 25", got)
	}
}

func TestNewCostTrend_IgnoresPartialPeriod(t *testing.T) {
	periods := CostPeriods(day(0), day(16), CostWeekly)
	periods[0].Unattributed = 999
	periods[1].Unattributed = 100
	periods[2].Unattributed = 150

	trend := NewCostTrend(CostWeekly, periods)

	if trend.Change.Unattributed != 50 {
		t.Errorf("unattributed change = %v, want 50 (the partial period left out)", trend.Change.Unattributed)
	}
	if got := trend.Projection.Unattributed; math.Abs(got-200) > 1e-9 {
		t.Errorf("projected unattributed = %v, want 200", got)
	}
	if got := trend.Projection.End.Sub(trend.Projection.Start); got != 7*24*time.Hour {
		t.Errorf("projection spans %v, want one week", got)
	}
}

func TestNewCostTrend_OneFullPeriodHasNoChangeOrProjection(t *testing.T) {
	trend := NewCostTrend(CostWeekly, CostPeriods(day(0), day(9), CostWeekly))

	if trend.Change != nil || trend.Projection != nil {
		t.Errorf("trend = %+v, want neither change nor projection", trend)
	}
	if len(trend.Periods) != 2 {
		t.Errorf("got %d periods, want the partial and the full one", len(trend.Periods))
	}
}

func TestTagCost_JSONOmitsTrendUnlessRequested(t *testing.T) {
	plain, err := json.Marshal(&TagCost{Tag: "owner"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(plain), "trend") {
		t.Errorf("JSON without a trend = %s, want no trend key", plain)
	}

	withTrend, err := json.Marshal(&TagCost{
		Tag:   "owner",
		Trend: NewCostTrend(CostDaily, periodsWithUnattributed(10, 20)),
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, key := range []string{`"trend"`, `"granularity":"daily"`, `"periods"`, `"change"`, `"projection"`, `"estimate":true`} {
		if !strings.Contains(string(withTrend), key) {
			t.Errorf("JSON with a trend = %s, want %s", withTrend, key)
		}
	}
}

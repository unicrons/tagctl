package types

import "time"

// CostReport answers the question that gets tagging work funded: how much of
// this bill cannot be attributed to anyone?
type CostReport struct {
	// Start is the first day of the period covered, inclusive.
	Start time.Time `json:"start"`

	// End is the day after the period covered, exclusive, which is how Cost
	// Explorer expresses an interval.
	End time.Time `json:"end"`

	// Currency is the unit every amount is expressed in.
	Currency string `json:"currency"`

	// Total is the spend over the period.
	Total float64 `json:"total"`

	// Tags holds the attribution coverage per tag, keyed by tag name.
	Tags map[string]*TagCost `json:"tags"`
}

// TagCost is how much spend a single tag does and does not account for.
type TagCost struct {
	// Tag is the tag key.
	Tag string `json:"tag"`

	// Attributed is spend on resources carrying this tag.
	Attributed float64 `json:"attributed"`

	// Unattributed is spend on resources without it. This is the money that
	// cannot be assigned to an owner, an environment or a cost centre.
	Unattributed float64 `json:"unattributed"`

	// Values is the spend per tag value, most expensive first.
	Values []ValueCost `json:"values"`

	// Trend is the attribution per period, set only when a trend was requested.
	Trend *CostTrend `json:"trend,omitempty"`
}

// Total is the spend this tag was measured over.
func (t *TagCost) Total() float64 {
	return t.Attributed + t.Unattributed
}

// CoveragePct is the share of spend this tag accounts for.
func (t *TagCost) CoveragePct() float64 {
	total := t.Total()
	if total == 0 {
		return 0
	}
	return t.Attributed / total * 100
}

// ValueCost is the spend recorded against one value of a tag.
type ValueCost struct {
	// Value is the tag value, empty for spend carrying no value at all.
	Value string `json:"value"`

	// Amount is the spend attributed to it.
	Amount float64 `json:"amount"`
}

// WorstCoverage returns the tag accounting for the least spend, which is where
// attribution work pays off most. Returns nil when no tag was measured.
func (r *CostReport) WorstCoverage() *TagCost {
	var worst *TagCost

	for _, tag := range r.Tags {
		if worst == nil {
			worst = tag
			continue
		}
		// Break ties by name so the answer is stable across runs.
		if tag.CoveragePct() < worst.CoveragePct() ||
			(tag.CoveragePct() == worst.CoveragePct() && tag.Tag < worst.Tag) {
			worst = tag
		}
	}

	return worst
}

// Days is the length of the period covered.
func (r *CostReport) Days() int {
	return int(r.End.Sub(r.Start).Hours() / 24)
}

// CostGranularity is the length of the periods a cost trend is split into.
type CostGranularity string

// Supported trend granularities.
const (
	CostDaily  CostGranularity = "daily"
	CostWeekly CostGranularity = "weekly"
)

// days is the length of one period.
func (g CostGranularity) days() int {
	if g == CostWeekly {
		return 7
	}
	return 1
}

// CostPeriod is the spend one tag does and does not account for in one slice
// of the reported window.
type CostPeriod struct {
	// Start is the first day of the period, inclusive.
	Start time.Time `json:"start"`

	// End is the day after the period, exclusive.
	End time.Time `json:"end"`

	// Attributed is spend on resources carrying the tag.
	Attributed float64 `json:"attributed"`

	// Unattributed is spend on resources without it.
	Unattributed float64 `json:"unattributed"`

	// Partial marks a period shorter than the granularity. It is listed but
	// left out of the change and the projection.
	Partial bool `json:"partial,omitempty"`
}

// CoveragePct is the share of the period's spend the tag accounts for.
func (p CostPeriod) CoveragePct() float64 {
	total := p.Attributed + p.Unattributed
	if total == 0 {
		return 0
	}
	return p.Attributed / total * 100
}

// Days is the length of the period.
func (p CostPeriod) Days() int {
	return int(p.End.Sub(p.Start).Hours() / 24)
}

// Contains reports whether the day falls inside the period.
func (p CostPeriod) Contains(day time.Time) bool {
	return !day.Before(p.Start) && day.Before(p.End)
}

// CostChange is the movement between the first and the last full period.
type CostChange struct {
	// Unattributed is the change in unattributed spend, positive when it grew.
	Unattributed float64 `json:"unattributed"`

	// CoveragePoints is the change in coverage, in percentage points.
	CoveragePoints float64 `json:"coverage_points"`
}

// CostProjection is an estimate of the period that follows the window.
type CostProjection struct {
	// Start is the first day of the projected period, inclusive.
	Start time.Time `json:"start"`

	// End is the day after the projected period, exclusive.
	End time.Time `json:"end"`

	// Unattributed is the projected unattributed spend, never below zero.
	Unattributed float64 `json:"unattributed"`

	// Method names how the estimate was derived.
	Method string `json:"method"`

	// Estimate is always true, so the figure cannot be read as billed spend.
	Estimate bool `json:"estimate"`
}

// CostTrend is how a tag's attribution moved across the window.
type CostTrend struct {
	// Granularity is the length of each period.
	Granularity CostGranularity `json:"granularity"`

	// Periods are the slices of the window, oldest first.
	Periods []CostPeriod `json:"periods"`

	// Change compares the first and last full period. Nil with fewer than two.
	Change *CostChange `json:"change,omitempty"`

	// Projection estimates the next period. Nil with fewer than two full periods.
	Projection *CostProjection `json:"projection,omitempty"`
}

// CostPeriods splits [start, end) into empty periods of the given
// granularity. Periods are aligned to the end of the window, so a window that
// is not a whole number of periods starts with a partial one.
func CostPeriods(start, end time.Time, granularity CostGranularity) []CostPeriod {
	size := granularity.days()
	days := int(end.Sub(start).Hours() / 24)
	if days <= 0 {
		return nil
	}

	periods := make([]CostPeriod, 0, days/size+1)
	if rest := days % size; rest > 0 {
		first := CostPeriod{Start: start, End: start.AddDate(0, 0, rest), Partial: true}
		periods = append(periods, first)
		start = first.End
	}
	for start.Before(end) {
		next := start.AddDate(0, 0, size)
		periods = append(periods, CostPeriod{Start: start, End: next})
		start = next
	}
	return periods
}

// ProjectionLinear is a least-squares line through the full periods.
const ProjectionLinear = "linear"

// NewCostTrend derives the change and the projection from filled periods.
func NewCostTrend(granularity CostGranularity, periods []CostPeriod) *CostTrend {
	trend := &CostTrend{Granularity: granularity, Periods: periods}

	full := make([]CostPeriod, 0, len(periods))
	for _, period := range periods {
		if !period.Partial {
			full = append(full, period)
		}
	}
	if len(full) < 2 {
		return trend
	}

	first, last := full[0], full[len(full)-1]
	trend.Change = &CostChange{
		Unattributed:   last.Unattributed - first.Unattributed,
		CoveragePoints: last.CoveragePct() - first.CoveragePct(),
	}
	trend.Projection = &CostProjection{
		Start:        last.End,
		End:          last.End.AddDate(0, 0, granularity.days()),
		Unattributed: projectNext(full),
		Method:       ProjectionLinear,
		Estimate:     true,
	}
	return trend
}

// projectNext fits a least-squares line through the unattributed spend of the
// periods and reads it one period ahead.
func projectNext(periods []CostPeriod) float64 {
	n := float64(len(periods))
	meanX := (n - 1) / 2

	var meanY float64
	for _, period := range periods {
		meanY += period.Unattributed
	}
	meanY /= n

	var covariance, variance float64
	for i, period := range periods {
		dx := float64(i) - meanX
		covariance += dx * (period.Unattributed - meanY)
		variance += dx * dx
	}

	return max(0, meanY+covariance/variance*(n-meanX))
}

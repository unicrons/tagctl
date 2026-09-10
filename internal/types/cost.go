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

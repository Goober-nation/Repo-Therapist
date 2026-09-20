package scoring

// MetricResult is one metric's full outcome: the raw value (never lossy, always kept — see
// docs/grading-policy.md principle 3) alongside the clamped 0-1 score actually used for
// aggregation.
type MetricResult struct {
	ID     string `json:"id"`
	NoData bool   `json:"no_data"`
	// RawValue/Score are meaningless when NoData is true (both left at zero value) — callers
	// must check NoData first. Deliberately no `omitempty`: a real computed 0 (e.g. genuinely
	// zero commits/week) must stay visible, not be indistinguishable from an omitted/missing
	// field — the exact distinction docs/grading-policy.md principle 3 exists to preserve.
	RawValue float64 `json:"raw_value"`
	Score    float64 `json:"score"` // 0-1
	Weight   float64 `json:"weight"`
}

type CategoryResult struct {
	ID      string         `json:"id"`
	NoData  bool           `json:"no_data"`
	Score   float64        `json:"score"` // 0-100
	Metrics []MetricResult `json:"metrics"`
}

// combineWeighted renormalizes over whatever isn't NoData — a metric that's config-disabled
// (not_calculated, never even attempted) and one that's calculated but had no data for this repo
// both arrive here already marked NoData:true, and are treated identically: dropped, remaining
// weights renormalized to sum to 1. See docs/grading-policy.md principle 2.
func combineWeighted(results []MetricResult) (score float64, noData bool) {
	var totalWeight, weightedSum float64
	for _, r := range results {
		if r.NoData {
			continue
		}
		totalWeight += r.Weight
		weightedSum += r.Weight * r.Score
	}
	if totalWeight == 0 {
		return 0, true
	}
	return (weightedSum / totalWeight) * 100, false
}

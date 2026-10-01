package scoring

import (
	"slices"
	"strings"
)

// Recommendation is one deduction-reason -> pre-written-text pairing, generated while scoring —
// see docs/grading/00-overview.md's public/private split and config/recommendations.yaml for the
// catalog. Mirrors contracts.Recommendation (grade-service's own copy, kept dependency-free of
// the Kafka contract package — this one lives in the scoring package, close to CategoryResult).
type Recommendation struct {
	CategoryID string `json:"category_id"`
	MetricID   string `json:"metric_id"`
	Text       string `json:"text"`
}

// BuildRecommendations walks every category in categoryIDs and appends one Recommendation for
// every metric that's !NoData and scored below perfect (Score < 1.0) — "below healthy," not just
// "at critical," per 2026-09-25 decision: catches early warning signs, not just outright failures.
// A metric with no catalog entry is skipped, not fabricated — the catalog (config/recommendations.
// yaml) is expected to grow to cover every metric ID, not something this function should guess at.
// Categories absent from the map (not yet computed, e.g. not_calculated) are skipped, same as
// NoData ones — nothing to recommend without a real measurement.
func BuildRecommendations(categories map[string]CategoryResult, categoryIDs []string, catalog map[string]string) []Recommendation {
	var recs []Recommendation
	for _, catID := range categoryIDs {
		cat, ok := categories[catID]
		if !ok || cat.NoData {
			continue
		}
		for _, m := range cat.Metrics {
			if m.NoData || m.Score >= 1.0 {
				continue
			}
			text, ok := catalog[m.ID]
			if !ok {
				continue
			}
			recs = append(recs, Recommendation{CategoryID: catID, MetricID: m.ID, Text: text})
		}
	}
	return recs
}

// DocsAdviceMetricID and DocsAdviceSeverity label the LLM service's Documentation advice in the
// shared pool: its texts carry no metric code or severity of their own (2026-09-29 decision).
const (
	DocsAdviceMetricID = "llm_advice"
	DocsAdviceSeverity = "[medium] "
)

// DocsRecommendations turns the LLM service's Documentation advice into pool entries, in order.
// Nothing is added when Documentation isn't in categoryIDs or has no data — advice from a reply
// that wasn't used for the score would describe a grade the user never sees.
func DocsRecommendations(categories map[string]CategoryResult, categoryIDs []string, advice []string) []Recommendation {
	cat, ok := categories["documentation"]
	if !ok || cat.NoData || !slices.Contains(categoryIDs, "documentation") {
		return nil
	}
	recs := make([]Recommendation, 0, len(advice))
	for _, text := range advice {
		if text = strings.TrimSpace(text); text != "" {
			recs = append(recs, Recommendation{CategoryID: "documentation", MetricID: DocsAdviceMetricID, Text: DocsAdviceSeverity + text})
		}
	}
	return recs
}

// CombineCategories is combineWeighted (the same NoData-drop-and-renormalize logic every category
// already uses internally for its own metrics) applied one level up, across categories — the
// public/private score split is structurally identical to a category combining its metrics, so it
// reuses the exact same function rather than a parallel implementation. weights should be each
// category's own config/grading.yaml `weight` (already summing to 1.0 across all six; a public-
// only subset's weights don't need pre-renormalizing — combineWeighted does that regardless of
// what the subset's weights sum to). A categoryID missing from categories (not yet computed, e.g.
// not_calculated) is treated as NoData, same as one present but NoData.
func CombineCategories(categories map[string]CategoryResult, categoryIDs []string, weights map[string]float64) (score float64, noData bool) {
	results := make([]MetricResult, 0, len(categoryIDs))
	for _, id := range categoryIDs {
		cat, ok := categories[id]
		if !ok {
			cat = CategoryResult{ID: id, NoData: true}
		}
		results = append(results, MetricResult{
			ID:     id,
			NoData: cat.NoData,
			Score:  cat.Score / 100, // CategoryResult.Score is 0-100; combineWeighted works in 0-1
			Weight: weights[id],
		})
	}
	return combineWeighted(results)
}

// MissingCategories names every category in categoryIDs that's NoData or wasn't computed at all —
// GradingResultEvent.MissingData, telling the Aggregator/frontend which parts of the picture are
// incomplete, distinct from a real (possibly harsh) score.
func MissingCategories(categories map[string]CategoryResult, categoryIDs []string) []string {
	var missing []string
	for _, id := range categoryIDs {
		cat, ok := categories[id]
		if !ok || cat.NoData {
			missing = append(missing, id)
		}
	}
	return missing
}

// SubsetCategories returns only the named categories — used to build public_category_scores
// (just the public-view categories) separately from private_category_scores (all six), so the
// public JSON blob never carries a private-only category's detail even opaquely.
func SubsetCategories(categories map[string]CategoryResult, categoryIDs []string) map[string]CategoryResult {
	subset := make(map[string]CategoryResult, len(categoryIDs))
	for _, id := range categoryIDs {
		if cat, ok := categories[id]; ok {
			subset[id] = cat
		}
	}
	return subset
}

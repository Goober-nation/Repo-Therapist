package scoring

import (
	"sort"

	"repo-therapist/grade-service/internal/contracts"
)

// ComputeDocumentation maps the LLM service's reply onto a CategoryResult. The LLM owns the
// method, so its grade is taken as the category score unchanged; subgrades are listed as metrics
// for display only (equal weights, not used to recompute the score). A nil reply (timed out,
// disabled, never arrived) or a failure reply (nonempty Error, or null grade) is NoData for the
// whole section, subgrades included — never a fabricated score.
func ComputeDocumentation(reply *contracts.DocsGraded) CategoryResult {
	if reply == nil || reply.Error != "" || reply.Grade == nil {
		return CategoryResult{ID: "documentation", NoData: true}
	}
	ids := make([]string, 0, len(reply.Subgrades))
	for id := range reply.Subgrades {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	metrics := make([]MetricResult, 0, len(ids))
	for _, id := range ids {
		v := reply.Subgrades[id]
		metrics = append(metrics, MetricResult{ID: id, RawValue: float64(v), Score: float64(v) / 100, Weight: 1 / float64(len(ids))})
	}
	return CategoryResult{ID: "documentation", Score: float64(*reply.Grade), Metrics: metrics}
}

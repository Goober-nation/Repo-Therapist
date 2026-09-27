package scoring

import (
	"sort"
	"time"

	"repo-therapist/grade-service/internal/config"
)

// CIRun matches one entry of repository-service's _meta/ci-runs.json `runs` array. Timestamps are
// RFC3339 strings, "" when absent (repository-service already blanks SourceCraft's 1970-01-01
// placeholder dates on queued runs).
type CIRun struct {
	Slug       string `json:"slug"`
	Status     string `json:"status"`
	EventType  string `json:"event_type"`
	CreatedAt  string `json:"created_at"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
}

// CIExport is the full _meta/ci-runs.json shape.
type CIExport struct {
	ConfigFiles   []string `json:"config_files"`
	RunsAvailable bool     `json:"runs_available"`
	Runs          []CIRun  `json:"runs"`
}

// CICDInputs: Available=false means ci-runs.json itself couldn't be read (whole category NoData),
// same pattern as CodeHealthInputs.
type CICDInputs struct {
	Export    CIExport
	Available bool
	// ConfigContents maps each Export.ConfigFiles path to its bytes, when readable. A path
	// missing here just skips the no-op check for that file.
	ConfigContents map[string][]byte
}

// noOpMetricID is the zero-weight marker metric a no-op pipeline gets, so the verdict is
// visible in category_scores and picks up its recommendation like any other metric.
const noOpMetricID = "ci_noop_pipeline"

// Run.Status outcomes. Only these count toward success_rate/stability_trend: canceled, skipped,
// awaiting_approval and rejected say nothing about whether the code passed, and created/prepared/
// processing haven't finished yet (see docs/grading/ci-cd.md).
var (
	ciPassStatuses = map[string]bool{"success": true}
	ciFailStatuses = map[string]bool{"failed": true, "timeout": true}
)

// ComputeCICD scores CI/CD from ci-runs.json (docs/grading/ci-cd.md). Like Code health, raw
// values are bespoke snapshot statistics while weights/thresholds come from config/grading.yaml.
//
// Category-level rules, in order:
//  1. export unavailable, or runs not readable (token doesn't own the repo) → NoData.
//  2. no config file AND zero runs → hard gate: category score 0 (the has_ci metric fails).
//  3. config file does no real work (IsNoOpCIConfig) → category score 0.
//  4. config file but zero runs → NoData (CI set up, never observed running — nothing to score).
//  5. otherwise → metrics computed normally, each with its own min_sample_size floor.
func ComputeCICD(cfg config.CategoryConfig, inputs CICDInputs) CategoryResult {
	if cfg.Status != config.StatusCalculated || !inputs.Available || !inputs.Export.RunsAvailable {
		return CategoryResult{ID: "ci_cd", NoData: true}
	}
	ex := inputs.Export
	if isNoOpPipeline(ex.ConfigFiles, inputs.ConfigContents) {
		return CategoryResult{ID: "ci_cd", Score: 0, Metrics: []MetricResult{{ID: noOpMetricID, RawValue: 1}}}
	}
	if len(ex.Runs) == 0 && len(ex.ConfigFiles) > 0 {
		return CategoryResult{ID: "ci_cd", NoData: true}
	}

	finished := finishedRunsOldestFirst(ex.Runs)
	results := make([]MetricResult, 0, len(cfg.Metrics))
	gateFailed := false
	for _, m := range cfg.Metrics {
		r := computeCICDMetric(m, ex, finished)
		if m.Formula.Type == "gate" && !r.NoData && r.Score == 0 {
			gateFailed = true
		}
		results = append(results, r)
	}

	// A failed gate zeroes the whole category regardless of other metrics — the "gate" formula
	// side effect formula.go describes. Only CI/CD uses a gate so far, so it lives here rather
	// than in combineWeighted.
	if gateFailed {
		return CategoryResult{ID: "ci_cd", Score: 0, Metrics: results}
	}
	score, noData := combineWeighted(results)
	return CategoryResult{ID: "ci_cd", Score: score * ciSuccessMultiplier(results), NoData: noData, Metrics: results}
}

// isNoOpPipeline: every readable config file is a no-op, and at least one was read.
func isNoOpPipeline(paths []string, contents map[string][]byte) bool {
	read := 0
	for _, p := range paths {
		data, ok := contents[p]
		if !ok {
			continue
		}
		if !IsNoOpCIConfig(data) {
			return false
		}
		read++
	}
	return read > 0
}

// ciSuccessFloor: the weighted score is multiplied by ciSuccessFloor + (1-ciSuccessFloor)·s, where
// s is ci_success_rate's 0-1 score. A pipeline that never passes is capped at 0.4× — an
// always-red but stable, fast pipeline lands at 20 instead of 50 — while stability trend and
// duration keep a proportional effect at every success level, instead of being made pointless by
// a much larger success-rate weight (decided 2026-09-26, docs/grading/ci-cd.md).
const ciSuccessFloor = 0.4

// ciSuccessMultiplier returns 1 (no scaling) when ci_success_rate is NoData or absent — nothing
// known about pass/fail, so nothing to scale by.
func ciSuccessMultiplier(results []MetricResult) float64 {
	for _, r := range results {
		if r.ID == "ci_success_rate" && !r.NoData {
			return ciSuccessFloor + (1-ciSuccessFloor)*r.Score
		}
	}
	return 1
}

func computeCICDMetric(m config.MetricConfig, ex CIExport, finished []CIRun) MetricResult {
	base := MetricResult{ID: m.ID, Weight: m.Weight}
	if m.Status != config.StatusCalculated {
		base.NoData = true
		return base
	}

	var agg aggResult
	switch m.Aggregation.Type {
	case "ci_has_ci":
		agg = aggResult{Value: 0}
		if len(ex.ConfigFiles) > 0 || len(ex.Runs) > 0 {
			agg.Value = 1
		}
	case "ci_success_rate":
		agg = ciSuccessRate(m.Aggregation, finished)
	case "ci_stability_trend":
		agg = ciStabilityTrend(m.Aggregation, finished)
	case "ci_median_duration_min":
		agg = ciMedianDurationMin(m.Aggregation, finished)
	default:
		agg = aggResult{NoData: true}
	}
	if agg.NoData {
		base.NoData = true
		return base
	}
	base.RawValue = agg.Value
	base.Score = applyFormula(m.Formula, agg.Value)
	return base
}

// finishedRunsOldestFirst keeps runs with a pass/fail outcome and a parseable created_at, sorted
// oldest → newest (stability_trend needs chronological halves).
func finishedRunsOldestFirst(runs []CIRun) []CIRun {
	type dated struct {
		run CIRun
		at  time.Time
	}
	kept := make([]dated, 0, len(runs))
	for _, r := range runs {
		if !ciPassStatuses[r.Status] && !ciFailStatuses[r.Status] {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, r.CreatedAt)
		if err != nil {
			continue
		}
		kept = append(kept, dated{r, at})
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].at.Before(kept[j].at) })
	out := make([]CIRun, len(kept))
	for i, d := range kept {
		out[i] = d.run
	}
	return out
}

func passShare(runs []CIRun) float64 {
	var pass int
	for _, r := range runs {
		if ciPassStatuses[r.Status] {
			pass++
		}
	}
	return float64(pass) / float64(len(runs))
}

// ciSuccessRate: share of finished runs that passed.
func ciSuccessRate(cfg config.AggregationConfig, finished []CIRun) aggResult {
	if len(finished) == 0 || len(finished) < cfg.MinSampleSize {
		return aggResult{NoData: true}
	}
	return aggResult{Value: passShare(finished)}
}

// ciStabilityTrend: newer half's pass share minus older half's, in [-1, 1]. With an odd count the
// middle run is left out so both halves are equal size. Negative = getting less stable.
func ciStabilityTrend(cfg config.AggregationConfig, finished []CIRun) aggResult {
	half := len(finished) / 2
	if half == 0 || len(finished) < cfg.MinSampleSize {
		return aggResult{NoData: true}
	}
	older := finished[:half]
	newer := finished[len(finished)-half:]
	return aggResult{Value: passShare(newer) - passShare(older)}
}

// ciMedianDurationMin: median finished_at − started_at in minutes, over finished runs that have
// both timestamps. The sample floor counts only runs with a usable duration.
func ciMedianDurationMin(cfg config.AggregationConfig, finished []CIRun) aggResult {
	durations := make([]float64, 0, len(finished))
	for _, r := range finished {
		start, err1 := time.Parse(time.RFC3339Nano, r.StartedAt)
		end, err2 := time.Parse(time.RFC3339Nano, r.FinishedAt)
		if err1 != nil || err2 != nil || end.Before(start) {
			continue
		}
		durations = append(durations, end.Sub(start).Minutes())
	}
	if len(durations) == 0 || len(durations) < cfg.MinSampleSize {
		return aggResult{NoData: true}
	}
	return aggResult{Value: median(durations)}
}

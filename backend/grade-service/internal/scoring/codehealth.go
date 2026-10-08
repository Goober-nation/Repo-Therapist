package scoring

import (
	"fmt"
	"sort"
	"strings"

	"repo-therapist/grade-service/internal/config"
)

// Marker is the subset of repository-service's todomarkers.Marker Code health needs — matches the
// shape written to _meta/todo-markers.json.
type Marker struct {
	File      string  `json:"file"`
	Line      int     `json:"line"`
	Type      string  `json:"type"`
	AgeDays   float64 `json:"age_days"`
	AgeCapped bool    `json:"age_capped"`
}

// CodeHealthInputs bundles the single S3 source Code health draws from. Available distinguishes
// "fetched fine, repo genuinely has 0 TODOs" (a real, scoreable result) from "couldn't fetch
// todo-markers.json at all" (whole category NoData) — same pattern as ActivityInputs' *Available
// flags.
type CodeHealthInputs struct {
	Markers  []Marker
	TotalLOC int
	// ScannedFiles is the number of scanned text files; only the low-confidence flag reads it.
	ScannedFiles int
	Available    bool
	// DeadCode is _meta/dead-functions.json; nil when it couldn't be fetched, which makes only
	// unused_private_share no-data, not the whole category.
	DeadCode *DeadCodeExport
}

// DeadFunction / DeadCodeExport mirror repository-service's deadcode.Function / deadcode.Export.
type DeadFunction struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Name string `json:"name"`
	Lang string `json:"lang"`
}

type DeadCodeExport struct {
	Unused       []DeadFunction `json:"unused"`
	UnusedTotal  int            `json:"unused_total"`
	PrivateTotal int            `json:"private_total"`
}

// ComputeCodeHealth doesn't go through ComputeCategory/eventsForSource like Activity/Issues: its
// raw values (median age, density, a stale share, worst-file concentration) are snapshot
// statistics over the whole marker list, not per-event rates to window — there's no meaningful
// "aggregation over timestamped events" here, same reasoning security.go documents for Security.
// Unlike Security, though, weights/thresholds still come from config/grading.yaml (matching
// Activity/Issues) since each metric maps cleanly onto the existing linear_clamp formula config —
// only the raw-value computation itself is bespoke Go, not table-driven.
//
// A per-repo commit-cadence-relative version of median_marker_age/share_stale_markers'
// thresholds was tried and reverted (2026-09-25) — real data showed age doesn't scale
// proportionally with cadence either: the age/cadence ratio varied 64x across repos tested, and
// using it would have penalized the fastest-moving, most actively-maintained repos hardest for
// the same or better absolute TODO age than slower ones. See docs/grading/code-health.md's Design
// history. Flat, empirically-calibrated thresholds (config/grading.yaml) are what's used instead.
func ComputeCodeHealth(cfg config.CategoryConfig, inputs CodeHealthInputs) CategoryResult {
	if cfg.Status != config.StatusCalculated || !inputs.Available {
		return CategoryResult{ID: "code_health", NoData: true}
	}

	results := make([]MetricResult, 0, len(cfg.Metrics))
	for _, m := range cfg.Metrics {
		results = append(results, computeCodeHealthMetric(m, inputs))
	}

	score, noData := combineWeighted(results)
	return CategoryResult{ID: "code_health", Score: score, NoData: noData, Metrics: results}
}

func computeCodeHealthMetric(m config.MetricConfig, inputs CodeHealthInputs) MetricResult {
	base := MetricResult{ID: m.ID, Weight: m.Weight}
	if m.Status != config.StatusCalculated {
		base.NoData = true
		return base
	}

	agg := aggregateCodeHealth(m, inputs)
	if agg.NoData {
		base.NoData = true
		return base
	}

	base.RawValue = agg.Value
	base.Score = applyFormula(m.Formula, agg.Value)
	if m.Aggregation.Type == "unused_private_share" {
		base.Detail = deadCodeAdvice(inputs.DeadCode)
	}
	return base
}

func aggregateCodeHealth(m config.MetricConfig, inputs CodeHealthInputs) aggResult {
	switch m.Aggregation.Type {
	case "median_marker_age":
		return medianMarkerAge(m.Aggregation, inputs.Markers)
	case "density_per_kloc":
		return densityPerKLOC(inputs)
	case "share_stale_markers":
		return shareStaleMarkers(m.Aggregation, inputs.Markers)
	case "worst_file_concentration":
		return worstFileConcentration(m.Aggregation, inputs.Markers)
	case "unused_private_share":
		return unusedPrivateShare(m.Aggregation, inputs.DeadCode)
	default:
		return aggResult{NoData: true}
	}
}

// uncappedMarkers filters out markers whose AgeDays was capped at the shallow-clone window
// (Marker.AgeCapped) — their true age is unknown beyond "at least this old," not a real measured
// value. Feeding a capped age into an age-sensitive statistic would score "we don't know" as "very
// old," exactly the no-data-treated-as-bad failure this whole grading policy is built to avoid
// (00-overview.md principle 2) — discovered empirically 2026-09-25 testing real repos: postgres,
// divkit, diktat and most other established codebases had 90-100% of their markers capped, since
// almost none of their TODOs were touched within the clone's --shallow-since window. Only
// medianMarkerAge and shareStaleMarkers need this — density_per_kloc and worst_file_concentration
// don't depend on age at all.
func uncappedMarkers(markers []Marker) []Marker {
	out := make([]Marker, 0, len(markers))
	for _, m := range markers {
		if !m.AgeCapped {
			out = append(out, m)
		}
	}
	return out
}

// medianMarkerAge: median AgeDays over markers with a confidently known age (see
// uncappedMarkers). Undefined with 0 such markers (nothing to take a median of) — below
// cfg.MinSampleSize uncapped markers, NoData, same small-sample floor Activity/Issues use
// elsewhere (a median over 1-4 markers swings too far on a single one). The floor applies to the
// uncapped count, not the raw marker count — a repo with 50 markers all capped has 0 usable data
// points for this metric, not 50.
func medianMarkerAge(cfg config.AggregationConfig, markers []Marker) aggResult {
	known := uncappedMarkers(markers)
	if len(known) < cfg.MinSampleSize {
		return aggResult{NoData: true}
	}
	ages := make([]float64, len(known))
	for i, m := range known {
		ages[i] = m.AgeDays
	}
	return aggResult{Value: median(ages)}
}

// densityPerKLOC: marker count per 1000 lines of scanned code — deliberately the only Code health
// metric with no min_sample_size floor. 0 markers over a real, non-empty LOC count is a real,
// good (0) density, not NoData (see docs/grading/code-health.md). Only undefined when TotalLOC is
// itself 0 (e.g. a repo of nothing but binaries/vendor) — genuinely nothing to divide by, not a
// small-sample problem.
func densityPerKLOC(inputs CodeHealthInputs) aggResult {
	if inputs.TotalLOC == 0 {
		return aggResult{NoData: true}
	}
	return aggResult{Value: float64(len(inputs.Markers)) / (float64(inputs.TotalLOC) / 1000)}
}

// shareStaleMarkers: fraction of markers whose AgeDays is at least cfg.StalenessDays — a ratio,
// deliberately replacing an earlier "oldest marker age" (raw max) design. A raw max is an
// order statistic: with more markers sampled from the same age distribution, the max skews
// older purely from sample size, which would systematically penalize large repos over small ones
// for no real quality difference. A share is normalized by construction, same statistical
// pattern as Issues' abandoned_share.
//
// cfg.StalenessDays is a flat, empirically-calibrated cutoff (config/grading.yaml) — a per-repo
// commit-cadence-relative version was tried and reverted, see ComputeCodeHealth's comment.
//
// Computed over uncappedMarkers only (see that function) — a capped marker's age is an unknown
// lower bound, not a confirmed >=StalenessDays measurement, so counting it as "stale" would
// silently convert "we don't know" into "this is bad." Below cfg.MinSampleSize uncapped markers
// → NoData, same reasoning as medianMarkerAge.
func shareStaleMarkers(cfg config.AggregationConfig, markers []Marker) aggResult {
	known := uncappedMarkers(markers)
	if len(known) < cfg.MinSampleSize {
		return aggResult{NoData: true}
	}
	var stale int
	for _, m := range known {
		if m.AgeDays >= float64(cfg.StalenessDays) {
			stale++
		}
	}
	return aggResult{Value: float64(stale) / float64(len(known))}
}

// worstFileConcentration: the single worst file's share of all markers — % of all markers found
// in one file. Undefined with 0 markers (nothing to concentrate) — below cfg.MinSampleSize
// markers, NoData, same reasoning as the other marker-count-dependent metrics here.
func worstFileConcentration(cfg config.AggregationConfig, markers []Marker) aggResult {
	if len(markers) < cfg.MinSampleSize {
		return aggResult{NoData: true}
	}
	counts := make(map[string]int)
	for _, m := range markers {
		counts[m.File]++
	}
	var max int
	for _, c := range counts {
		if c > max {
			max = c
		}
	}
	return aggResult{Value: float64(max) / float64(len(markers))}
}

// unusedPrivateShare: unused private functions ÷ all private functions in supported languages.
// Private only, since a public function may be called from outside the repo. Below
// cfg.MinSampleSize private functions → NoData: in a tiny codebase one helper swings the share
// too far. Also NoData when the export is missing.
func unusedPrivateShare(cfg config.AggregationConfig, dead *DeadCodeExport) aggResult {
	if dead == nil || dead.PrivateTotal == 0 || dead.PrivateTotal < cfg.MinSampleSize {
		return aggResult{NoData: true}
	}
	return aggResult{Value: float64(dead.UnusedTotal) / float64(dead.PrivateTotal)}
}

// deadCodeAdviceFiles is how many files the advice names before "и других".
const deadCodeAdviceFiles = 3

// deadCodeAdvice renders the Russian recommendation naming the files with the most unused
// functions; "" when nothing was found.
func deadCodeAdvice(dead *DeadCodeExport) string {
	if dead == nil || dead.UnusedTotal == 0 {
		return ""
	}
	counts := make(map[string]int)
	for _, f := range dead.Unused {
		counts[f.File]++
	}
	files := make([]string, 0, len(counts))
	for f := range counts {
		files = append(files, f)
	}
	sort.Slice(files, func(i, j int) bool {
		if counts[files[i]] != counts[files[j]] {
			return counts[files[i]] > counts[files[j]]
		}
		return files[i] < files[j]
	})
	more := len(files) > deadCodeAdviceFiles
	if more {
		files = files[:deadCodeAdviceFiles]
	}
	where := "в файле"
	if len(files) > 1 {
		where = "в файлах"
	}
	text := fmt.Sprintf("Найдено %d %s %s %s", dead.UnusedTotal,
		ruPlural(dead.UnusedTotal, "неиспользуемая приватная функция", "неиспользуемые приватные функции", "неиспользуемых приватных функций"),
		where, strings.Join(files, ", "))
	if more {
		text += " и других"
	}
	return text + ". Удалите мёртвый код или используйте его — он усложняет чтение и сопровождение."
}

// ruPlural picks the Russian noun form for n (1 функция, 2 функции, 5 функций).
func ruPlural(n int, one, few, many string) string {
	n %= 100
	if n >= 11 && n <= 14 {
		return many
	}
	switch n % 10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	}
	return many
}

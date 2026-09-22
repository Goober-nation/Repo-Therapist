package scoring

import (
	"time"

	"repo-therapist/grade-service/internal/appsecwrapper"
)

// Security doesn't go through ComputeCategory/eventsForSource like Activity: its input isn't a
// stream of timestamped S3 events to aggregate, it's a single already-aggregated snapshot
// (AppsecResult, one gRPC call) — source/aggregation config has nothing to plug into. Metric
// weights/thresholds are hardcoded here instead of config/grading.yaml for the same reason; only
// the category's own weight/status (config/grading.yaml's `security:` block) is config-driven.
//
// docs/grading-policy.md's Security section: open critical/high/medium/low counts plus days since
// last scan, and HasScan=false must report "Нет данных" (NoData:true), never a fabricated 0.
const (
	securityWeightCritical = 0.35
	securityWeightHigh     = 0.25
	securityWeightMedium   = 0.15
	securityWeightLow      = 0.10
	securityWeightRecency  = 0.15
)

// ComputeSecurity turns one AppsecResult into Security's weighted 0-100 category score. All
// thresholds are provisional (first-guess, not yet validated against real repos), same caveat as
// activity.go's config-driven metrics carry via their `provisional: true` flags.
func ComputeSecurity(result appsecwrapper.AppsecResult, now time.Time) CategoryResult {
	if !result.HasScan {
		return CategoryResult{ID: "security", NoData: true}
	}

	metrics := []MetricResult{
		severityMetric("open_critical", securityWeightCritical, result.Open.Critical, 5, 0),
		severityMetric("open_high", securityWeightHigh, result.Open.High, 15, 0),
		severityMetric("open_medium", securityWeightMedium, result.Open.Medium, 40, 0),
		severityMetric("open_low", securityWeightLow, result.Open.Low, 80, 0),
		daysSinceLastScanMetric(securityWeightRecency, result.Scan.TimeFinished, now),
	}

	score, noData := combineWeighted(metrics)
	return CategoryResult{ID: "security", Score: score, NoData: noData, Metrics: metrics}
}

// severityMetric scores an open-defect-group count for one severity tier: `critical` open groups
// is as bad as it gets (score 0), `healthy` (0) open groups is perfect — direction "-", fewer is
// better, via the same linearClamp every other metric in the codebase uses.
func severityMetric(id string, weight float64, count int64, critical, healthy float64) MetricResult {
	x := float64(count)
	return MetricResult{
		ID:       id,
		Weight:   weight,
		RawValue: x,
		Score:    linearClamp(x, critical, healthy),
	}
}

// daysSinceLastScanMetric: a scan older than 90 days is as bad as never having scanned recently;
// same-day is perfect.
func daysSinceLastScanMetric(weight float64, timeFinished time.Time, now time.Time) MetricResult {
	if timeFinished.IsZero() {
		return MetricResult{ID: "days_since_last_scan", Weight: weight, NoData: true}
	}
	days := now.Sub(timeFinished).Hours() / 24
	return MetricResult{
		ID:       "days_since_last_scan",
		Weight:   weight,
		RawValue: days,
		Score:    linearClamp(days, 90, 0),
	}
}

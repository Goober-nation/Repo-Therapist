package scoring

import "repo-therapist/grade-service/internal/config"

// applyFormula maps an aggregated raw value to a score in [0,1]. This is the one place every
// metric's "value -> score" logic lives, regardless of category — see docs/grading-policy.md
// principle 1 for why a single formula family (vs. the original draft's mix of log/linear) matters.
func applyFormula(cfg config.FormulaConfig, x float64) float64 {
	switch cfg.Type {
	case "linear_clamp":
		return linearClamp(x, cfg.Critical, cfg.Healthy)
	case "band":
		return band(x, cfg.HealthyLow, cfg.HealthyHigh, cfg.CriticalLow, cfg.CriticalHigh)
	case "gate":
		// Pass-through: x is expected to already be 0 or 1. combine.go does not yet implement
		// gate's "zero the whole category on failure" side effect (no calculated metric uses
		// this formula type yet — CI/CD's has_ci gate, when built, will need it). Tracked in
		// backlog/phase-2-grade-service.md, not silently forgotten.
		return clamp01(x)
	default:
		return 0
	}
}

// linearClamp: score = clamp((x - critical) / (healthy - critical), 0, 1). Works for both
// directions purely by which of critical/healthy is numerically larger — no separate sign branch.
func linearClamp(x, critical, healthy float64) float64 {
	if healthy == critical {
		return 0
	}
	return clamp01((x - critical) / (healthy - critical))
}

// band: score = 1 inside [healthyLow, healthyHigh], linear falloff to 0 at criticalLow/criticalHigh
// outside that. Used for two-sided metrics (e.g. change dynamics) where both too little and too
// much are bad.
func band(x, healthyLow, healthyHigh, criticalLow, criticalHigh float64) float64 {
	switch {
	case x >= healthyLow && x <= healthyHigh:
		return 1
	case x < healthyLow:
		return clamp01((x - criticalLow) / (healthyLow - criticalLow))
	default: // x > healthyHigh
		return clamp01((criticalHigh - x) / (criticalHigh - healthyHigh))
	}
}

func clamp01(x float64) float64 {
	switch {
	case x < 0:
		return 0
	case x > 1:
		return 1
	default:
		return x
	}
}

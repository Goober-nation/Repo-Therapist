package scoring

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"repo-therapist/grade-service/internal/config"
)

// ciCfg loads the real config/grading.yaml ci_cd block, so these tests pin the thresholds that
// actually ship — a threshold change must come with updated expectations here.
func ciCfg(t *testing.T) config.CategoryConfig {
	t.Helper()
	cfg, err := config.LoadConfig(filepath.Join("..", "..", "..", "..", "config", "grading.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cat, ok := cfg.Categories["ci_cd"]
	if !ok {
		t.Fatal("config has no ci_cd category")
	}
	return cat
}

func loadCIFixture(t *testing.T, name string) CICDInputs {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "ci", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var ex CIExport
	if err := json.Unmarshal(data, &ex); err != nil {
		t.Fatal(err)
	}
	return CICDInputs{Export: ex, Available: true}
}

// noData marks a metric expected to be NoData in a case's metric table.
const noData = -1.0

// TestComputeCICD_Fixtures runs each testdata/ci/*.json export (the same shape repository-service
// writes to _meta/ci-runs.json) through ComputeCICD. Fixture runs are 2 minutes long unless the
// case says otherwise. Expected scores follow from config/grading.yaml's ci_cd thresholds:
// success_rate 0.5→0.9, stability_trend -0.3→0, median_duration_min 30→5; weights 0.5/0.3/0.2;
// the weighted score is then multiplied by 0.4 + 0.6·(success_rate score).
func TestComputeCICD_Fixtures(t *testing.T) {
	cases := []struct {
		fixture     string
		wantNoData  bool
		wantScore   float64
		wantMetrics map[string]float64 // metric ID -> expected 0-1 score, or noData
	}{
		{"green", false, 100, map[string]float64{"has_ci": 1, "ci_success_rate": 1, "ci_stability_trend": 1, "ci_median_duration_min": 1}},
		// Always failing but stable and fast: weighted 50, × 0.4 success multiplier = 20.
		{"red", false, 20, map[string]float64{"ci_success_rate": 0, "ci_stability_trend": 1, "ci_median_duration_min": 1}},
		// S S S F | S F F F: 50% pass, trend 0.25-0.75 = -0.5. Weighted 20 × 0.4.
		{"flaky_degrading", false, 8, map[string]float64{"ci_success_rate": 0, "ci_stability_trend": 0, "ci_median_duration_min": 1}},
		// F F F F | S S S S: 50% pass, trend +1 clamps to 1. Weighted 50 × 0.4.
		{"improving", false, 20, map[string]float64{"ci_success_rate": 0, "ci_stability_trend": 1, "ci_median_duration_min": 1}},
		{"dormant", true, 0, nil},
		{"no_ci", false, 0, map[string]float64{"has_ci": 0, "ci_success_rate": noData, "ci_stability_trend": noData, "ci_median_duration_min": noData}},
		{"runs_denied", true, 0, nil},
		{"too_few_runs", true, 0, map[string]float64{"has_ci": 1, "ci_success_rate": noData, "ci_stability_trend": noData, "ci_median_duration_min": noData}},
		{"queued_only", true, 0, nil},
		// canceled/skipped/processing ignored → S S F S S: 80% pass (0.75), 5 finished < 6 so no
		// trend, 10-minute runs (0.8). (0.5*0.75 + 0.2*0.8) / 0.7 = 76.43, × (0.4 + 0.6*0.75).
		{"mixed_statuses", false, 64.9643, map[string]float64{"ci_success_rate": 0.75, "ci_stability_trend": noData, "ci_median_duration_min": 0.8}},
		{"runs_without_config", false, 100, map[string]float64{"has_ci": 1}},
		// Only 2 of 4 runs have start/finish times: below the duration floor of 3.
		{"missing_durations", false, 100, map[string]float64{"ci_success_rate": 1, "ci_median_duration_min": noData}},
		{"slow", false, 80, map[string]float64{"ci_median_duration_min": 0}},
		// S S T S: timeout is a failure → 75% pass (0.625). (0.5*0.625 + 0.2*1) / 0.7 = 73.21,
		// × (0.4 + 0.6*0.625).
		{"timeout_counts_as_fail", false, 56.7411, map[string]float64{"ci_success_rate": 0.625}},
	}

	cfg := ciCfg(t)
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			got := ComputeCICD(cfg, loadCIFixture(t, tc.fixture))
			if got.NoData != tc.wantNoData {
				t.Fatalf("NoData = %v, want %v (%+v)", got.NoData, tc.wantNoData, got)
			}
			if !tc.wantNoData && math.Abs(got.Score-tc.wantScore) > 0.01 {
				t.Errorf("Score = %.4f, want %.4f", got.Score, tc.wantScore)
			}
			byID := make(map[string]MetricResult, len(got.Metrics))
			for _, m := range got.Metrics {
				byID[m.ID] = m
			}
			for id, want := range tc.wantMetrics {
				m, ok := byID[id]
				if !ok {
					t.Errorf("metric %s missing", id)
					continue
				}
				if want == noData {
					if !m.NoData {
						t.Errorf("metric %s: want NoData, got score %.4f (raw %.4f)", id, m.Score, m.RawValue)
					}
					continue
				}
				if m.NoData || math.Abs(m.Score-want) > 0.0001 {
					t.Errorf("metric %s: score %.4f (NoData=%v), want %.4f", id, m.Score, m.NoData, want)
				}
			}
		})
	}
}

func TestComputeCICD_Unavailable(t *testing.T) {
	if got := ComputeCICD(ciCfg(t), CICDInputs{Available: false}); !got.NoData {
		t.Fatal("expected NoData when ci-runs.json wasn't available")
	}
}

func TestComputeCICD_NotCalculated(t *testing.T) {
	cfg := ciCfg(t)
	cfg.Status = config.StatusNotCalculated
	if got := ComputeCICD(cfg, loadCIFixture(t, "green")); !got.NoData {
		t.Fatal("expected NoData when category status is not_calculated")
	}
}

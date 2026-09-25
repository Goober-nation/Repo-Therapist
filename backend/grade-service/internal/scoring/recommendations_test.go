package scoring

import "testing"

func TestCombineCategories_DropsNoDataAndRenormalizes(t *testing.T) {
	categories := map[string]CategoryResult{
		"activity":    {ID: "activity", Score: 80},
		"issues":      {ID: "issues", Score: 40},
		"code_health": {ID: "code_health", NoData: true},
		// "documentation" absent entirely — not_calculated, never in the map at all.
	}
	weights := map[string]float64{"activity": 0.15, "documentation": 0.15, "issues": 0.15, "code_health": 0.20}

	score, noData := CombineCategories(categories, []string{"activity", "documentation", "issues", "code_health"}, weights)
	if noData {
		t.Fatal("expected a real score: activity and issues both have data")
	}
	// Only activity (0.15) and issues (0.15) have data; renormalized weight is 0.5/0.5.
	want := (0.15*0.80 + 0.15*0.40) / 0.30 * 100
	if diff := score - want; diff > 0.001 || diff < -0.001 {
		t.Errorf("score = %v, want %v", score, want)
	}
}

func TestCombineCategories_AllNoDataYieldsNoData(t *testing.T) {
	categories := map[string]CategoryResult{
		"activity": {ID: "activity", NoData: true},
	}
	weights := map[string]float64{"activity": 0.15}

	_, noData := CombineCategories(categories, []string{"activity"}, weights)
	if !noData {
		t.Error("expected NoData when every category in the list is NoData")
	}
}

func TestMissingCategories(t *testing.T) {
	categories := map[string]CategoryResult{
		"activity": {ID: "activity", Score: 80},
		"issues":   {ID: "issues", NoData: true},
		// "code_health" absent entirely.
	}
	missing := MissingCategories(categories, []string{"activity", "issues", "code_health"})
	if len(missing) != 2 || missing[0] != "issues" || missing[1] != "code_health" {
		t.Errorf("missing = %v, want [issues code_health]", missing)
	}
}

func TestSubsetCategories(t *testing.T) {
	categories := map[string]CategoryResult{
		"activity": {ID: "activity", Score: 80},
		"security": {ID: "security", Score: 50},
	}
	subset := SubsetCategories(categories, []string{"activity"})
	if len(subset) != 1 {
		t.Fatalf("subset has %d entries, want 1", len(subset))
	}
	if _, ok := subset["security"]; ok {
		t.Error("subset should not leak the security category into a public-only view")
	}
}

func TestBuildRecommendations_SkipsHealthyNoDataAndUncataloged(t *testing.T) {
	categories := map[string]CategoryResult{
		"activity": {
			ID: "activity",
			Metrics: []MetricResult{
				{ID: "healthy_metric", Score: 1.0},            // perfect, no recommendation
				{ID: "bad_metric", Score: 0.2},                // below healthy, has a catalog entry
				{ID: "uncataloged_metric", Score: 0.1},        // below healthy, no catalog entry
				{ID: "nodata_metric", Score: 0, NoData: true}, // no data, never recommend
			},
		},
		"security": {ID: "security", NoData: true}, // whole category NoData, skip entirely
	}
	catalog := map[string]string{"bad_metric": "fix the bad thing"}

	recs := BuildRecommendations(categories, []string{"activity", "security"}, catalog)
	if len(recs) != 1 {
		t.Fatalf("got %d recommendations, want 1: %+v", len(recs), recs)
	}
	if recs[0].MetricID != "bad_metric" || recs[0].Text != "fix the bad thing" || recs[0].CategoryID != "activity" {
		t.Errorf("recommendation = %+v, want {activity bad_metric \"fix the bad thing\"}", recs[0])
	}
}

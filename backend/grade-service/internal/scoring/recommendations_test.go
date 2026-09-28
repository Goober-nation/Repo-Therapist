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

func TestPickRandom(t *testing.T) {
	recs := []Recommendation{{MetricID: "a"}, {MetricID: "b"}, {MetricID: "c"}, {MetricID: "d"}, {MetricID: "e"}}
	reverse := func(n int, swap func(i, j int)) {
		for i := 0; i < n/2; i++ {
			swap(i, n-1-i)
		}
	}

	got := PickRandom(recs, 3, reverse)
	if len(got) != 3 || got[0].MetricID != "e" || got[1].MetricID != "d" || got[2].MetricID != "c" {
		t.Errorf("PickRandom(5 recs, 3) = %+v, want first 3 after shuffle (e, d, c)", got)
	}
	if recs[0].MetricID != "a" {
		t.Error("PickRandom must not reorder the input slice")
	}

	if got := PickRandom(recs[:2], 3, reverse); len(got) != 2 {
		t.Errorf("fewer than n recs should return all of them, got %d", len(got))
	}
	if got := PickRandom(recs, 0, reverse); len(got) != len(recs) {
		t.Errorf("n = 0 should return all recs, got %d", len(got))
	}
	if got := PickRandom(nil, 3, reverse); len(got) != 0 {
		t.Errorf("no recs should return none, got %d", len(got))
	}
}

func TestDocsRecommendations_ForwardsAdviceInOrderWithLabel(t *testing.T) {
	ids := []string{"activity", "documentation"}
	cats := map[string]CategoryResult{"documentation": {ID: "documentation", Score: 61}}
	advice := []string{"Добавьте команды тестов.", "  ", "Документация оценена упрощённо, без LLM."}

	got := DocsRecommendations(cats, ids, advice)
	want := []Recommendation{
		{CategoryID: "documentation", MetricID: "llm_advice", Text: "[medium] Добавьте команды тестов."},
		{CategoryID: "documentation", MetricID: "llm_advice", Text: "[medium] Документация оценена упрощённо, без LLM."},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("item %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestDocsRecommendations_NothingWithoutUsableDocumentation(t *testing.T) {
	advice := []string{"x"}
	cases := map[string]struct {
		cats map[string]CategoryResult
		ids  []string
	}{
		"no data":         {map[string]CategoryResult{"documentation": {ID: "documentation", NoData: true}}, []string{"documentation"}},
		"absent":          {map[string]CategoryResult{}, []string{"documentation"}},
		"not in the view": {map[string]CategoryResult{"documentation": {ID: "documentation", Score: 50}}, []string{"activity"}},
	}
	for name, tc := range cases {
		if got := DocsRecommendations(tc.cats, tc.ids, advice); len(got) != 0 {
			t.Errorf("%s: got %v, want none", name, got)
		}
	}
}

package scoring

import (
	"testing"

	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/contracts"
)

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
	catalog := map[string]config.RecommendationEntry{"bad_metric": {Priority: "high", Text: "fix the bad thing"}}

	recs := BuildRecommendations(categories, []string{"activity", "security"}, catalog)
	if len(recs) != 1 {
		t.Fatalf("got %d recommendations, want 1: %+v", len(recs), recs)
	}
	want := Recommendation{CategoryID: "activity", MetricID: "bad_metric", Priority: "high", Text: "[high] fix the bad thing"}
	if recs[0] != want {
		t.Errorf("recommendation = %+v, want %+v", recs[0], want)
	}
}

func TestDocsRecommendations_ForwardsAdviceInOrderWithLabel(t *testing.T) {
	ids := []string{"activity", "documentation"}
	cats := map[string]CategoryResult{"documentation": {ID: "documentation", Score: 61}}
	advice := []contracts.DocsAdvice{
		{Code: "DOC_TESTING_INSTRUCTIONS_MISSING", Priority: "high", Text: "Добавьте команды тестов."},
		{Code: "DOC_X", Priority: "low", Text: "  "},
		{Code: "DOC_ALGORITHMIC_NOTICE", Priority: "low", Text: "Документация оценена упрощённо, без LLM."},
		// No code and an unknown priority fall back to llm_advice / medium.
		{Priority: "urgent", Text: "Старый формат."},
	}

	got := DocsRecommendations(cats, ids, advice)
	want := []Recommendation{
		{CategoryID: "documentation", MetricID: "DOC_TESTING_INSTRUCTIONS_MISSING", Priority: "high", Text: "[high] Добавьте команды тестов."},
		{CategoryID: "documentation", MetricID: "DOC_ALGORITHMIC_NOTICE", Priority: "low", Text: "[low] Документация оценена упрощённо, без LLM."},
		{CategoryID: "documentation", MetricID: "llm_advice", Priority: "medium", Text: "[medium] Старый формат."},
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
	advice := []contracts.DocsAdvice{{Code: "c", Priority: "low", Text: "x"}}
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

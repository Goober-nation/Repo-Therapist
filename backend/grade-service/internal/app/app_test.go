package app

import (
	"encoding/json"
	"testing"
	"time"

	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/contracts"
	"repo-therapist/grade-service/internal/scoring"
)

// TestBuildGradingResult_RoundsNotTruncates is the regression test for the real bug found
// reviewing a live pipeline run (2026-09-25): int16(x) truncates rather than rounds, silently
// biasing every headline score down by up to a full point.
func TestBuildGradingResult_RoundsNotTruncates(t *testing.T) {
	// A single 100%-weighted category scoring 50.7 must round to 51, not truncate to 50.
	out := &GradeOutput{
		RepoID: "test-repo",
		Categories: map[string]scoring.CategoryResult{
			"activity": {ID: "activity", Score: 50.7},
		},
	}
	cfg := &config.Config{Categories: map[string]config.CategoryConfig{
		"activity": {Weight: 1.0},
	}}

	result, err := BuildGradingResult(out, cfg, map[string]string{}, contracts.VisibilityPublic, time.Now(), noShuffle)
	if err != nil {
		t.Fatalf("BuildGradingResult: %v", err)
	}
	if result.Score == nil || *result.Score != 51 {
		t.Errorf("Score = %v, want 51 (50.7 rounds up, a bare int16 cast would truncate to 50)", result.Score)
	}
}

// TestBuildGradingResult_AbsentCategoriesStayAbsent locks in a deliberate decision (2026-09-25):
// a not_calculated category (never added to GradeOutput.Categories at all, e.g. Documentation/
// CI/CD today) stays fully absent from category_scores' JSON — no {"no_data": true} placeholder.
// An earlier version injected placeholders for uniform shape; rejected as a bigger risk than the
// asymmetry itself (more objects shaped like a real category, for a consumer that might not be
// defensive about it) — see docs/grading/00-overview.md or backlog/phase-5-aggregator-
// integration.md for the limitation this accepts. A category Grade actually attempted and got
// NoData back (e.g. Security with no PAT) still appears explicitly — that asymmetry (attempted-
// and-failed vs. never-attempted) is the known, accepted limitation, not something to "fix" here.
func TestBuildGradingResult_AbsentCategoriesStayAbsent(t *testing.T) {
	out := &GradeOutput{
		RepoID: "test-repo",
		Categories: map[string]scoring.CategoryResult{
			"activity": {ID: "activity", Score: 80},
			"security": {ID: "security", NoData: true}, // attempted, came back NoData
			// "documentation" and "ci_cd" absent entirely — not_calculated, never computed.
		},
	}
	cfg := &config.Config{Categories: map[string]config.CategoryConfig{
		"activity": {Weight: 0.15}, "documentation": {Weight: 0.15}, "issues": {Weight: 0.15},
		"code_health": {Weight: 0.20}, "security": {Weight: 0.20}, "ci_cd": {Weight: 0.15},
	}}

	result, err := BuildGradingResult(out, cfg, map[string]string{}, contracts.VisibilityPrivate, time.Now(), noShuffle)
	if err != nil {
		t.Fatalf("BuildGradingResult: %v", err)
	}

	var privateCategories map[string]scoring.CategoryResult
	if err := json.Unmarshal(result.CategoryScores, &privateCategories); err != nil {
		t.Fatalf("unmarshaling private_category_scores: %v", err)
	}

	for _, id := range []string{"documentation", "ci_cd"} {
		if _, ok := privateCategories[id]; ok {
			t.Errorf("category %q should be fully absent from private_category_scores (not_calculated, never computed) — got a key for it", id)
		}
	}
	if cat, ok := privateCategories["security"]; !ok || !cat.NoData {
		t.Errorf(`security should appear explicitly as {"no_data": true} (it was attempted), got %+v (present=%v)`, cat, ok)
	}

	// MissingData is the authoritative "what's not computed" signal regardless of what
	// category_scores does or doesn't contain.
	missingSet := make(map[string]bool)
	for _, id := range result.MissingData {
		missingSet[id] = true
	}
	for _, id := range []string{"documentation", "ci_cd", "security"} {
		if !missingSet[id] {
			t.Errorf("MissingData should list %q, got %v", id, result.MissingData)
		}
	}
}

func noShuffle(int, func(i, j int)) {}

func TestBuildGradingResult_FillsOnlyItsVisibility(t *testing.T) {
	out := &GradeOutput{
		RepoID: "test-repo",
		Categories: map[string]scoring.CategoryResult{
			"activity": {ID: "activity", Score: 80},
		},
	}
	cfg := &config.Config{Categories: map[string]config.CategoryConfig{"activity": {Weight: 1.0}}}

	pub, err := BuildGradingResult(out, cfg, map[string]string{}, contracts.VisibilityPublic, time.Now(), noShuffle)
	if err != nil {
		t.Fatal(err)
	}
	if pub.Visibility != contracts.VisibilityPublic || pub.Score == nil || pub.Error != "" {
		t.Errorf("unexpected public result: %+v", pub)
	}
	for _, id := range pub.MissingData {
		if id == "security" || id == "ci_cd" {
			t.Errorf("public missing_data must not list private-only category %q: %v", id, pub.MissingData)
		}
	}

	priv, err := BuildGradingResult(out, cfg, map[string]string{}, contracts.VisibilityPrivate, time.Now(), noShuffle)
	if err != nil {
		t.Fatal(err)
	}
	if priv.Visibility != contracts.VisibilityPrivate || priv.Score == nil || priv.Error != "" {
		t.Errorf("unexpected private result: %+v", priv)
	}
}

func TestBuildGradingResult_CapsRecommendationsAcrossCategories(t *testing.T) {
	low := func(id string) scoring.MetricResult { return scoring.MetricResult{ID: id, Score: 0.2} }
	out := &GradeOutput{
		RepoID: "test-repo",
		Categories: map[string]scoring.CategoryResult{
			"activity":    {ID: "activity", Score: 20, Metrics: []scoring.MetricResult{low("a1"), low("a2")}},
			"issues":      {ID: "issues", Score: 20, Metrics: []scoring.MetricResult{low("i1"), low("i2")}},
			"code_health": {ID: "code_health", Score: 20, Metrics: []scoring.MetricResult{low("c1")}},
		},
	}
	cfg := &config.Config{Categories: map[string]config.CategoryConfig{
		"activity": {Weight: 0.3}, "issues": {Weight: 0.3}, "code_health": {Weight: 0.4},
	}}
	catalog := map[string]string{"a1": "A1", "a2": "A2", "i1": "I1", "i2": "I2", "c1": "C1"}

	result, err := BuildGradingResult(out, cfg, catalog, contracts.VisibilityPublic, time.Now(), noShuffle)
	if err != nil {
		t.Fatal(err)
	}
	var recs []contracts.Recommendation
	if err := json.Unmarshal(result.Advice, &recs); err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("got %d recommendations from a pool of 5, want 3", len(recs))
	}
}

func TestGradingFailureWireShape(t *testing.T) {
	raw, err := json.Marshal(GradingFailure("u1", contracts.VisibilityPublic, contracts.GradingErrGrading, time.Unix(0, 0).UTC()))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["error"] != "GRADING_FAILED" || got["score"] != nil || got["category_scores"] != nil || got["advice"] != nil ||
		got["sourcecraft_uuid"] != "u1" || got["visibility"] != "public" {
		t.Fatalf("unexpected failure event %s", raw)
	}
}

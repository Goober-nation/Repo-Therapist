package app

import (
	"encoding/json"
	"testing"
	"time"

	"repo-therapist/grade-service/internal/config"
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

	result, err := BuildGradingResult(out, cfg, map[string]string{}, time.Now())
	if err != nil {
		t.Fatalf("BuildGradingResult: %v", err)
	}
	if result.PublicScore == nil || *result.PublicScore != 51 {
		t.Errorf("PublicScore = %v, want 51 (50.7 rounds up, a bare int16 cast would truncate to 50)", result.PublicScore)
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

	result, err := BuildGradingResult(out, cfg, map[string]string{}, time.Now())
	if err != nil {
		t.Fatalf("BuildGradingResult: %v", err)
	}

	var privateCategories map[string]scoring.CategoryResult
	if err := json.Unmarshal(result.PrivateCategoryScores, &privateCategories); err != nil {
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

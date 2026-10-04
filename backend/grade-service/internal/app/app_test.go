package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"repo-therapist/grade-service/internal/analytics"
	"repo-therapist/grade-service/internal/config"
	"repo-therapist/grade-service/internal/contracts"
	"repo-therapist/grade-service/internal/gitlog"
	"repo-therapist/grade-service/internal/scoring"
)

// DeleteRepoData only ever deletes one job's export, never the whole bucket or every job of a repo
// via an empty or malformed prefix.
func TestDeleteRepoDataRefusesUnexpectedPrefix(t *testing.T) {
	for _, prefix := range []string{"", "/", "repos/", "repos", "other/u1/j1"} {
		if _, err := DeleteRepoData(t.Context(), prefix); err == nil || !strings.Contains(err.Error(), "refusing") {
			t.Errorf("DeleteRepoData(%q) err = %v, want refusal", prefix, err)
		}
	}
}

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

	result, err := BuildGradingResult(out, cfg, map[string]config.RecommendationEntry{}, contracts.VisibilityPublic, time.Now())
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

	result, err := BuildGradingResult(out, cfg, map[string]config.RecommendationEntry{}, contracts.VisibilityPrivate, time.Now())
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

func TestBuildGradingResult_FillsOnlyItsVisibility(t *testing.T) {
	out := &GradeOutput{
		RepoID: "test-repo",
		Categories: map[string]scoring.CategoryResult{
			"activity": {ID: "activity", Score: 80},
		},
	}
	cfg := &config.Config{Categories: map[string]config.CategoryConfig{"activity": {Weight: 1.0}}}

	pub, err := BuildGradingResult(out, cfg, map[string]config.RecommendationEntry{}, contracts.VisibilityPublic, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if pub.GradeMode != contracts.VisibilityPublic || pub.Score == nil || pub.Error != "" {
		t.Errorf("unexpected public result: %+v", pub)
	}
	for _, id := range pub.MissingData {
		if id == "security" || id == "ci_cd" {
			t.Errorf("public missing_data must not list private-only category %q: %v", id, pub.MissingData)
		}
	}

	priv, err := BuildGradingResult(out, cfg, map[string]config.RecommendationEntry{}, contracts.VisibilityPrivate, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if priv.GradeMode != contracts.VisibilityPrivate || priv.Score == nil || priv.Error != "" {
		t.Errorf("unexpected private result: %+v", priv)
	}
}

func TestBuildGradingResult_SendsEveryRecommendationInCategoryOrder(t *testing.T) {
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
	entry := func(text string) config.RecommendationEntry {
		return config.RecommendationEntry{Priority: "low", Text: text}
	}
	catalog := map[string]config.RecommendationEntry{"a1": entry("A1"), "a2": entry("A2"), "i1": entry("I1"), "i2": entry("I2"), "c1": entry("C1")}

	result, err := BuildGradingResult(out, cfg, catalog, contracts.VisibilityPublic, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var recs []contracts.Recommendation
	if err := json.Unmarshal(result.Advice, &recs); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range recs {
		got = append(got, r.Text)
	}
	if strings.Join(got, ",") != "A1,A2,I1,I2,C1" {
		t.Fatalf("recommendations = %v, want all 5 in category order", got)
	}
}

func TestBuildGradingResult_DocsAdviceJoinsThePool(t *testing.T) {
	out := &GradeOutput{
		RepoID: "test-repo",
		Categories: map[string]scoring.CategoryResult{
			"activity":      {ID: "activity", Score: 20, Metrics: []scoring.MetricResult{{ID: "a1", Score: 0.2}}},
			"documentation": {ID: "documentation", Score: 61},
		},
		DocsAdvice: []contracts.DocsAdvice{{Code: "DOC_TESTING_INSTRUCTIONS_MISSING", Priority: "medium", Text: "D1"}, {Code: "DOC_README_MISSING", Priority: "high", Text: "D2"}},
	}
	cfg := &config.Config{Categories: map[string]config.CategoryConfig{
		"activity": {Weight: 0.5}, "documentation": {Weight: 0.5},
	}}
	result, err := BuildGradingResult(out, cfg, map[string]config.RecommendationEntry{"a1": {Priority: "critical", Text: "A1"}}, contracts.VisibilityPublic, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var recs []contracts.Recommendation
	if err := json.Unmarshal(result.Advice, &recs); err != nil {
		t.Fatal(err)
	}
	want := []contracts.Recommendation{
		{CategoryID: "activity", MetricID: "a1", Priority: "critical", Text: "A1"},
		{CategoryID: "documentation", MetricID: "DOC_TESTING_INSTRUCTIONS_MISSING", Priority: "medium", Text: "D1"},
		{CategoryID: "documentation", MetricID: "DOC_README_MISSING", Priority: "high", Text: "D2"},
	}
	if len(recs) != len(want) {
		t.Fatalf("got %+v, want %+v", recs, want)
	}
	for i := range want {
		if recs[i] != want[i] {
			t.Errorf("item %d: got %+v, want %+v", i, recs[i], want[i])
		}
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
		got["sourcecraft_uuid"] != "u1" || got["grade_mode"] != "public" {
		t.Fatalf("unexpected failure event %s", raw)
	}
}

// Open critical/high Security findings cap the private overall score at 59/79; lower scores and
// the public view (no Security) are untouched.
func TestBuildGradingResult_SecurityCap(t *testing.T) {
	cfg := &config.Config{Categories: map[string]config.CategoryConfig{
		"activity": {Weight: 0.15}, "documentation": {Weight: 0.15}, "issues": {Weight: 0.15},
		"code_health": {Weight: 0.20}, "security": {Weight: 0.20}, "ci_cd": {Weight: 0.15},
	}}
	security := func(critical, high float64) scoring.CategoryResult {
		return scoring.CategoryResult{ID: "security", Score: 100, Metrics: []scoring.MetricResult{
			{ID: "open_critical", RawValue: critical}, {ID: "open_high", RawValue: high},
		}}
	}
	all := func(score float64, sec scoring.CategoryResult) map[string]scoring.CategoryResult {
		return map[string]scoring.CategoryResult{
			"activity": {ID: "activity", Score: score}, "documentation": {ID: "documentation", Score: score},
			"issues": {ID: "issues", Score: score}, "code_health": {ID: "code_health", Score: score},
			"ci_cd": {ID: "ci_cd", Score: score}, "security": sec,
		}
	}
	cases := []struct {
		name       string
		categories map[string]scoring.CategoryResult
		visibility string
		want       int16
	}{
		{"open critical caps at 59", all(100, security(1, 0)), contracts.VisibilityPrivate, 59},
		{"open high caps at 79", all(100, security(0, 3)), contracts.VisibilityPrivate, 79},
		{"critical wins over high", all(100, security(2, 2)), contracts.VisibilityPrivate, 59},
		{"score already below cap unchanged", all(40, scoring.CategoryResult{ID: "security", Score: 40,
			Metrics: []scoring.MetricResult{{ID: "open_critical", RawValue: 1}}}), contracts.VisibilityPrivate, 40},
		{"no open findings: no cap", all(100, security(0, 0)), contracts.VisibilityPrivate, 100},
		{"no scan: no cap", all(100, scoring.CategoryResult{ID: "security", NoData: true}), contracts.VisibilityPrivate, 100},
		{"public view ignores security", all(100, security(1, 0)), contracts.VisibilityPublic, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := &GradeOutput{RepoID: "r", Categories: tc.categories}
			result, err := BuildGradingResult(out, cfg, map[string]config.RecommendationEntry{}, tc.visibility, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if result.Score == nil || *result.Score != tc.want {
				t.Errorf("Score = %v, want %d", result.Score, tc.want)
			}
		})
	}
}

func confidenceTestConfig() *config.Config {
	return &config.Config{
		Categories: map[string]config.CategoryConfig{
			"activity": {Weight: 0.5}, "issues": {Weight: 0.5}, "documentation": {Weight: 0.5}, "code_health": {Weight: 0.5},
		},
		Confidence: config.ConfidenceConfig{
			Enabled: true, WindowDays: 180, MinFiles: 5, MinLOC: 200, MinCommits: 5, MinCommitsSingleAuthor: 20, MinCategoriesWithData: 2,
			Reasons: map[string]string{config.ReasonLowCode: "code", config.ReasonLowHistory: "history", config.ReasonLowCoverage: "coverage"},
		},
	}
}

// The flag rides on public grades only and never touches the score.
func TestBuildGradingResult_ConfidenceIsPublicOnlyAndKeepsTheScore(t *testing.T) {
	cfg := confidenceTestConfig()
	out := &GradeOutput{
		RepoID: "r",
		Categories: map[string]scoring.CategoryResult{
			"activity": {ID: "activity", Score: 80}, "issues": {ID: "issues", NoData: true},
			"documentation": {ID: "documentation", NoData: true}, "code_health": {ID: "code_health", NoData: true},
		},
		ConfidenceInputs: scoring.ConfidenceInputs{CodeAvailable: true, ScannedFiles: 1, TotalLOC: 10},
	}

	pub, err := BuildGradingResult(out, cfg, map[string]config.RecommendationEntry{}, contracts.VisibilityPublic, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if pub.Confidence == nil || !pub.Confidence.LowConfidence || len(pub.Confidence.Reasons) != 2 {
		t.Fatalf("public confidence = %+v, want low with LOW_CODE and LOW_COVERAGE", pub.Confidence)
	}
	if pub.Score == nil || *pub.Score != 80 {
		t.Errorf("score = %v, want 80: the flag must not change it", pub.Score)
	}

	priv, err := BuildGradingResult(out, cfg, map[string]config.RecommendationEntry{}, contracts.VisibilityPrivate, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if priv.Confidence != nil {
		t.Errorf("private confidence = %+v, want nil", priv.Confidence)
	}

	raw, _ := json.Marshal(pub)
	if !strings.Contains(string(raw), `"confidence":{"low_confidence":true,"reasons":[{"code":"LOW_CODE","text":"code"}`) {
		t.Errorf("wire shape = %s", raw)
	}
}

func TestBuildGradingResult_ConfidenceDisabledIsNull(t *testing.T) {
	cfg := confidenceTestConfig()
	cfg.Confidence.Enabled = false
	out := &GradeOutput{RepoID: "r", Categories: map[string]scoring.CategoryResult{"activity": {ID: "activity", Score: 80}}}

	result, err := BuildGradingResult(out, cfg, map[string]config.RecommendationEntry{}, contracts.VisibilityPublic, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	if result.Confidence != nil || !strings.Contains(string(raw), `"confidence":null`) {
		t.Errorf("confidence = %+v wire = %s, want null", result.Confidence, raw)
	}
}

func analyticsTestOutput() *GradeOutput {
	now := time.Now()
	var commits []gitlog.Commit
	for i := 0; i < 8; i++ {
		commits = append(commits, gitlog.Commit{
			Hash: "h", AuthorName: "Ann", AuthorEmail: "ann@x.dev", AuthoredAt: now.AddDate(0, 0, -i-1),
			Parents: []string{"p"}, FilesChanged: []gitlog.FileChange{{Path: "src/a.go", Added: 5}},
		})
	}
	return &GradeOutput{
		RepoID:     "r",
		Categories: map[string]scoring.CategoryResult{"activity": {ID: "activity", Score: 80}},
		AnalyticsInputs: analytics.Inputs{
			CommitsAvailable: true, Commits: commits, CodeownersPath: "CODEOWNERS", Codeowners: []byte("* @ann"),
		},
	}
}

// The analytics ride on both grade modes and never touch the score.
func TestBuildGradingResult_AnalyticsForBothModesWithoutChangingTheScore(t *testing.T) {
	cfg := confidenceTestConfig()
	cfg.Analytics = config.AnalyticsConfig{Enabled: true, WindowDays: 180, MinCommits: 5, TopAuthors: 5, TopAreas: 8}

	var scores []int16
	for _, mode := range []string{contracts.VisibilityPublic, contracts.VisibilityPrivate} {
		result, err := BuildGradingResult(analyticsTestOutput(), cfg, map[string]config.RecommendationEntry{}, mode, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		var got analytics.Result
		if err := json.Unmarshal(result.Analytics, &got); err != nil {
			t.Fatalf("%s: analytics is not the analytics object: %v (%s)", mode, err, result.Analytics)
		}
		if got.BusFactor.Status != analytics.StatusOK || got.BusFactor.Value != 1 || got.Ownership.Areas[0].Path != "src/" || !got.Ownership.Codeowners.Present {
			t.Errorf("%s: analytics = %+v", mode, got)
		}
		scores = append(scores, *result.Score)
	}
	if scores[0] != 80 || scores[1] != 80 {
		t.Errorf("scores = %v, want 80 in both modes: analytics must not change them", scores)
	}
}

func TestBuildGradingResult_AnalyticsDisabledIsNull(t *testing.T) {
	cfg := confidenceTestConfig() // analytics left off
	result, err := BuildGradingResult(analyticsTestOutput(), cfg, map[string]config.RecommendationEntry{}, contracts.VisibilityPublic, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	if result.Analytics != nil || !strings.Contains(string(raw), `"analytics":null`) {
		t.Errorf("analytics = %s, want null on the wire", result.Analytics)
	}
}

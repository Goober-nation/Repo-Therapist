package config

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
)

// The shipped config files must parse: grade-service refuses to start otherwise.
func TestShippedConfigsLoad(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "config")
	if _, err := LoadConfig(filepath.Join(root, "grading.yaml")); err != nil {
		t.Fatalf("grading.yaml: %v", err)
	}
	catalog, err := LoadRecommendations(filepath.Join(root, "recommendations.yaml"))
	if err != nil {
		t.Fatalf("recommendations.yaml: %v", err)
	}
	for _, id := range []string{"ci_noop_pipeline", "ci_median_duration_min"} {
		if catalog[id].Text == "" || catalog[id].Priority == "" {
			t.Errorf("recommendations.yaml has no text for %q", id)
		}
	}
}

// Category weights are a grading decision (docs/grading/00-overview.md): they must sum to 1.0 and
// changing one must come with this test and the docs updated.
func TestShippedCategoryWeights(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join("..", "..", "..", "..", "config", "grading.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{
		"activity": 0.20, "issues": 0.20, "code_health": 0.10,
		"security": 0.20, "documentation": 0.15, "ci_cd": 0.15,
	}
	var sum float64
	for id, cat := range cfg.Categories {
		sum += cat.Weight
		if w, ok := want[id]; !ok || math.Abs(cat.Weight-w) > 1e-9 {
			t.Errorf("category %q weight = %v, want %v", id, cat.Weight, want[id])
		}
	}
	if len(cfg.Categories) != len(want) {
		t.Errorf("got %d categories, want %d", len(cfg.Categories), len(want))
	}
	if math.Abs(sum-1.0) > 1e-9 {
		t.Errorf("category weights sum to %v, want 1.0", sum)
	}
}

// The shipped confidence block must load, be enabled, and carry a generic text per reason that
// does not leak a threshold number.
func TestShippedConfidenceConfig(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join("..", "..", "..", "..", "config", "grading.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	c := cfg.Confidence
	if !c.Enabled || c.WindowDays != 180 || c.MinFiles != 5 || c.MinLOC != 200 || c.MinCommits != 5 ||
		c.MinCommitsSingleAuthor != 20 || c.MinCategoriesWithData != 2 {
		t.Errorf("confidence = %+v", c)
	}
	for _, code := range []string{ReasonLowCode, ReasonLowHistory, ReasonLowCoverage} {
		text := c.Reasons[code]
		if text == "" {
			t.Errorf("no text for %s", code)
		}
		for _, digit := range "0123456789" {
			if strings.ContainsRune(text, digit) {
				t.Errorf("%s text %q names a number: reasons must not reveal thresholds", code, text)
			}
		}
	}
}

func TestConfidenceValidation(t *testing.T) {
	good := ConfidenceConfig{Enabled: true, WindowDays: 30, Reasons: map[string]string{ReasonLowCode: "a", ReasonLowHistory: "b", ReasonLowCoverage: "c"}}
	if err := validateConfidence(good); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if err := validateConfidence(ConfidenceConfig{}); err != nil {
		t.Errorf("disabled config must not be validated: %v", err)
	}

	noWindow := good
	noWindow.WindowDays = 0
	missingText := good
	missingText.Reasons = map[string]string{ReasonLowCode: "a"}
	negative := good
	negative.MinLOC = -1
	for name, c := range map[string]ConfidenceConfig{"window": noWindow, "missing reason text": missingText, "negative floor": negative} {
		if err := validateConfidence(c); err == nil {
			t.Errorf("%s: want a validation error", name)
		}
	}
}

// The anti-gaming filters are a grading decision calibrated on real histories (docs/grading/
// 00-overview.md): changing a value means re-running the calibration and updating the docs.
func TestShippedHardeningFilters(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join("..", "..", "..", "..", "config", "grading.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	activity, issues := cfg.Categories["activity"], cfg.Categories["issues"]

	if activity.CommitFilters.MinChangedLines != 4 {
		t.Errorf("activity min_changed_lines = %d, want 4", activity.CommitFilters.MinChangedLines)
	}
	if activity.IssueFilters != (IssueFilters{InstantCloseSeconds: 30, MilestonesNeedIssues: true}) {
		t.Errorf("activity issue_filters = %+v", activity.IssueFilters)
	}
	if issues.IssueFilters.InstantCloseSeconds != 30 || issues.IssueFilters.MilestonesNeedIssues {
		t.Errorf("issues issue_filters = %+v, want only instant_close_seconds: 30", issues.IssueFilters)
	}
}

func TestShippedAnalyticsConfig(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join("..", "..", "..", "..", "config", "grading.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Analytics != (AnalyticsConfig{Enabled: true, WindowDays: 180, MinCommits: 5, TopAuthors: 5, TopAreas: 8, TopSubareas: 10, TopFiles: 10, MinMergedPRs: 3, MinReviewedPRs: 3, WaitingDays: 7, ImportShare: 0.8}) {
		t.Errorf("analytics = %+v", cfg.Analytics)
	}
	if err := validateAnalytics(AnalyticsConfig{Enabled: true, WindowDays: 0, TopAuthors: 1, TopAreas: 1, TopSubareas: 1, TopFiles: 1, MinMergedPRs: 3, MinReviewedPRs: 3, WaitingDays: 7, ImportShare: 0.8}); err == nil {
		t.Error("a zero window must be rejected")
	}
	ok := AnalyticsConfig{Enabled: true, WindowDays: 1, TopAuthors: 1, TopAreas: 1, TopSubareas: 1, TopFiles: 1, MinMergedPRs: 3, MinReviewedPRs: 3, WaitingDays: 7, ImportShare: 0.8}
	if err := validateAnalytics(ok); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
	for name, bad := range map[string]AnalyticsConfig{
		"no merged PR minimum": {Enabled: true, WindowDays: 1, TopAuthors: 1, TopAreas: 1, TopSubareas: 1, TopFiles: 1, MinReviewedPRs: 3, WaitingDays: 7, ImportShare: 0.8},
		"zero import share":    {Enabled: true, WindowDays: 1, TopAuthors: 1, TopAreas: 1, TopSubareas: 1, TopFiles: 1, MinMergedPRs: 3, MinReviewedPRs: 3, WaitingDays: 7},
		"import share above 1": {Enabled: true, WindowDays: 1, TopAuthors: 1, TopAreas: 1, TopSubareas: 1, TopFiles: 1, MinMergedPRs: 3, MinReviewedPRs: 3, WaitingDays: 7, ImportShare: 1.5},
		"no reviewed minimum":  {Enabled: true, WindowDays: 1, TopAuthors: 1, TopAreas: 1, TopSubareas: 1, TopFiles: 1, MinMergedPRs: 3, WaitingDays: 7, ImportShare: 0.8},
		"no waiting days":      {Enabled: true, WindowDays: 1, TopAuthors: 1, TopAreas: 1, TopSubareas: 1, TopFiles: 1, MinMergedPRs: 3, MinReviewedPRs: 3, ImportShare: 0.8},
		"no subarea limit":     {Enabled: true, WindowDays: 1, TopAuthors: 1, TopAreas: 1, TopFiles: 1, MinMergedPRs: 3, MinReviewedPRs: 3, WaitingDays: 7, ImportShare: 0.8},
		"no file limit":        {Enabled: true, WindowDays: 1, TopAuthors: 1, TopAreas: 1, TopSubareas: 1, MinMergedPRs: 3, MinReviewedPRs: 3, WaitingDays: 7, ImportShare: 0.8},
	} {
		if validateAnalytics(bad) == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
	if err := validateAnalytics(AnalyticsConfig{}); err != nil {
		t.Errorf("disabled analytics must not be validated: %v", err)
	}
}

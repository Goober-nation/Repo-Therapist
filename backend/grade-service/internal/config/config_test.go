package config

import (
	"math"
	"path/filepath"
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

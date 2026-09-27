package config

import (
	"testing"
	"time"
)

func TestLLMRequestTTL(t *testing.T) {
	t.Setenv("LLM_REQUEST_TTL", "")
	if got := LoadConfig().Grading.LLMRequestTTL; got != 2*time.Minute {
		t.Errorf("unset: got %s, want 2m", got)
	}
	t.Setenv("LLM_REQUEST_TTL", "5m")
	if got := LoadConfig().Grading.LLMRequestTTL; got != 5*time.Minute {
		t.Errorf("5m: got %s", got)
	}
}

func TestRegradeSettings(t *testing.T) {
	t.Setenv("GRADING_REGRADE_ON_START", "")
	t.Setenv("GRADING_REGRADE_LIMIT", "")
	g := LoadConfig().Grading
	if !g.RegradeOnStart || g.RegradeLimit != 0 {
		t.Errorf("defaults: got on_start=%v limit=%d, want true 0", g.RegradeOnStart, g.RegradeLimit)
	}
	t.Setenv("GRADING_REGRADE_ON_START", "false")
	t.Setenv("GRADING_REGRADE_LIMIT", "25")
	g = LoadConfig().Grading
	if g.RegradeOnStart || g.RegradeLimit != 25 {
		t.Errorf("override: got on_start=%v limit=%d, want false 25", g.RegradeOnStart, g.RegradeLimit)
	}
	t.Setenv("GRADING_REGRADE_LIMIT", "-3")
	if got := LoadConfig().Grading.RegradeLimit; got != 0 {
		t.Errorf("negative: got %d, want 0", got)
	}
}

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
	t.Setenv("GRADING_REGRADE_INTERVAL", "")
	t.Setenv("GRADING_REGRADE_ON_START", "")
	t.Setenv("GRADING_REGRADE_LIMIT", "")
	g := LoadConfig().Grading
	if g.RegradeOnStart || g.RegradeLimit != 20 || g.RegradeInterval != 72*time.Hour {
		t.Errorf("defaults: got on_start=%v limit=%d interval=%s, want false 20 72h", g.RegradeOnStart, g.RegradeLimit, g.RegradeInterval)
	}
	t.Setenv("GRADING_REGRADE_ON_START", "true")
	t.Setenv("GRADING_REGRADE_LIMIT", "25")
	g = LoadConfig().Grading
	if !g.RegradeOnStart || g.RegradeLimit != 25 {
		t.Errorf("override: got on_start=%v limit=%d, want true 25", g.RegradeOnStart, g.RegradeLimit)
	}
	t.Setenv("GRADING_REGRADE_LIMIT", "-3")
	if got := LoadConfig().Grading.RegradeLimit; got != 20 {
		t.Errorf("negative: got %d, want 20", got)
	}
}

func TestAuditRetentionDays(t *testing.T) {
	t.Setenv("AUDIT_RETENTION_DAYS", "")
	if got := LoadConfig().Audit.RetentionDays; got != 30 {
		t.Errorf("unset: got %d, want 30", got)
	}
	t.Setenv("AUDIT_RETENTION_DAYS", "90")
	if got := LoadConfig().Audit.RetentionDays; got != 90 {
		t.Errorf("90: got %d", got)
	}
	t.Setenv("AUDIT_RETENTION_DAYS", "0")
	if got := LoadConfig().Audit.RetentionDays; got != 0 {
		t.Errorf("0 keeps the log forever: got %d", got)
	}
	t.Setenv("AUDIT_RETENTION_DAYS", "forever")
	if got := LoadConfig().Audit.RetentionDays; got != 30 {
		t.Errorf("invalid: got %d, want the default 30", got)
	}
}

func TestBatchRegradeSettings(t *testing.T) {
	t.Setenv("BATCH_REGRADE_CONCURRENCY", "")
	t.Setenv("BATCH_REGRADE_MAX_REPOS", "")
	t.Setenv("BATCH_REGRADE_ITEM_TIMEOUT", "")

	b := LoadConfig().Batch
	if b.Concurrency != 3 || b.MaxRepos != 500 || b.ItemTimeout != 15*time.Minute {
		t.Errorf("defaults: %+v", b)
	}

	t.Setenv("BATCH_REGRADE_CONCURRENCY", "5")
	t.Setenv("BATCH_REGRADE_MAX_REPOS", "50")
	t.Setenv("BATCH_REGRADE_ITEM_TIMEOUT", "2m")

	b = LoadConfig().Batch
	if b.Concurrency != 5 || b.MaxRepos != 50 || b.ItemTimeout != 2*time.Minute {
		t.Errorf("override: %+v", b)
	}
}

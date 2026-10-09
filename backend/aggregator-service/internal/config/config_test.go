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
	if g.RegradeOnStart || g.RegradeLimit != 500 || g.RegradeInterval != 72*time.Hour {
		t.Errorf("defaults: got on_start=%v limit=%d interval=%s, want false 500 72h", g.RegradeOnStart, g.RegradeLimit, g.RegradeInterval)
	}
	t.Setenv("GRADING_REGRADE_ON_START", "true")
	t.Setenv("GRADING_REGRADE_LIMIT", "25")
	g = LoadConfig().Grading
	if !g.RegradeOnStart || g.RegradeLimit != 25 {
		t.Errorf("override: got on_start=%v limit=%d, want true 25", g.RegradeOnStart, g.RegradeLimit)
	}
	t.Setenv("GRADING_REGRADE_LIMIT", "-3")
	if got := LoadConfig().Grading.RegradeLimit; got != 500 {
		t.Errorf("negative: got %d, want 500", got)
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

func TestBatchGradingSettings(t *testing.T) {
	t.Setenv("BATCH_GRADING_CONCURRENCY", "")
	t.Setenv("BATCH_GRADING_ITEM_TIMEOUT", "")

	t.Setenv("BATCH_GRADING_MAX_LIMIT", "")

	b := LoadConfig().Batch
	if b.MaxLimit != 0 || b.Concurrency != 3 || b.ItemTimeout != 15*time.Minute {
		t.Errorf("defaults: %+v", b)
	}

	t.Setenv("BATCH_GRADING_CONCURRENCY", "5")
	t.Setenv("BATCH_GRADING_MAX_LIMIT", "40")
	t.Setenv("BATCH_GRADING_ITEM_TIMEOUT", "2m")

	b = LoadConfig().Batch
	if b.MaxLimit != 40 || b.Concurrency != 5 || b.ItemTimeout != 2*time.Minute {
		t.Errorf("override: %+v", b)
	}
}

func TestLockTimeoutWarning(t *testing.T) {
	t.Setenv("CLONE_TIMEOUT", "")
	t.Setenv("GRADE_TIMEOUT", "")
	if got := LockTimeoutWarning(20 * time.Minute); got != "" {
		t.Fatalf("default 20m vs 14m: unexpected warning %q", got)
	}
	if got := LockTimeoutWarning(0); got != "" {
		t.Fatalf("0 (off): unexpected warning %q", got)
	}

	t.Setenv("CLONE_TIMEOUT", "15m")
	t.Setenv("GRADE_TIMEOUT", "6m")
	if got := LockTimeoutWarning(20 * time.Minute); got == "" {
		t.Fatal("20m vs 21m: expected a warning")
	}
	if got := LockTimeoutWarning(21 * time.Minute); got == "" {
		t.Fatal("21m vs 21m: expected a warning (must be above)")
	}
}

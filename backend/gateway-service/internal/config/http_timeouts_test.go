package config

import (
	"testing"
	"time"
)

func TestHTTPTimeouts(t *testing.T) {
	t.Setenv("HTTP_WRITE_TIMEOUT", "")
	if got := LoadConfig().HTTP.WriteTimeout; got != 30*time.Second {
		t.Errorf("unset: got %s, want 30s", got)
	}
	t.Setenv("HTTP_WRITE_TIMEOUT", "2m")
	if got := LoadConfig().HTTP.WriteTimeout; got != 2*time.Minute {
		t.Errorf("2m: got %s", got)
	}
	t.Setenv("HTTP_WRITE_TIMEOUT", "later")
	if got := LoadConfig().HTTP.WriteTimeout; got != 30*time.Second {
		t.Errorf("invalid: got %s, want 30s", got)
	}
}

func TestPublicAPIRateLimitDefaultsAndOverride(t *testing.T) {
	t.Setenv("PUBLIC_API_RATE_LIMIT", "")
	t.Setenv("PUBLIC_API_RATE_LIMIT_WINDOW", "")
	cfg := LoadConfig()
	if cfg.PublicAPIRateLimit.Limit != 300 || cfg.PublicAPIRateLimit.Window != time.Minute {
		t.Errorf("defaults = %+v, want 300 per minute", cfg.PublicAPIRateLimit)
	}

	t.Setenv("PUBLIC_API_RATE_LIMIT", "50")
	t.Setenv("PUBLIC_API_RATE_LIMIT_WINDOW", "10s")
	cfg = LoadConfig()
	if cfg.PublicAPIRateLimit.Limit != 50 || cfg.PublicAPIRateLimit.Window != 10*time.Second {
		t.Errorf("override = %+v, want 50 per 10s", cfg.PublicAPIRateLimit)
	}
}

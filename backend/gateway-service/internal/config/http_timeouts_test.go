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

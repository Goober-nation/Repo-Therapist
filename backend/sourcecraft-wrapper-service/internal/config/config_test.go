package config

import "testing"

func TestMaxPages(t *testing.T) {
	t.Setenv("SOURCECRAFT_MAX_PAGES", "")
	if got := LoadConfig().MaxPages; got != "50" {
		t.Errorf("unset: got %q, want 50", got)
	}
	t.Setenv("SOURCECRAFT_MAX_PAGES", "120")
	if got := LoadConfig().MaxPages; got != "120" {
		t.Errorf("override: got %q, want 120", got)
	}
}

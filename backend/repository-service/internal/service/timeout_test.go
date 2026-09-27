package service

import (
	"testing"
	"time"
)

func TestCloneTimeout(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want time.Duration
	}{
		{"", DefaultCloneTimeout},
		{"15m", 15 * time.Minute},
		{"soon", DefaultCloneTimeout},
		{"-1m", DefaultCloneTimeout},
	} {
		t.Setenv("CLONE_TIMEOUT", tc.env)
		if got := CloneTimeout(); got != tc.want {
			t.Errorf("CLONE_TIMEOUT=%q: got %s, want %s", tc.env, got, tc.want)
		}
	}
}

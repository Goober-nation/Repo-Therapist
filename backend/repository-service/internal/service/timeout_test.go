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

func TestGradeRequestMaxAge(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want time.Duration
	}{
		{"", DefaultGradeRequestMaxAge},
		{"5m", 5 * time.Minute},
		{"0", 0},
		{"-1m", DefaultGradeRequestMaxAge},
		{"soon", DefaultGradeRequestMaxAge},
	} {
		t.Setenv("GRADE_REQUEST_MAX_AGE", tc.env)
		if got := GradeRequestMaxAge(); got != tc.want {
			t.Errorf("GRADE_REQUEST_MAX_AGE=%q: got %s, want %s", tc.env, got, tc.want)
		}
	}
}

func TestStaleRequest(t *testing.T) {
	now := time.Date(2026, 10, 10, 9, 38, 0, 0, time.UTC)
	for _, tc := range []struct {
		name        string
		requestedAt time.Time
		maxAge      time.Duration
		want        bool
	}{
		{"fresh", now.Add(-time.Minute), 20 * time.Minute, false},
		{"left over from a crash 10h ago", now.Add(-10 * time.Hour), 20 * time.Minute, true},
		{"no time", time.Time{}, 20 * time.Minute, false},
		{"check off", now.Add(-10 * time.Hour), 0, false},
	} {
		if got := StaleRequest(tc.requestedAt, now, tc.maxAge); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

package app

import (
	"testing"
	"time"
)

func TestGradeTimeout(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want time.Duration
	}{
		{"", DefaultGradeTimeout},
		{"6m", 6 * time.Minute},
		{"soon", DefaultGradeTimeout},
		{"-1m", DefaultGradeTimeout},
	} {
		t.Setenv("GRADE_TIMEOUT", tc.env)
		if got := GradeTimeout(); got != tc.want {
			t.Errorf("GRADE_TIMEOUT=%q: got %s, want %s", tc.env, got, tc.want)
		}
	}
}

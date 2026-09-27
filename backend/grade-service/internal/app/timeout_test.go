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

func TestJobTTL(t *testing.T) {
	t.Setenv("GRADE_JOB_TTL", "")
	if got := JobTTL(); got != DefaultJobTTL {
		t.Errorf("unset: got %s, want %s", got, DefaultJobTTL)
	}
	t.Setenv("GRADE_JOB_TTL", "45m")
	if got := JobTTL(); got != 45*time.Minute {
		t.Errorf("45m: got %s", got)
	}
	t.Setenv("GRADE_JOB_TTL", "0s")
	if got := JobTTL(); got != DefaultJobTTL {
		t.Errorf("0s: got %s, want %s", got, DefaultJobTTL)
	}
}

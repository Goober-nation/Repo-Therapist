package app

import (
	"log"
	"os"
	"time"
)

// DefaultGradeTimeout bounds one repo's grading (S3 reads, AppSec call, scoring). Raised from 2m
// (2026-09-27): stability over speed before the defence. The LLM wait (LLM_TIMEOUT, 70s) runs
// alongside and fits inside it.
const DefaultGradeTimeout = 4 * time.Minute

// GradeTimeout returns GRADE_TIMEOUT (a Go duration, e.g. "6m") or DefaultGradeTimeout when it is
// unset or invalid.
func GradeTimeout() time.Duration {
	v := os.Getenv("GRADE_TIMEOUT")
	if v == "" {
		return DefaultGradeTimeout
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		log.Printf("ignoring invalid GRADE_TIMEOUT=%q, using %s", v, DefaultGradeTimeout)
		return DefaultGradeTimeout
	}
	return d
}

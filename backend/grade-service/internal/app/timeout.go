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

// DefaultJobTTL bounds how long a user token can sit in memory if repo.ready never arrives.
const DefaultJobTTL = 30 * time.Minute

// GradeTimeout returns GRADE_TIMEOUT (a Go duration, e.g. "6m") or DefaultGradeTimeout when it is
// unset or invalid.
func GradeTimeout() time.Duration {
	return DurationEnv("GRADE_TIMEOUT", DefaultGradeTimeout)
}

// JobTTL returns GRADE_JOB_TTL (a Go duration) or DefaultJobTTL when it is unset or invalid.
func JobTTL() time.Duration {
	return DurationEnv("GRADE_JOB_TTL", DefaultJobTTL)
}

// DurationEnv returns env var key parsed as a positive Go duration, or def when it is unset or
// invalid (an invalid value is logged).
func DurationEnv(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		log.Printf("ignoring invalid %s=%q, using %s", key, v, def)
		return def
	}
	return d
}

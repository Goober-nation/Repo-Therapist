package service

import (
	"log"
	"os"
	"time"
)

// DefaultCloneTimeout bounds one repo's clone + export + upload. Raised from 5m (2026-09-27):
// stability over speed before the defence. The Kafka consumer handles one message at a time,
// so this also caps how long one stuck repo can hold up the queue.
const DefaultCloneTimeout = 10 * time.Minute

// CloneTimeout returns CLONE_TIMEOUT (a Go duration, e.g. "15m") or DefaultCloneTimeout when it
// is unset or invalid.
func CloneTimeout() time.Duration {
	v := os.Getenv("CLONE_TIMEOUT")
	if v == "" {
		return DefaultCloneTimeout
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		log.Printf("ignoring invalid CLONE_TIMEOUT=%q, using %s", v, DefaultCloneTimeout)
		return DefaultCloneTimeout
	}
	return d
}

// DefaultGradeRequestMaxAge matches the aggregator's GRADING_LOCK_TIMEOUT: by then the aggregator
// has freed the repo lock and stopped waiting, so cloning for the request is wasted work.
const DefaultGradeRequestMaxAge = 20 * time.Minute

// GradeRequestMaxAge returns GRADE_REQUEST_MAX_AGE (a Go duration) or DefaultGradeRequestMaxAge;
// "0" turns the age check off.
func GradeRequestMaxAge() time.Duration {
	v := os.Getenv("GRADE_REQUEST_MAX_AGE")
	if v == "" {
		return DefaultGradeRequestMaxAge
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		log.Printf("ignoring invalid GRADE_REQUEST_MAX_AGE=%q, using %s", v, DefaultGradeRequestMaxAge)
		return DefaultGradeRequestMaxAge
	}
	return d
}

// StaleRequest reports whether a request made at requestedAt is older than maxAge at now. A request
// without a time (e.g. a hand-made one) and a zero maxAge never count as stale.
func StaleRequest(requestedAt, now time.Time, maxAge time.Duration) bool {
	return maxAge > 0 && !requestedAt.IsZero() && now.Sub(requestedAt) > maxAge
}

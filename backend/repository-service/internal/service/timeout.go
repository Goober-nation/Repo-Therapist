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

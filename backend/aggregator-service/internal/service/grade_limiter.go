package service

import (
	"sync"
	"time"
)

// GradeLimiter caps how many new gradings users may start across the whole service within a
// sliding window. It lives in memory, so a restart resets the count.
type GradeLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	starts []time.Time
}

// NewGradeLimiter returns a limiter allowing limit starts per window; limit <= 0 disables it.
func NewGradeLimiter(limit int, window time.Duration) *GradeLimiter {
	return &GradeLimiter{limit: limit, window: window}
}

// Reserve takes a slot at now. When the window is full it returns false and how long until the
// oldest start leaves the window.
func (l *GradeLimiter) Reserve(now time.Time) (bool, time.Duration) {
	if l == nil || l.limit <= 0 {
		return true, 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := now.Add(-l.window)
	kept := l.starts[:0]
	for _, start := range l.starts {
		if start.After(cutoff) {
			kept = append(kept, start)
		}
	}
	l.starts = kept

	if len(l.starts) >= l.limit {
		return false, l.starts[0].Add(l.window).Sub(now)
	}

	l.starts = append(l.starts, now)

	return true, 0
}

// Cancel returns the slot taken at start, for a grading that was never published.
func (l *GradeLimiter) Cancel(start time.Time) {
	if l == nil || l.limit <= 0 {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	for i := len(l.starts) - 1; i >= 0; i-- {
		if l.starts[i].Equal(start) {
			l.starts = append(l.starts[:i], l.starts[i+1:]...)
			return
		}
	}
}

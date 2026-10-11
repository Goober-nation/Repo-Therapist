package service

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Background gradings (admin batches, the scheduled public regrade) yield to gradings users asked
// for: while any user grading is in flight no background grading starts, and at most
// batchConcurrency background gradings are in flight at once. Gradings already started run to the
// end. The state is in memory, so a restart forgets the gradings in flight and nothing waits.

// flight is one grading this process published and has not seen a result for yet.
type flight struct {
	background bool
	at         time.Time
}

type flights struct {
	mu       sync.Mutex
	byRepo   map[uuid.UUID]flight
	reserved int // background turns taken but not yet published or given back
}

func isBackgroundTrigger(trigger string) bool {
	return trigger == "batch" || trigger == "schedule"
}

// trackFlight records a published grading of repoID.
func (as *AggregatorService) trackFlight(repoID uuid.UUID, background bool) {
	as.inflight.mu.Lock()
	defer as.inflight.mu.Unlock()

	if as.inflight.byRepo == nil {
		as.inflight.byRepo = map[uuid.UUID]flight{}
	}

	as.inflight.byRepo[repoID] = flight{background: background, at: as.currentTime()}
}

// landFlight forgets the grading of repoID once its result arrived.
func (as *AggregatorService) landFlight(repoID uuid.UUID) {
	as.inflight.mu.Lock()
	defer as.inflight.mu.Unlock()

	delete(as.inflight.byRepo, repoID)
}

// flightCounts drops gradings older than the repo lock timeout (their result is lost, the lock
// has expired) and counts the rest. Callers hold inflight.mu.
func (as *AggregatorService) flightCounts() (foreground, background int) {
	cutoff := as.currentTime().Add(-as.lockTimeout())

	for id, f := range as.inflight.byRepo {
		if f.at.Before(cutoff) {
			delete(as.inflight.byRepo, id)
			continue
		}

		if f.background {
			background++
		} else {
			foreground++
		}
	}

	return foreground, background
}

func (as *AggregatorService) lockTimeout() time.Duration {
	if ttl := as.tokenTTL(); ttl > 0 {
		return ttl
	}

	return DefaultGradingTokenTTL
}

// ForegroundGradings is how many user gradings are in flight; background gradings wait while it
// is above zero.
func (as *AggregatorService) ForegroundGradings() int {
	as.inflight.mu.Lock()
	defer as.inflight.mu.Unlock()

	foreground, _ := as.flightCounts()

	return foreground
}

// backgroundTurn blocks until a background grading may start: no user grading in flight and a
// free background slot. The returned release must be called once the grading was published (or
// was not); until then the turn holds the slot.
func (as *AggregatorService) backgroundTurn(ctx context.Context) (release func(), err error) {
	for {
		as.inflight.mu.Lock()
		foreground, background := as.flightCounts()

		if foreground == 0 && background+as.inflight.reserved < as.batchConcurrency() {
			as.inflight.reserved++
			as.inflight.mu.Unlock()

			var once sync.Once

			return func() {
				once.Do(func() {
					as.inflight.mu.Lock()
					as.inflight.reserved--
					as.inflight.mu.Unlock()
				})
			}, nil
		}

		as.inflight.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(as.batchPoll()):
		}
	}
}

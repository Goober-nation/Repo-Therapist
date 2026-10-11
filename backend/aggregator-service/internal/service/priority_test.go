package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBackgroundTurnWaitsForUserGradings(t *testing.T) {
	f := newFixture("public")
	f.svc.BatchPoll = 5 * time.Millisecond

	user := uuid.New()
	f.svc.trackFlight(user, false)

	got := make(chan struct{})
	go func() {
		release, err := f.svc.backgroundTurn(context.Background())
		if err == nil {
			release()
		}
		close(got)
	}()

	select {
	case <-got:
		t.Fatal("background grading started while a user grading was in flight")
	case <-time.After(50 * time.Millisecond):
	}

	f.svc.landFlight(user)

	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("background grading did not start after the user grading finished")
	}
}

func TestBackgroundTurnCapsBackgroundGradings(t *testing.T) {
	f := newFixture("public")
	f.svc.BatchConcurrency = 2
	f.svc.BatchPoll = 5 * time.Millisecond

	f.svc.trackFlight(uuid.New(), true)

	release, err := f.svc.backgroundTurn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	if _, err := f.svc.backgroundTurn(ctx); err == nil {
		t.Fatal("third background turn granted with a concurrency of 2")
	}
}

func TestStaleFlightsStopBlocking(t *testing.T) {
	f := newFixture("public")
	f.svc.trackFlight(uuid.New(), false)

	if got := f.svc.ForegroundGradings(); got != 1 {
		t.Fatalf("foreground = %d, want 1", got)
	}

	// The fixture's clock is fixed; move it past the lock timeout.
	f.svc.now = func() time.Time { return testNow.Add(f.svc.lockTimeout() + time.Second) }

	if got := f.svc.ForegroundGradings(); got != 0 {
		t.Errorf("foreground = %d after the lock timeout, want 0", got)
	}
}

func TestUserGradingIsTrackedAndLandsOnResult(t *testing.T) {
	f := newFixture("public")

	_, _ = f.svc.GetGrading(userCtx(""), "org", "repo")
	if got := f.svc.ForegroundGradings(); got != 1 {
		t.Fatalf("foreground = %d after a user grading, want 1", got)
	}

	f.svc.landFlight(testRepoID)
	if got := f.svc.ForegroundGradings(); got != 0 {
		t.Errorf("foreground = %d after the result, want 0", got)
	}
}

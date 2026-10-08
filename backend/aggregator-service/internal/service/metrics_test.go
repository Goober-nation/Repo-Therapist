package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"aggregator-service/internal/domain"
)

// recorder remembers what the service counted.
type recorder struct {
	mu        sync.Mutex
	requested []string // visibility/trigger
	completed []string // visibility/outcome
	durations []time.Duration
	limited   int
	llm       []string
	items     []string
	events    []string
}

func (r *recorder) GradingRequested(visibility, trigger string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.requested = append(r.requested, visibility+"/"+trigger)
}

func (r *recorder) GradingCompleted(visibility, outcome string, d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.completed = append(r.completed, visibility+"/"+outcome)
	r.durations = append(r.durations, d)
}

func (r *recorder) GradeLimiterRejected() { r.mu.Lock(); r.limited++; r.mu.Unlock() }
func (r *recorder) LLMRequest(o string)   { r.mu.Lock(); r.llm = append(r.llm, o); r.mu.Unlock() }
func (r *recorder) BatchItem(s string)    { r.mu.Lock(); r.items = append(r.items, s); r.mu.Unlock() }
func (r *recorder) BatchEvent(e string)   { r.mu.Lock(); r.events = append(r.events, e); r.mu.Unlock() }

func (r *recorder) snapshot() recorder {
	r.mu.Lock()
	defer r.mu.Unlock()

	return recorder{requested: append([]string(nil), r.requested...), completed: append([]string(nil), r.completed...),
		durations: append([]time.Duration(nil), r.durations...), limited: r.limited, llm: append([]string(nil), r.llm...),
		items: append([]string(nil), r.items...), events: append([]string(nil), r.events...)}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func TestUserGradingIsCountedAndTimedAtItsResult(t *testing.T) {
	f := newFixture("public")
	rec := &recorder{}
	f.svc.Metrics = rec

	clock := testNow
	f.svc.now = func() time.Time { return clock }

	if _, err := f.svc.GetGrading(userCtx(""), "org", "repo"); !errors.Is(err, domain.GradingPending) {
		t.Fatalf("err = %v", err)
	}

	if got := rec.snapshot().requested; !equal(got, []string{"public/user"}) {
		t.Fatalf("requested = %v", got)
	}

	clock = clock.Add(42 * time.Second)

	score := int16(80)
	if err := f.svc.SaveGradingResult(context.Background(), domain.RepoGrade{SourcecraftUUID: testRepoID, Score: &score}, "public", ""); err != nil {
		t.Fatal(err)
	}

	done := rec.snapshot()
	if !equal(done.completed, []string{"public/ok"}) || done.durations[0] != 42*time.Second {
		t.Fatalf("completed = %v, durations = %v", done.completed, done.durations)
	}
}

func TestFailedAndUntimedResultsAreCounted(t *testing.T) {
	f := newFixture("public")
	rec := &recorder{}
	f.svc.Metrics = rec

	// No request was seen by this process (it came before a restart): counted, with no duration.
	_ = f.svc.SaveGradingResult(context.Background(), domain.RepoGrade{SourcecraftUUID: testRepoID}, "public", "clone_failed")
	_ = f.svc.SaveGradingResult(context.Background(), domain.RepoGrade{SourcecraftUUID: testRepoID}, "bogus-mode", "")

	got := rec.snapshot()
	if !equal(got.completed, []string{"public/error"}) || got.durations[0] != 0 {
		t.Fatalf("completed = %v durations = %v: an invalid mode must not make a series", got.completed, got.durations)
	}
}

func TestScheduledGradingHasItsOwnTrigger(t *testing.T) {
	f := newFixture("public")
	rec := &recorder{}
	f.svc.Metrics = rec

	repo := domain.Repo{SourcecraftUUID: testRepoID, OrgSlug: "org", RepoSlug: "repo", Visibility: "public"}
	if err := f.svc.requestGrading(context.Background(), repo, false, false); !errors.Is(err, domain.GradingPending) {
		t.Fatal(err)
	}

	if got := rec.snapshot().requested; !equal(got, []string{"public/schedule"}) {
		t.Fatalf("requested = %v", got)
	}
}

func TestLimiterRejectionIsCountedAndNotCountedAsAGrading(t *testing.T) {
	f := newFixture("public")
	rec := &recorder{}
	f.svc.Metrics = rec
	f.svc.GradeLimiter = NewGradeLimiter(1, time.Hour)

	repo := domain.Repo{SourcecraftUUID: testRepoID, OrgSlug: "org", RepoSlug: "repo", Visibility: "public"}
	_ = f.svc.requestGrading(context.Background(), repo, false, true)
	f.repos.lockFree = true

	if err := f.svc.requestGrading(context.Background(), repo, false, true); !errors.Is(err, domain.ErrGradingRateLimited) {
		t.Fatalf("err = %v", err)
	}

	got := rec.snapshot()
	if got.limited != 1 || len(got.requested) != 1 {
		t.Fatalf("limited = %d, requested = %v", got.limited, got.requested)
	}
}

func TestBatchRunCountsItsTriggerItemsAndEvents(t *testing.T) {
	w, _, _ := newBatchWorld(t, "org/a", "org/b", "org/c")
	w.failSlug = "org/b"
	rec := &recorder{}
	w.svc.Metrics = rec

	batch, err := w.svc.StartBatch(context.Background(), "abc123", allFilter)
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, "completion", func() bool { return batchStatus(t, w.svc, batch.ID).Status == domain.BatchCompleted })
	waitFor(t, "complete event", func() bool { return len(rec.snapshot().events) == 2 })

	got := rec.snapshot()

	if !equal(got.requested, []string{"public/batch", "public/batch", "public/batch"}) {
		t.Errorf("requested = %v", got.requested)
	}

	ok, failed := 0, 0

	for _, status := range got.items {
		switch status {
		case "ok":
			ok++
		case "failed":
			failed++
		}
	}

	if ok != 2 || failed != 1 {
		t.Errorf("items = %v", got.items)
	}

	if !equal(got.events, []string{"start", "complete"}) {
		t.Errorf("events = %v", got.events)
	}
}

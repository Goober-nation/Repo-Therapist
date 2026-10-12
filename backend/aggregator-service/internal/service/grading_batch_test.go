package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"aggregator-service/internal/domain"

	"github.com/google/uuid"
)

// memBatches is an in-memory BatchRepository that behaves like the SQL one.
type memBatches struct {
	mu      sync.Mutex
	nextID  int64
	batches map[int64]*domain.Batch
	items   map[int64][]*domain.BatchItem
	public  []domain.BatchCandidate // what SelectBatchRepos returns, whatever the filter
	avg     float64
	lookup  func(uuid.UUID) (domain.Repo, bool)
	// lastFilter is the filter SelectBatchRepos saw last.
	lastFilter domain.BatchFilter
}

func (m *memBatches) GetBatchRepo(_ context.Context, id uuid.UUID) (domain.Repo, error) {
	if repo, ok := m.lookup(id); ok {
		return repo, nil
	}

	return domain.Repo{}, domain.ErrRepositoryNotFound
}

func newMemBatches(public ...domain.BatchCandidate) *memBatches {
	return &memBatches{batches: map[int64]*domain.Batch{}, items: map[int64][]*domain.BatchItem{}, public: public}
}

// SelectBatchRepos applies only the limit and the hand-picked list; public is already in rating order.
func (m *memBatches) SelectBatchRepos(_ context.Context, filter domain.BatchFilter) ([]domain.BatchCandidate, int, error) {
	m.lastFilter = filter

	var matched []domain.BatchCandidate

	for _, c := range m.public {
		if len(filter.Repos) == 0 || slices.ContainsFunc(filter.Repos, func(pick string) bool { return lowerASCII(pick) == lowerASCII(c.Slug()) }) {
			matched = append(matched, c)
		}
	}

	if len(matched) > filter.Limit {
		return matched[:filter.Limit], len(matched), nil
	}

	return matched, len(matched), nil
}

func (m *memBatches) AvgGradeSeconds(context.Context) (float64, error) { return m.avg, nil }

func (m *memBatches) CreateBatch(_ context.Context, actor string, filter domain.BatchFilter, repos []domain.BatchCandidate) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, b := range m.batches {
		if b.Active() {
			return 0, domain.ErrBatchActive
		}
	}

	m.nextID++
	m.batches[m.nextID] = &domain.Batch{ID: m.nextID, Status: domain.BatchRunning, ActorHash: actor, Filter: filter, CreatedAt: time.Now()}

	for _, r := range repos {
		m.items[m.nextID] = append(m.items[m.nextID], &domain.BatchItem{RepoID: r.RepoID, OrgSlug: r.OrgSlug, RepoSlug: r.RepoSlug, Status: domain.BatchItemPending})
	}

	return m.nextID, nil
}

func (m *memBatches) counted(id int64) domain.Batch {
	b := *m.batches[id]
	b.Total, b.Pending, b.Running, b.OK, b.Failed, b.Skipped = len(m.items[id]), 0, 0, 0, 0, 0

	for _, it := range m.items[id] {
		switch it.Status {
		case domain.BatchItemPending:
			b.Pending++
		case domain.BatchItemRunning:
			b.Running++
		case domain.BatchItemOK:
			b.OK++
		case domain.BatchItemFailed:
			b.Failed++
		case domain.BatchItemSkipped:
			b.Skipped++
		}
	}

	return b
}

func (m *memBatches) GetBatch(_ context.Context, id int64) (domain.Batch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.batches[id]; !ok {
		return domain.Batch{}, domain.ErrBatchNotFound
	}

	return m.counted(id), nil
}

func (m *memBatches) GetActiveBatch(_ context.Context) (domain.Batch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, b := range m.batches {
		if b.Active() {
			return m.counted(id), nil
		}
	}

	return domain.Batch{}, domain.ErrBatchNotFound
}

func (m *memBatches) ListBatches(_ context.Context, limit int) ([]domain.Batch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []domain.Batch
	for id := m.nextID; id >= 1 && len(out) < limit; id-- {
		out = append(out, m.counted(id))
	}

	return out, nil
}

func (m *memBatches) ListBatchItems(_ context.Context, id int64, status string, limit int) ([]domain.BatchItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []domain.BatchItem

	for _, it := range m.items[id] {
		if (status == "" || it.Status == status) && len(out) < limit {
			out = append(out, *it)
		}
	}

	return out, nil
}

func (m *memBatches) SetBatchStatus(_ context.Context, id int64, from []string, to string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	b, ok := m.batches[id]
	if !ok {
		return false, nil
	}

	for _, s := range from {
		if b.Status == s {
			b.Status = to
			return true, nil
		}
	}

	return false, nil
}

func (m *memBatches) FinishBatch(_ context.Context, id int64, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if b := m.batches[id]; b != nil && b.Active() {
		now := time.Now()
		b.Status, b.FinishedAt = status, &now
	}

	return nil
}

func (m *memBatches) ClaimNextBatchItem(_ context.Context, id int64) (domain.BatchItem, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.batches[id].Status != domain.BatchRunning {
		return domain.BatchItem{}, false, nil
	}

	for _, it := range m.items[id] {
		if it.Status == domain.BatchItemPending {
			it.Status = domain.BatchItemRunning
			return *it, true, nil
		}
	}

	return domain.BatchItem{}, false, nil
}

func (m *memBatches) FinishBatchItem(_ context.Context, id int64, repoID uuid.UUID, status, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, it := range m.items[id] {
		if it.RepoID == repoID {
			it.Status, it.Error = status, code
		}
	}

	return nil
}

func (m *memBatches) SkipPendingItems(_ context.Context, id int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	n := 0

	for _, it := range m.items[id] {
		if it.Status == domain.BatchItemPending {
			it.Status = domain.BatchItemSkipped
			n++
		}
	}

	return n, nil
}

func (m *memBatches) SkipOrphanedItems(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, items := range m.items {
		if m.batches[id].Active() {
			continue
		}

		for _, it := range items {
			if it.Status == domain.BatchItemRunning {
				it.Status = domain.BatchItemSkipped
			}
		}
	}

	return nil
}

func (m *memBatches) RequeueRunningItems(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, it := range m.items[id] {
		if it.Status == domain.BatchItemRunning {
			it.Status = domain.BatchItemPending
		}
	}

	return nil
}

// batchWorld is a thread-safe stand-in for the repos, locks and the grading pipeline: publishing a
// grading request starts a goroutine that, after a delay, behaves like grade-service plus the
// Aggregator's result consumer (grade stored or failure noted, lock released).
type batchWorld struct {
	svc *AggregatorService

	mu         sync.Mutex
	repos      map[uuid.UUID]domain.Repo
	locks      map[uuid.UUID]bool // true = busy
	grades     map[uuid.UUID]domain.RepoGrade
	inflight   int
	maxInfl    int
	requested  []string
	delay      time.Duration
	failSlug   string // grading reported as failed
	silentSlug string // lock released with neither a grade nor a failure
	hangSlug   string // never finishes
	gate       chan struct{}
}

type worldRepos struct{ w *batchWorld }

func (r worldRepos) CreateRepository(context.Context, domain.Repo) (uuid.UUID, error) {
	return uuid.Nil, nil
}
func (r worldRepos) UpdateRepository(context.Context, domain.Repo) (uuid.UUID, error) {
	return uuid.Nil, nil
}
func (r worldRepos) DeleteRepository(context.Context, uuid.UUID) error { return nil }
func (r worldRepos) PurgeRepository(context.Context, uuid.UUID) error  { return nil }
func (r worldRepos) GetRepositoryBySlugs(context.Context, string, string) (domain.Repo, error) {
	return domain.Repo{}, domain.ErrRepositoryNotFound
}

func (r worldRepos) GetRepositoryByID(_ context.Context, id uuid.UUID) (domain.Repo, error) {
	r.w.mu.Lock()
	defer r.w.mu.Unlock()

	repo, ok := r.w.repos[id]
	if !ok {
		return domain.Repo{}, domain.ErrRepositoryNotFound
	}

	return repo, nil
}

func (r worldRepos) GetRepositoryLock(_ context.Context, id uuid.UUID) (domain.RepoLock, error) {
	r.w.mu.Lock()
	defer r.w.mu.Unlock()

	status := "free"
	if r.w.locks[id] {
		status = "busy"
	}

	return domain.RepoLock{SourcecraftUUID: id, Status: status}, nil
}

func (r worldRepos) AcquireRepositoryLock(_ context.Context, id uuid.UUID) (bool, error) {
	r.w.mu.Lock()
	defer r.w.mu.Unlock()

	if r.w.locks[id] {
		return false, nil
	}

	r.w.locks[id] = true

	return true, nil
}

func (r worldRepos) ReleaseRepositoryLock(_ context.Context, id uuid.UUID) error {
	r.w.mu.Lock()
	defer r.w.mu.Unlock()

	r.w.locks[id] = false

	return nil
}

type worldGrades struct {
	domain.GradesRepository
	w *batchWorld
}

func (g worldGrades) GetPublicGradeByRepoID(_ context.Context, id uuid.UUID) (domain.RepoGrade, error) {
	g.w.mu.Lock()
	defer g.w.mu.Unlock()

	grade, ok := g.w.grades[id]
	if !ok {
		return domain.RepoGrade{}, domain.ErrGradeNotFound
	}

	return grade, nil
}

type worldPublisher struct{ w *batchWorld }

func (p worldPublisher) Publish(_ context.Context, event domain.GradingRequestedEvent) error {
	w := p.w
	slug := event.OrgSlug + "/" + event.RepoSlug

	w.mu.Lock()
	w.requested = append(w.requested, slug)
	w.inflight++

	if w.inflight > w.maxInfl {
		w.maxInfl = w.inflight
	}

	var id uuid.UUID

	for rid, repo := range w.repos {
		if repo.OrgSlug == event.OrgSlug && repo.RepoSlug == event.RepoSlug {
			id = rid
		}
	}
	w.mu.Unlock()

	go func() {
		if slug == w.hangSlug {
			return
		}

		if w.gate != nil {
			<-w.gate
		}

		time.Sleep(w.delay)

		w.mu.Lock()
		w.inflight--

		switch slug {
		case w.failSlug:
			w.mu.Unlock()
			w.svc.gradingFailed(llmKey(id, domain.VisibilityPublic))
			w.mu.Lock()
		case w.silentSlug:
		default:
			at := time.Now()
			score := int16(80)
			w.grades[id] = domain.RepoGrade{SourcecraftUUID: id, Score: &score, GradedAt: &at}
		}

		w.locks[id] = false
		w.mu.Unlock()
	}()

	return nil
}

// lockedAudit is an audit repository safe to write from the batch worker and read from the test.
type lockedAudit struct {
	fakeAuditRepo
	mu sync.Mutex
}

func (a *lockedAudit) InsertAudit(ctx context.Context, event domain.AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.fakeAuditRepo.InsertAudit(ctx, event)
}

func (a *lockedAudit) events() []domain.AuditEvent {
	a.mu.Lock()
	defer a.mu.Unlock()

	return append([]domain.AuditEvent(nil), a.inserted...)
}

func newBatchWorld(t *testing.T, slugs ...string) (*batchWorld, *memBatches, *lockedAudit) {
	t.Helper()

	w := &batchWorld{repos: map[uuid.UUID]domain.Repo{}, locks: map[uuid.UUID]bool{}, grades: map[uuid.UUID]domain.RepoGrade{}, delay: 5 * time.Millisecond}

	var candidates []domain.BatchCandidate

	for _, slug := range slugs {
		org, name, _ := strings.Cut(slug, "/")
		id := uuid.New()
		w.repos[id] = domain.Repo{SourcecraftUUID: id, OrgSlug: org, RepoSlug: name, Visibility: domain.VisibilityPublic}
		candidates = append(candidates, domain.BatchCandidate{RepoID: id, OrgSlug: org, RepoSlug: name})
	}

	batches := newMemBatches(candidates...)
	batches.lookup = func(id uuid.UUID) (domain.Repo, bool) {
		w.mu.Lock()
		defer w.mu.Unlock()

		repo, ok := w.repos[id]

		return repo, ok
	}
	audit := &lockedAudit{}

	s := NewAggregatorService(worldRepos{w}, worldGrades{w: w}, nil, &batchWrapper{}, worldPublisher{w}, nil, time.Second, "")
	s.BatchRepo = batches
	s.AuditRepo = audit
	s.BatchPoll = 2 * time.Millisecond
	s.BatchItemTimeout = 2 * time.Second
	s.now = time.Now
	w.svc = s

	return w, batches, audit
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", what)
}

func batchStatus(t *testing.T, s *AggregatorService, id int64) domain.Batch {
	t.Helper()

	b, _, err := s.BatchStatus(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}

	return b
}

// allFilter matches every repo; the limit is far above any test's repo count.
var allFilter = domain.BatchFilter{Limit: 1000}

func TestPreviewBatchCountsSamplesAndEstimates(t *testing.T) {
	slugs := make([]string, 25)
	for i := range slugs {
		slugs[i] = fmt.Sprintf("org/repo%02d", i)
	}

	w, batches, _ := newBatchWorld(t, slugs...)
	batches.avg = 60

	preview, err := w.svc.PreviewBatch(context.Background(), allFilter)
	if err != nil {
		t.Fatal(err)
	}

	if preview.Count != 25 || preview.Matched != 25 || len(preview.Sample) != domain.PreviewSampleSize || preview.Sample[0] != "org/repo00" {
		t.Fatalf("preview = %+v", preview)
	}

	// 25 repos at 3 in flight is 9 rounds of the measured 60 seconds.
	if preview.EstimateSeconds != 9*60 {
		t.Fatalf("estimate = %d, want %d", preview.EstimateSeconds, 9*60)
	}

	if len(batches.batches) != 0 {
		t.Fatal("a preview must not create a batch")
	}
}

func TestPreviewBatchUsesDefaultEstimateAndReportsUnmatchedPicks(t *testing.T) {
	w, _, _ := newBatchWorld(t, "org/a", "org/b")

	preview, err := w.svc.PreviewBatch(context.Background(), domain.BatchFilter{Limit: 10, Repos: []string{"org/a", "ORG/B", "org/missing"}})
	if err != nil {
		t.Fatal(err)
	}

	if len(preview.Unmatched) != 1 || preview.Unmatched[0] != "org/missing" {
		t.Fatalf("unmatched = %v", preview.Unmatched)
	}

	if preview.EstimateSeconds != DefaultBatchEstimateSeconds {
		t.Fatalf("estimate = %d, want one round of the default", preview.EstimateSeconds)
	}
}

func TestBatchLimitAboveTheStandMaximumIsRefused(t *testing.T) {
	w, batches, _ := newBatchWorld(t, "org/a", "org/b", "org/c")
	w.svc.BatchMaxLimit = 2

	for _, call := range []func() error{
		func() error {
			_, err := w.svc.PreviewBatch(context.Background(), domain.BatchFilter{Limit: 3})
			return err
		},
		func() error {
			_, err := w.svc.StartBatch(context.Background(), "abc", domain.BatchFilter{Limit: 3})
			return err
		},
	} {
		if err := call(); !errors.Is(err, domain.ErrBatchLimitTooHigh) || !errors.Is(err, domain.ErrInvalidBatch) {
			t.Fatalf("limit 3 over max 2: %v", err)
		}
	}

	if len(batches.batches) != 0 {
		t.Fatal("a refused start must not create a batch")
	}

	if batch, err := w.svc.StartBatch(context.Background(), "abc", domain.BatchFilter{Limit: 2}); err != nil || batch.Total != 2 {
		t.Fatalf("limit at the max: total %d, %v", batch.Total, err)
	}
}

func TestPreviewBatchTakesTheLimitAndRefusesInvalidFilters(t *testing.T) {
	w, batches, _ := newBatchWorld(t, "org/a", "org/b", "org/c")

	preview, err := w.svc.PreviewBatch(context.Background(), domain.BatchFilter{Limit: 2, OnlyRegrade: true})
	if err != nil || preview.Matched != 3 || preview.Count != 2 || strings.Join(preview.Sample, ",") != "org/a,org/b" {
		t.Fatalf("preview = %+v, err %v", preview, err)
	}
	if !batches.lastFilter.OnlyRegrade {
		t.Error("only_regrade must reach the selection")
	}

	// A pick below the limit's cut is not reported as missing.
	preview, err = w.svc.PreviewBatch(context.Background(), domain.BatchFilter{Limit: 1, Repos: []string{"org/a", "org/c"}})
	if err != nil || preview.Count != 1 || len(preview.Unmatched) != 0 {
		t.Fatalf("cut picks: %+v, err %v", preview, err)
	}

	if _, err := w.svc.PreviewBatch(context.Background(), domain.BatchFilter{Languages: []string{"go"}}); !errors.Is(err, domain.ErrInvalidBatch) {
		t.Fatalf("no limit: %v", err)
	}

	w.svc.BatchRepo = nil
	if _, err := w.svc.PreviewBatch(context.Background(), allFilter); !errors.Is(err, domain.ErrInvalidBatch) {
		t.Fatalf("no repository: %v", err)
	}
}

func TestStartBatchRunsEveryRepoWithBoundedConcurrency(t *testing.T) {
	slugs := make([]string, 8)
	for i := range slugs {
		slugs[i] = fmt.Sprintf("org/repo%d", i)
	}

	w, _, audit := newBatchWorld(t, slugs...)
	w.delay = 30 * time.Millisecond

	batch, err := w.svc.StartBatch(context.Background(), "abc123", allFilter)
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, "completion", func() bool { return batchStatus(t, w.svc, batch.ID).Status == domain.BatchCompleted })

	done := batchStatus(t, w.svc, batch.ID)
	if done.Total != 8 || done.OK != 8 || done.Failed != 0 || done.FinishedAt == nil {
		t.Fatalf("batch = %+v", done)
	}

	w.mu.Lock()
	maxInflight, requested := w.maxInfl, len(w.requested)
	w.mu.Unlock()

	if requested != 8 {
		t.Fatalf("requested %d gradings, want 8", requested)
	}

	if maxInflight > DefaultBatchConcurrency {
		t.Fatalf("%d gradings in flight, limit is %d", maxInflight, DefaultBatchConcurrency)
	}

	if maxInflight < 2 {
		t.Fatalf("only %d in flight at once: the batch is not running in parallel", maxInflight)
	}

	var actions []string
	for _, e := range audit.events() {
		actions = append(actions, e.Action+"/"+e.ActorKind)
	}

	if strings.Join(actions, ",") != "batch_grading.start/admin,batch_grading.complete/system" {
		t.Fatalf("audit = %v", actions)
	}

	if audit.events()[0].ActorHash != "abc123" || audit.events()[0].Target != fmt.Sprint(batch.ID) {
		t.Fatalf("start audit = %+v", audit.events()[0])
	}
}

func TestBatchRecordsPerRepoFailuresWithFixedCodes(t *testing.T) {
	w, _, _ := newBatchWorld(t, "org/good", "org/bad", "org/silent", "org/hang")
	w.failSlug, w.silentSlug, w.hangSlug = "org/bad", "org/silent", "org/hang"
	w.svc.BatchItemTimeout = 150 * time.Millisecond

	batch, err := w.svc.StartBatch(context.Background(), "abc123", allFilter)
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, "completion", func() bool { return batchStatus(t, w.svc, batch.ID).Status == domain.BatchCompleted })

	status, failures, err := w.svc.BatchStatus(context.Background(), batch.ID)
	if err != nil {
		t.Fatal(err)
	}

	if status.OK != 1 || status.Failed != 3 {
		t.Fatalf("batch = %+v", status)
	}

	got := map[string]string{}
	for _, f := range failures {
		got[f.OrgSlug+"/"+f.RepoSlug] = f.Error
	}

	want := map[string]string{"org/bad": domain.BatchErrorGrading, "org/silent": domain.BatchErrorNoGrade, "org/hang": domain.BatchErrorTimeout}
	for slug, code := range want {
		if got[slug] != code {
			t.Errorf("%s: error %q, want %q (all: %v)", slug, got[slug], code, got)
		}
	}
}

func TestOnlyOneBatchRunsAtATime(t *testing.T) {
	w, _, _ := newBatchWorld(t, "org/a", "org/b")
	w.gate = make(chan struct{})

	first, err := w.svc.StartBatch(context.Background(), "abc123", allFilter)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := w.svc.StartBatch(context.Background(), "abc123", allFilter); !errors.Is(err, domain.ErrBatchActive) {
		t.Fatalf("second batch: %v", err)
	}

	close(w.gate)
	waitFor(t, "completion", func() bool { return batchStatus(t, w.svc, first.ID).Status == domain.BatchCompleted })

	if _, err := w.svc.StartBatch(context.Background(), "abc123", allFilter); err != nil {
		t.Fatalf("a new batch after completion: %v", err)
	}
}

func TestStartBatchRefusesEmptyAndOversizedSelections(t *testing.T) {
	w, batches, _ := newBatchWorld(t)

	if _, err := w.svc.StartBatch(context.Background(), "abc123", allFilter); !errors.Is(err, domain.ErrInvalidBatch) {
		t.Fatalf("nothing selected: %v", err)
	}

	if len(batches.batches) != 0 {
		t.Fatal("a refused start must not create a batch")
	}

	// No batch cap: a large match is cut by the limit, never refused.
	slugs := make([]string, 600)
	for i := range slugs {
		slugs[i] = fmt.Sprintf("org/r%03d", i)
	}

	w, batches, _ = newBatchWorld(t, slugs...)

	batch, err := w.svc.StartBatch(context.Background(), "abc123", domain.BatchFilter{Limit: 550})
	if err != nil || batch.Total != 550 {
		t.Fatalf("limit 550 of 600: total %d, %v", batch.Total, err)
	}
}

func TestPauseStopsNewGradingsAndResumeContinues(t *testing.T) {
	slugs := []string{"org/a", "org/b", "org/c", "org/d", "org/e", "org/f"}
	w, _, audit := newBatchWorld(t, slugs...)
	w.svc.BatchConcurrency = 1
	w.gate = make(chan struct{}, 16)

	batch, err := w.svc.StartBatch(context.Background(), "abc123", allFilter)
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, "first request", func() bool { w.mu.Lock(); defer w.mu.Unlock(); return len(w.requested) == 1 })

	paused, err := w.svc.ControlBatch(context.Background(), "abc123", batch.ID, domain.BatchActionPause)
	if err != nil || paused.Status != domain.BatchPaused {
		t.Fatalf("pause: %+v, %v", paused, err)
	}

	w.gate <- struct{}{} // the in-flight one finishes
	waitFor(t, "in-flight item to finish", func() bool { return batchStatus(t, w.svc, batch.ID).OK == 1 })

	time.Sleep(60 * time.Millisecond)

	w.mu.Lock()
	requested := len(w.requested)
	w.mu.Unlock()

	if requested != 1 {
		t.Fatalf("%d gradings requested while paused, want 1", requested)
	}

	if _, err := w.svc.ControlBatch(context.Background(), "abc123", batch.ID, domain.BatchActionPause); !errors.Is(err, domain.ErrBatchState) {
		t.Fatalf("pausing a paused batch: %v", err)
	}

	if _, err := w.svc.ControlBatch(context.Background(), "abc123", batch.ID, domain.BatchActionResume); err != nil {
		t.Fatal(err)
	}

	for range slugs {
		w.gate <- struct{}{}
	}

	waitFor(t, "completion", func() bool { return batchStatus(t, w.svc, batch.ID).Status == domain.BatchCompleted })

	if done := batchStatus(t, w.svc, batch.ID); done.OK != 6 {
		t.Fatalf("batch = %+v", done)
	}

	// completeBatch records the audit entry right after the status, so it can lag behind it.
	waitFor(t, "completion audit", func() bool { return len(audit.events()) == 4 })

	var actions []string
	for _, e := range audit.events() {
		actions = append(actions, e.Action)
	}

	sort.Strings(actions)

	if strings.Join(actions, ",") != "batch_grading.complete,batch_grading.pause,batch_grading.resume,batch_grading.start" {
		t.Fatalf("audit = %v", actions)
	}
}

func TestCancelSkipsTheRestAndLetsInFlightFinish(t *testing.T) {
	w, _, _ := newBatchWorld(t, "org/a", "org/b", "org/c", "org/d")
	w.svc.BatchConcurrency = 1
	w.gate = make(chan struct{}, 8)

	batch, err := w.svc.StartBatch(context.Background(), "abc123", allFilter)
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, "first request", func() bool { w.mu.Lock(); defer w.mu.Unlock(); return len(w.requested) == 1 })

	cancelled, err := w.svc.ControlBatch(context.Background(), "abc123", batch.ID, domain.BatchActionCancel)
	if err != nil || cancelled.Status != domain.BatchCancelled || cancelled.Skipped != 3 {
		t.Fatalf("cancel: %+v, %v", cancelled, err)
	}

	w.gate <- struct{}{}
	waitFor(t, "in-flight item to finish", func() bool { return batchStatus(t, w.svc, batch.ID).OK == 1 })

	time.Sleep(60 * time.Millisecond)

	if final := batchStatus(t, w.svc, batch.ID); final.Status != domain.BatchCancelled || final.OK != 1 || final.Skipped != 3 {
		t.Fatalf("batch = %+v", final)
	}

	if _, err := w.svc.ControlBatch(context.Background(), "abc123", batch.ID, domain.BatchActionCancel); !errors.Is(err, domain.ErrBatchState) {
		t.Fatalf("cancelling a finished batch: %v", err)
	}

	if _, err := w.svc.ControlBatch(context.Background(), "abc123", 999, domain.BatchActionPause); !errors.Is(err, domain.ErrBatchNotFound) {
		t.Fatalf("unknown batch: %v", err)
	}

	if _, err := w.svc.ControlBatch(context.Background(), "abc123", batch.ID, "explode"); !errors.Is(err, domain.ErrInvalidBatch) {
		t.Fatalf("unknown action: %v", err)
	}
}

func TestBatchSkipsARepoThatIsNoLongerPublic(t *testing.T) {
	w, _, _ := newBatchWorld(t, "org/a", "org/b")

	for id, repo := range w.repos {
		if repo.RepoSlug == "b" {
			repo.Visibility = domain.VisibilityPrivate
			w.repos[id] = repo
		}
	}

	batch, err := w.svc.StartBatch(context.Background(), "abc123", allFilter)
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, "completion", func() bool { return batchStatus(t, w.svc, batch.ID).Status == domain.BatchCompleted })

	w.mu.Lock()
	requested := append([]string(nil), w.requested...)
	w.mu.Unlock()

	if len(requested) != 1 || requested[0] != "org/a" {
		t.Fatalf("requested = %v: a private repo must never be graded by a batch", requested)
	}

	if done := batchStatus(t, w.svc, batch.ID); done.OK != 1 || done.Skipped != 1 {
		t.Fatalf("batch = %+v", done)
	}
}

func TestResumeBatchRestartsARunningBatchAfterARestart(t *testing.T) {
	w, batches, _ := newBatchWorld(t, "org/a", "org/b", "org/c")

	id, err := batches.CreateBatch(context.Background(), "abc123", allFilter, batches.public)
	if err != nil {
		t.Fatal(err)
	}

	// A previous process claimed one item and died.
	if _, ok, _ := batches.ClaimNextBatchItem(context.Background(), id); !ok {
		t.Fatal("claim")
	}

	w.svc.ResumeBatch(context.Background())

	waitFor(t, "completion", func() bool { return batchStatus(t, w.svc, id).Status == domain.BatchCompleted })

	if done := batchStatus(t, w.svc, id); done.OK != 3 {
		t.Fatalf("batch = %+v: the item left running must be redone", done)
	}
}

func TestResumeBatchLeavesAPausedBatchPaused(t *testing.T) {
	w, batches, _ := newBatchWorld(t, "org/a")

	id, _ := batches.CreateBatch(context.Background(), "abc123", allFilter, batches.public)
	_, _ = batches.SetBatchStatus(context.Background(), id, []string{domain.BatchRunning}, domain.BatchPaused)

	w.svc.ResumeBatch(context.Background())
	time.Sleep(50 * time.Millisecond)

	if b := batchStatus(t, w.svc, id); b.Status != domain.BatchPaused || b.Pending != 1 {
		t.Fatalf("batch = %+v", b)
	}
}

func TestBatchStatusWithoutIDPrefersTheActiveBatchThenTheLatest(t *testing.T) {
	w, _, _ := newBatchWorld(t, "org/a")

	if _, _, err := w.svc.BatchStatus(context.Background(), 0); !errors.Is(err, domain.ErrBatchNotFound) {
		t.Fatalf("no batches yet: %v", err)
	}

	batch, _ := w.svc.StartBatch(context.Background(), "abc123", allFilter)
	waitFor(t, "completion", func() bool { return batchStatus(t, w.svc, batch.ID).Status == domain.BatchCompleted })

	latest, _, err := w.svc.BatchStatus(context.Background(), 0)
	if err != nil || latest.ID != batch.ID {
		t.Fatalf("latest = %+v, %v", latest, err)
	}
}

func TestNewBatchWaitsUntilACancelledBatchHasWoundDown(t *testing.T) {
	w, _, _ := newBatchWorld(t, "org/a", "org/b")
	w.svc.BatchConcurrency = 1
	w.gate = make(chan struct{}, 8)

	batch, err := w.svc.StartBatch(context.Background(), "abc123", allFilter)
	if err != nil {
		t.Fatal(err)
	}

	waitFor(t, "first request", func() bool { w.mu.Lock(); defer w.mu.Unlock(); return len(w.requested) == 1 })

	if _, err := w.svc.ControlBatch(context.Background(), "abc123", batch.ID, domain.BatchActionCancel); err != nil {
		t.Fatal(err)
	}

	// The batch is cancelled, but its repo is still in flight: another batch would exceed the limit.
	if _, err := w.svc.StartBatch(context.Background(), "abc123", allFilter); !errors.Is(err, domain.ErrBatchActive) {
		t.Fatalf("start while winding down: %v", err)
	}

	w.gate <- struct{}{}
	waitFor(t, "wind-down", func() bool { return !w.svc.batchWorkerBusy() })

	if _, err := w.svc.StartBatch(context.Background(), "abc123", allFilter); err != nil {
		t.Fatalf("start after wind-down: %v", err)
	}
}

func TestResumeBatchClearsItemsStuckRunningInAFinishedBatch(t *testing.T) {
	w, batches, _ := newBatchWorld(t, "org/a", "org/b")

	id, _ := batches.CreateBatch(context.Background(), "abc123", allFilter, batches.public)
	_, _, _ = batches.ClaimNextBatchItem(context.Background(), id)
	_ = batches.FinishBatch(context.Background(), id, domain.BatchCancelled)
	_, _ = batches.SkipPendingItems(context.Background(), id)

	w.svc.ResumeBatch(context.Background())

	if b := batchStatus(t, w.svc, id); b.Running != 0 || b.Skipped != 2 {
		t.Fatalf("batch = %+v", b)
	}
}

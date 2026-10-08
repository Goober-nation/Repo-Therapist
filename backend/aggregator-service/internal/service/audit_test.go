package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"aggregator-service/internal/domain"
)

type fakeAuditRepo struct {
	inserted []domain.AuditEvent
	entries  []domain.AuditEntry
	filter   domain.AuditFilter
	cutoff   time.Time
	removed  int64
	err      error
}

func (f *fakeAuditRepo) InsertAudit(_ context.Context, event domain.AuditEvent) error {
	f.inserted = append(f.inserted, event)
	return f.err
}

func (f *fakeAuditRepo) ListAudit(_ context.Context, filter domain.AuditFilter) ([]domain.AuditEntry, error) {
	f.filter = filter

	limit := filter.Limit
	if limit > len(f.entries) {
		limit = len(f.entries)
	}

	return f.entries[:limit], f.err
}

func (f *fakeAuditRepo) DeleteAuditBefore(_ context.Context, cutoff time.Time) (int64, error) {
	f.cutoff = cutoff
	return f.removed, f.err
}

func auditService(repo *fakeAuditRepo) *AggregatorService {
	s := NewAggregatorService(nil, nil, nil, nil, nil, nil, time.Second, "")
	s.AuditRepo = repo
	s.now = func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) }

	return s
}

func TestRecordAuditStoresValidEventAndRejectsInvalid(t *testing.T) {
	repo := &fakeAuditRepo{}
	s := auditService(repo)

	good := domain.AuditEvent{ActorKind: domain.AuditActorAdmin, Action: "admin.denied", Outcome: domain.AuditOutcomeDenied}
	if err := s.RecordAudit(context.Background(), good); err != nil || len(repo.inserted) != 1 {
		t.Fatalf("valid event: err %v, stored %d", err, len(repo.inserted))
	}

	bad := good
	bad.Action = "NOT VALID"

	if err := s.RecordAudit(context.Background(), bad); !errors.Is(err, domain.ErrInvalidAudit) || len(repo.inserted) != 1 {
		t.Fatalf("invalid event: err %v, stored %d (must not reach the repository)", err, len(repo.inserted))
	}
}

func TestAuditNotConfigured(t *testing.T) {
	s := NewAggregatorService(nil, nil, nil, nil, nil, nil, time.Second, "")

	if err := s.RecordAudit(context.Background(), domain.AuditEvent{}); !errors.Is(err, domain.ErrInvalidAudit) {
		t.Errorf("RecordAudit err = %v", err)
	}

	if _, err := s.ListAudit(context.Background(), domain.AuditFilter{}); !errors.Is(err, domain.ErrInvalidAudit) {
		t.Errorf("ListAudit err = %v", err)
	}
}

func entriesWithIDs(ids ...int64) []domain.AuditEntry {
	entries := make([]domain.AuditEntry, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, domain.AuditEntry{ID: id})
	}

	return entries
}

func TestListAuditPagesWithACursor(t *testing.T) {
	repo := &fakeAuditRepo{entries: entriesWithIDs(9, 8, 7, 6, 5)}
	s := auditService(repo)

	page, err := s.ListAudit(context.Background(), domain.AuditFilter{Limit: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(page.Entries) != 3 || page.Entries[2].ID != 7 || page.NextBeforeID != 7 {
		t.Errorf("page = %+v (want 3 entries and a cursor at the last one)", page)
	}

	if repo.filter.Limit != 4 {
		t.Errorf("repository limit = %d, want limit+1 to detect a next page", repo.filter.Limit)
	}

	last, err := s.ListAudit(context.Background(), domain.AuditFilter{Limit: 5})
	if err != nil || len(last.Entries) != 5 || last.NextBeforeID != 0 {
		t.Errorf("last page = %+v, err %v (cursor must be 0 when the log is exhausted)", last, err)
	}
}

func TestListAuditDefaultsCapsAndValidates(t *testing.T) {
	repo := &fakeAuditRepo{}
	s := auditService(repo)

	if _, err := s.ListAudit(context.Background(), domain.AuditFilter{}); err != nil || repo.filter.Limit != domain.DefaultAuditPageSize+1 {
		t.Errorf("default limit = %d, err %v", repo.filter.Limit, err)
	}

	if _, err := s.ListAudit(context.Background(), domain.AuditFilter{Limit: 100000}); err != nil || repo.filter.Limit != domain.MaxAuditPageSize+1 {
		t.Errorf("capped limit = %d, err %v", repo.filter.Limit, err)
	}

	for name, filter := range map[string]domain.AuditFilter{
		"negative limit":  {Limit: -1},
		"negative cursor": {BeforeID: -5},
		"unknown outcome": {Outcome: "maybe"},
		"unknown scope":   {Scope: "everyone"},
		"huge action":     {Action: string(make([]byte, 200))},
	} {
		if _, err := s.ListAudit(context.Background(), filter); !errors.Is(err, domain.ErrInvalidAudit) {
			t.Errorf("%s: err = %v, want ErrInvalidAudit", name, err)
		}
	}
}

func TestPurgeAuditUsesRetentionCutoff(t *testing.T) {
	repo := &fakeAuditRepo{removed: 4}
	s := auditService(repo)

	s.purgeAudit(context.Background(), 30)

	want := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	if !repo.cutoff.Equal(want) {
		t.Errorf("cutoff = %v, want %v", repo.cutoff, want)
	}
}

func TestStartAuditRetentionDisabledDoesNothing(t *testing.T) {
	repo := &fakeAuditRepo{}
	s := auditService(repo)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.StartAuditRetention(ctx, 0)
	time.Sleep(20 * time.Millisecond)

	if !repo.cutoff.IsZero() {
		t.Error("a non-positive retention must keep the log and never purge")
	}
}

func TestListAuditPassesTheScopeOn(t *testing.T) {
	repo := &fakeAuditRepo{}
	s := auditService(repo)

	for _, scope := range []string{"", domain.AuditScopeAdmin, domain.AuditScopeUsers} {
		if _, err := s.ListAudit(context.Background(), domain.AuditFilter{Scope: scope}); err != nil || repo.filter.Scope != scope {
			t.Errorf("scope %q: got %q, err %v", scope, repo.filter.Scope, err)
		}
	}
}

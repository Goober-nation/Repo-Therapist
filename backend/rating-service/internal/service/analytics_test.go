package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"rating-service/internal/domain"
)

type fakeAnalytics struct {
	calls    int
	analytic domain.PlatformAnalytics
	err      error
	params   domain.AnalyticsParams

	total   int
	rows    []domain.PublicGradeRow
	limit   int
	offset  int
	listErr error
}

func (f *fakeAnalytics) GetPlatformAnalytics(ctx context.Context, p domain.AnalyticsParams) (domain.PlatformAnalytics, error) {
	f.calls++
	f.params = p

	return f.analytic, f.err
}

func (f *fakeAnalytics) CountPublicGrades(ctx context.Context) (int, error) { return f.total, nil }

func (f *fakeAnalytics) ListPublicGrades(ctx context.Context, limit, offset int) ([]domain.PublicGradeRow, error) {
	f.limit, f.offset = limit, offset

	return f.rows, f.listErr
}

func newAnalyticsService(f *fakeAnalytics, ttl time.Duration) *RatingService {
	s := NewRatingService(&fakeRepo{}, 0)
	s.SetAnalytics(f, ttl)

	return s
}

func TestPlatformAnalyticsIsCachedForTTL(t *testing.T) {
	fake := &fakeAnalytics{analytic: domain.PlatformAnalytics{GradedRepos: 7}}
	s := newAnalyticsService(fake, time.Hour)

	for i := 0; i < 3; i++ {
		got, err := s.GetPlatformAnalytics(context.Background())
		if err != nil || got.GradedRepos != 7 {
			t.Fatalf("call %d = %+v, %v", i, got, err)
		}
	}

	if fake.calls != 1 {
		t.Errorf("repository calls = %d, want 1 (cached)", fake.calls)
	}

	if fake.params.MinLanguageRepos != minLanguageRepos || fake.params.ImprovedDays != DefaultDynamicsDays {
		t.Errorf("params = %+v", fake.params)
	}
}

func TestPlatformAnalyticsErrorIsNotCached(t *testing.T) {
	fake := &fakeAnalytics{err: errors.New("db down")}
	s := newAnalyticsService(fake, time.Hour)

	if _, err := s.GetPlatformAnalytics(context.Background()); err == nil {
		t.Fatal("want the repository error")
	}

	fake.err = nil

	if _, err := s.GetPlatformAnalytics(context.Background()); err != nil {
		t.Fatalf("a later call must retry, got %v", err)
	}

	if fake.calls != 2 {
		t.Errorf("repository calls = %d, want 2", fake.calls)
	}
}

func TestPlatformAnalyticsRequiresConfiguration(t *testing.T) {
	_, err := NewRatingService(&fakeRepo{}, 0).GetPlatformAnalytics(context.Background())
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("err = %v, want ErrInvalidArgument", err)
	}
}

func TestListPublicGradesPagingAndDecoding(t *testing.T) {
	fake := &fakeAnalytics{
		total: 2500,
		rows: []domain.PublicGradeRow{{
			OrgSlug: "acme", Name: "tool", Score: 80,
			CategoryScores: []byte(`{"activity":{"no_data":false,"score":71.6},"issues":{"no_data":true,"score":null}}`),
		}},
	}
	s := newAnalyticsService(fake, 0)

	page, err := s.ListPublicGrades(context.Background(), 3, 500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fake.limit != 500 || fake.offset != 1000 {
		t.Errorf("limit/offset = %d/%d, want 500/1000", fake.limit, fake.offset)
	}

	if page.TotalCount != 2500 || page.Page != 3 || page.PageSize != 500 || len(page.Entries) != 1 {
		t.Fatalf("page = %+v", page)
	}

	c := page.Entries[0].Categories
	if len(c) != 2 || c[0].ID != "activity" || c[0].Score == nil || *c[0].Score != 72 || c[1].Score != nil {
		t.Errorf("categories = %+v (no-data must stay nil, not zero)", c)
	}
}

func TestListPublicGradesDefaultsAndCaps(t *testing.T) {
	fake := &fakeAnalytics{}
	s := newAnalyticsService(fake, 0)

	page, err := s.ListPublicGrades(context.Background(), 0, 0)
	if err != nil || page.Page != 1 || page.PageSize != DefaultExportPageSize || fake.offset != 0 {
		t.Fatalf("defaults = %+v, offset %d, err %v", page, fake.offset, err)
	}

	page, err = s.ListPublicGrades(context.Background(), 1, 50000)
	if err != nil || page.PageSize != MaxExportPageSize || fake.limit != MaxExportPageSize {
		t.Fatalf("cap = %+v, limit %d, err %v", page, fake.limit, err)
	}

	if _, err := s.ListPublicGrades(context.Background(), -1, 10); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("negative page: err = %v, want ErrInvalidArgument", err)
	}
}

func TestListPublicGradesRejectsBrokenCategoryJSON(t *testing.T) {
	fake := &fakeAnalytics{rows: []domain.PublicGradeRow{{CategoryScores: []byte("[1,2]")}}}

	if _, err := newAnalyticsService(fake, 0).ListPublicGrades(context.Background(), 1, 10); err == nil {
		t.Fatal("want a decode error")
	}
}

type cacheCounter struct{ hits, misses int }

func (c *cacheCounter) AnalyticsCache(hit bool) {
	if hit {
		c.hits++
	} else {
		c.misses++
	}
}

func TestAnalyticsCacheHitsAndMissesAreCounted(t *testing.T) {
	repo := &fakeAnalytics{}
	svc := NewRatingService(nil, 7)
	svc.SetAnalytics(repo, time.Hour)

	counter := &cacheCounter{}
	svc.SetCacheRecorder(counter)

	for range 3 {
		if _, err := svc.GetPlatformAnalytics(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	if counter.misses != 1 || counter.hits != 2 {
		t.Fatalf("misses = %d, hits = %d, want 1 and 2", counter.misses, counter.hits)
	}
}

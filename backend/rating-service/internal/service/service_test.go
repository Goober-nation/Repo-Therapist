package service

import (
	"context"
	"errors"
	"testing"

	"rating-service/internal/domain"
)

type fakeRepo struct {
	total    int
	countErr error
	rows     []domain.RankingRow
	listErr  error

	countLanguage string
	listQuery     *domain.RankingQuery
}

func (f *fakeRepo) CountRanking(ctx context.Context, language string) (int, error) {
	f.countLanguage = language
	return f.total, f.countErr
}

func (f *fakeRepo) ListRanking(ctx context.Context, q domain.RankingQuery) ([]domain.RankingRow, error) {
	f.listQuery = &q
	return f.rows, f.listErr
}

func TestGetRankingDefaults(t *testing.T) {
	repo := &fakeRepo{total: 1, rows: []domain.RankingRow{{SourcecraftUUID: "a"}}}

	page, err := NewRatingService(repo).GetRanking(context.Background(), domain.RankingRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := domain.RankingQuery{SortBy: domain.SortByScore, Limit: DefaultPageSize, Offset: 0}
	if *repo.listQuery != want {
		t.Errorf("query = %+v, want %+v", *repo.listQuery, want)
	}
	if page.Page != 1 || page.PageSize != DefaultPageSize {
		t.Errorf("page = %d, page_size = %d, want 1, %d", page.Page, page.PageSize, DefaultPageSize)
	}
}

func TestGetRankingPlacesAndOffset(t *testing.T) {
	repo := &fakeRepo{
		total: 25,
		rows:  []domain.RankingRow{{SourcecraftUUID: "a"}, {SourcecraftUUID: "b"}},
	}

	page, err := NewRatingService(repo).GetRanking(context.Background(), domain.RankingRequest{
		Page:     3,
		PageSize: 10,
		Language: "  Go ",
		SortBy:   domain.SortByLikes,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := domain.RankingQuery{Language: "Go", SortBy: domain.SortByLikes, Limit: 10, Offset: 20}
	if *repo.listQuery != want {
		t.Errorf("query = %+v, want %+v", *repo.listQuery, want)
	}
	if repo.countLanguage != "Go" {
		t.Errorf("count language = %q, want %q", repo.countLanguage, "Go")
	}
	if page.TotalCount != 25 {
		t.Errorf("total = %d, want 25", page.TotalCount)
	}
	if len(page.Entries) != 2 || page.Entries[0].Place != 21 || page.Entries[1].Place != 22 {
		t.Errorf("entries = %+v, want places 21, 22", page.Entries)
	}
	if page.Entries[1].SourcecraftUUID != "b" {
		t.Errorf("second entry = %q, want b", page.Entries[1].SourcecraftUUID)
	}
}

func TestGetRankingPastLastPageSkipsList(t *testing.T) {
	repo := &fakeRepo{total: 20}

	page, err := NewRatingService(repo).GetRanking(context.Background(), domain.RankingRequest{Page: 3, PageSize: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.listQuery != nil {
		t.Errorf("ListRanking called past the last page")
	}
	if page.Entries == nil || len(page.Entries) != 0 {
		t.Errorf("entries = %#v, want empty non-nil slice", page.Entries)
	}
	if page.TotalCount != 20 || page.Page != 3 {
		t.Errorf("page = %+v", page)
	}
}

func TestGetRankingValidation(t *testing.T) {
	cases := []struct {
		name string
		req  domain.RankingRequest
	}{
		{"negative page", domain.RankingRequest{Page: -1}},
		{"negative page size", domain.RankingRequest{PageSize: -1}},
		{"page size over max", domain.RankingRequest{PageSize: MaxPageSize + 1}},
		{"unknown sort", domain.RankingRequest{SortBy: "stars"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}

			_, err := NewRatingService(repo).GetRanking(context.Background(), tc.req)
			if !errors.Is(err, domain.ErrInvalidArgument) {
				t.Fatalf("err = %v, want ErrInvalidArgument", err)
			}
			if repo.listQuery != nil {
				t.Errorf("repository queried for invalid request")
			}
		})
	}
}

func TestGetRankingMaxPageSizeAllowed(t *testing.T) {
	repo := &fakeRepo{}

	page, err := NewRatingService(repo).GetRanking(context.Background(), domain.RankingRequest{PageSize: MaxPageSize})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page.PageSize != MaxPageSize {
		t.Errorf("page_size = %d, want %d", page.PageSize, MaxPageSize)
	}
}

func TestGetRankingRepositoryErrors(t *testing.T) {
	boom := errors.New("boom")

	if _, err := NewRatingService(&fakeRepo{countErr: boom}).GetRanking(context.Background(), domain.RankingRequest{}); !errors.Is(err, boom) {
		t.Errorf("count error = %v, want %v", err, boom)
	}

	if _, err := NewRatingService(&fakeRepo{total: 5, listErr: boom}).GetRanking(context.Background(), domain.RankingRequest{}); !errors.Is(err, boom) {
		t.Errorf("list error = %v, want %v", err, boom)
	}
}

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"rating-service/internal/domain"
)

type fakeRepo struct {
	total    int
	countErr error
	rows     []domain.RankingRow
	listErr  error

	countLanguage string
	countHideLow  bool
	listQuery     *domain.RankingQuery

	score    *int16
	scoreErr error
	scoreFor string

	summary    domain.PublicSummaryRow
	summaryErr error
	summaryFor string

	history    []domain.HistoryRow
	historyErr error
	historyFor string
	historyMax int

	ranked    []domain.RankedRow
	rankedErr error
}

func (f *fakeRepo) ListScoreHistory(ctx context.Context, orgSlug, repoSlug string, limit int) ([]domain.HistoryRow, error) {
	f.historyFor, f.historyMax = orgSlug+"/"+repoSlug, limit
	return f.history, f.historyErr
}

func (f *fakeRepo) ListRanked(ctx context.Context) ([]domain.RankedRow, error) {
	return f.ranked, f.rankedErr
}

func (f *fakeRepo) GetPublicSummary(ctx context.Context, orgSlug, repoSlug string) (domain.PublicSummaryRow, error) {
	f.summaryFor = orgSlug + "/" + repoSlug
	return f.summary, f.summaryErr
}

func (f *fakeRepo) GetPublicScore(ctx context.Context, orgSlug, repoSlug string) (*int16, error) {
	f.scoreFor = orgSlug + "/" + repoSlug
	return f.score, f.scoreErr
}

func (f *fakeRepo) CountRanking(ctx context.Context, language string, hideLowConfidence bool) (int, error) {
	f.countLanguage, f.countHideLow = language, hideLowConfidence
	return f.total, f.countErr
}

func (f *fakeRepo) ListRanking(ctx context.Context, q domain.RankingQuery) ([]domain.RankingRow, error) {
	f.listQuery = &q
	return f.rows, f.listErr
}

func TestGetRankingDefaults(t *testing.T) {
	repo := &fakeRepo{total: 1, rows: []domain.RankingRow{{SourcecraftUUID: "a"}}}

	page, err := NewRatingService(repo, 0).GetRanking(context.Background(), domain.RankingRequest{SortBy: domain.SortByScore})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := domain.RankingQuery{SortBy: domain.SortByScore, Limit: DefaultPageSize, Offset: 0, DynamicsDays: DefaultDynamicsDays}
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

	page, err := NewRatingService(repo, 0).GetRanking(context.Background(), domain.RankingRequest{
		Page:     3,
		PageSize: 10,
		Language: "  Go ",
		SortBy:   domain.SortByLikes,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := domain.RankingQuery{Language: "Go", SortBy: domain.SortByLikes, Limit: 10, Offset: 20, DynamicsDays: DefaultDynamicsDays}
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

	page, err := NewRatingService(repo, 0).GetRanking(context.Background(), domain.RankingRequest{Page: 3, PageSize: 10, SortBy: domain.SortByScore})
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
		{"empty sort", domain.RankingRequest{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{}

			_, err := NewRatingService(repo, 0).GetRanking(context.Background(), tc.req)
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

	page, err := NewRatingService(repo, 0).GetRanking(context.Background(), domain.RankingRequest{PageSize: MaxPageSize, SortBy: domain.SortByScore})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page.PageSize != MaxPageSize {
		t.Errorf("page_size = %d, want %d", page.PageSize, MaxPageSize)
	}
}

func TestGetRankingRepositoryErrors(t *testing.T) {
	boom := errors.New("boom")

	if _, err := NewRatingService(&fakeRepo{countErr: boom}, 0).GetRanking(context.Background(), domain.RankingRequest{SortBy: domain.SortByScore}); !errors.Is(err, boom) {
		t.Errorf("count error = %v, want %v", err, boom)
	}

	if _, err := NewRatingService(&fakeRepo{total: 5, listErr: boom}, 0).GetRanking(context.Background(), domain.RankingRequest{SortBy: domain.SortByScore}); !errors.Is(err, boom) {
		t.Errorf("list error = %v, want %v", err, boom)
	}
}

func TestGetPublicScoreTrimsAndValidates(t *testing.T) {
	score := int16(87)
	repo := &fakeRepo{score: &score}

	got, err := NewRatingService(repo, 0).GetPublicScore(context.Background(), " org ", "repo ")
	if err != nil || got == nil || *got != 87 || repo.scoreFor != "org/repo" {
		t.Fatalf("got=%v err=%v for=%q", got, err, repo.scoreFor)
	}

	if _, err := NewRatingService(repo, 0).GetPublicScore(context.Background(), "org", " "); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("empty repo slug err = %v, want ErrInvalidArgument", err)
	}
}

func TestGetPublicSummaryDecodesAndSortsCategories(t *testing.T) {
	score := int16(71)
	gradedAt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepo{summary: domain.PublicSummaryRow{
		Name:   "demo",
		WebURL: "https://sourcecraft.dev/o/demo",
		Score:  &score,
		CategoryScores: []byte(`{
			"issues": {"id": "issues", "no_data": false, "score": 64.6},
			"activity": {"id": "activity", "no_data": false, "score": 90},
			"code_health": {"id": "code_health", "no_data": true},
			"documentation": {"id": "documentation", "no_data": false, "score": 0}
		}`),
		MissingData: []string{"code_health"},
		GradedAt:    &gradedAt,
	}}

	got, err := NewRatingService(repo, 0).GetPublicSummary(context.Background(), " o ", "demo ")
	if err != nil || repo.summaryFor != "o/demo" {
		t.Fatalf("err=%v for=%q", err, repo.summaryFor)
	}

	if got.Score == nil || *got.Score != 71 || got.Name != "demo" || !got.GradedAt.Equal(gradedAt) {
		t.Errorf("summary = %+v", got)
	}

	wantIDs := []string{"activity", "code_health", "documentation", "issues"}
	if len(got.Categories) != len(wantIDs) {
		t.Fatalf("categories = %+v, want %d", got.Categories, len(wantIDs))
	}
	for i, id := range wantIDs {
		if got.Categories[i].ID != id {
			t.Errorf("categories[%d] = %q, want %q", i, got.Categories[i].ID, id)
		}
	}

	if got.Categories[1].Score != nil {
		t.Errorf("no-data category score = %d, want nil", *got.Categories[1].Score)
	}
	if got.Categories[2].Score == nil || *got.Categories[2].Score != 0 {
		t.Errorf("real zero must stay 0, got %v", got.Categories[2].Score)
	}
	if got.Categories[3].Score == nil || *got.Categories[3].Score != 65 {
		t.Errorf("rounded score = %v, want 65", got.Categories[3].Score)
	}
}

func TestGetPublicSummaryUngraded(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte(""), []byte("null"), []byte("{}")} {
		got, err := NewRatingService(&fakeRepo{summary: domain.PublicSummaryRow{Name: "demo", CategoryScores: raw}}, 0).
			GetPublicSummary(context.Background(), "o", "demo")
		if err != nil || got.Score != nil || len(got.Categories) != 0 {
			t.Errorf("raw=%q: got=%+v err=%v, want empty ungraded summary", raw, got, err)
		}
	}
}

func TestGetPublicSummaryErrors(t *testing.T) {
	if _, err := NewRatingService(&fakeRepo{}, 0).GetPublicSummary(context.Background(), "o", " "); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("empty slug err = %v, want ErrInvalidArgument", err)
	}

	if _, err := NewRatingService(&fakeRepo{summaryErr: domain.ErrNotFound}, 0).GetPublicSummary(context.Background(), "o", "r"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown repo err = %v, want ErrNotFound", err)
	}

	bad := &fakeRepo{summary: domain.PublicSummaryRow{CategoryScores: []byte("[1,2]")}}
	if _, err := NewRatingService(bad, 0).GetPublicSummary(context.Background(), "o", "r"); err == nil {
		t.Error("malformed category_scores: want error")
	}
}

func TestGetRankingPassesTheLowConfidenceToggleToCountAndList(t *testing.T) {
	repo := &fakeRepo{total: 1, rows: []domain.RankingRow{{SourcecraftUUID: "a"}}}

	if _, err := NewRatingService(repo, 0).GetRanking(context.Background(), domain.RankingRequest{SortBy: domain.SortByScore, HideLowConfidence: true}); err != nil {
		t.Fatal(err)
	}
	if !repo.countHideLow || !repo.listQuery.HideLowConfidence {
		t.Errorf("count hide=%v list hide=%v, want both true", repo.countHideLow, repo.listQuery.HideLowConfidence)
	}

	repo = &fakeRepo{total: 1, rows: []domain.RankingRow{{SourcecraftUUID: "a"}}}
	if _, err := NewRatingService(repo, 0).GetRanking(context.Background(), domain.RankingRequest{SortBy: domain.SortByScore}); err != nil {
		t.Fatal(err)
	}
	if repo.countHideLow || repo.listQuery.HideLowConfidence {
		t.Error("the toggle must default to off")
	}
}

func TestGetPublicSummaryCarriesTheFlagUnchanged(t *testing.T) {
	confidence := &domain.Confidence{Low: true, Reasons: []domain.ConfidenceReason{{Code: "LOW_CODE", Text: "t"}}}
	repo := &fakeRepo{summary: domain.PublicSummaryRow{Name: "demo", Confidence: confidence}}

	got, err := NewRatingService(repo, 0).GetPublicSummary(context.Background(), "o", "demo")
	if err != nil || got.Confidence != confidence {
		t.Errorf("got %+v err %v, want the flag passed through", got.Confidence, err)
	}

	got, err = NewRatingService(&fakeRepo{summary: domain.PublicSummaryRow{Name: "demo"}}, 0).GetPublicSummary(context.Background(), "o", "demo")
	if err != nil || got.Confidence != nil {
		t.Errorf("got %+v err %v, want nil (unknown) when there is no flag row", got.Confidence, err)
	}
}

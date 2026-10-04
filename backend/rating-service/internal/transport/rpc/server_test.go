package rpc

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"rating-service/internal/domain"
	pb "rating-service/internal/transport/gen"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeService struct {
	page  domain.RankingPage
	err   error
	req   *domain.RankingRequest
	score *int16

	summary    domain.PublicSummary
	summaryErr error
}

func (f *fakeService) GetPublicSummary(ctx context.Context, orgSlug, repoSlug string) (domain.PublicSummary, error) {
	return f.summary, f.summaryErr
}

func (f *fakeService) GetPublicScore(ctx context.Context, orgSlug, repoSlug string) (*int16, error) {
	return f.score, f.err
}

func (f *fakeService) GetRanking(ctx context.Context, req domain.RankingRequest) (domain.RankingPage, error) {
	f.req = &req
	return f.page, f.err
}

func TestGetRankingRequestMapping(t *testing.T) {
	cases := []struct {
		sortBy pb.SortBy
		want   domain.SortBy
	}{
		{pb.SortBy_SORT_BY_SCORE, domain.SortByScore},
		{pb.SortBy_SORT_BY_LAST_ACTIVITY, domain.SortByLastActivity},
		{pb.SortBy_SORT_BY_LIKES, domain.SortByLikes},
	}

	for _, tc := range cases {
		t.Run(tc.sortBy.String(), func(t *testing.T) {
			svc := &fakeService{}

			_, err := NewServer(svc).GetRanking(context.Background(), &pb.GetRankingRequest{
				Page:     2,
				PageSize: 15,
				Language: "Go",
				SortBy:   tc.sortBy,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			want := domain.RankingRequest{Page: 2, PageSize: 15, Language: "Go", SortBy: tc.want}
			if *svc.req != want {
				t.Errorf("request = %+v, want %+v", *svc.req, want)
			}
		})
	}
}

func TestGetRankingUnknownSortEnum(t *testing.T) {
	svc := &fakeService{}

	_, err := NewServer(svc).GetRanking(context.Background(), &pb.GetRankingRequest{SortBy: pb.SortBy(99)})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
	if svc.req != nil {
		t.Errorf("service called for unknown sort")
	}
}

func TestGetRankingResponseMapping(t *testing.T) {
	score := int16(87)
	likes := 12.5
	activity := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	svc := &fakeService{page: domain.RankingPage{
		TotalCount: 42,
		Page:       2,
		PageSize:   20,
		Entries: []domain.RankingEntry{
			{
				Place: 21,
				RankingRow: domain.RankingRow{
					SourcecraftUUID: "uuid-1",
					Name:            "repo",
					WebURL:          "https://sourcecraft.dev/org/repo",
					PublicScore:     &score,
					Likes:           &likes,
					Language:        domain.Language{Name: "Go", Color: "#00ADD8"},
					LastActivityAt:  &activity,
				},
			},
			{
				Place:      22,
				RankingRow: domain.RankingRow{SourcecraftUUID: "uuid-2", Name: "ungraded"},
			},
		},
	}}

	resp, err := NewServer(svc).GetRanking(context.Background(), &pb.GetRankingRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.GetTotalCount() != 42 || resp.GetPage() != 2 || resp.GetPageSize() != 20 {
		t.Errorf("paging = %d/%d/%d, want 42/2/20", resp.GetTotalCount(), resp.GetPage(), resp.GetPageSize())
	}
	if len(resp.GetEntries()) != 2 {
		t.Fatalf("entries = %d, want 2", len(resp.GetEntries()))
	}

	full := resp.GetEntries()[0]
	if full.GetPlace() != 21 || full.GetSourcecraftUuid() != "uuid-1" || full.GetName() != "repo" || full.GetWebUrl() != "https://sourcecraft.dev/org/repo" {
		t.Errorf("full entry identity = %+v", full)
	}
	if full.PublicScore == nil || full.GetPublicScore() != 87 {
		t.Errorf("public_score = %v, want 87", full.PublicScore)
	}
	if full.Likes == nil || full.GetLikes() != 12.5 {
		t.Errorf("likes = %v, want 12.5", full.Likes)
	}
	if full.GetLanguage().GetName() != "Go" || full.GetLanguage().GetColor() != "#00ADD8" {
		t.Errorf("language = %v, want Go/#00ADD8", full.GetLanguage())
	}
	if !full.GetLastActivityAt().AsTime().Equal(activity) {
		t.Errorf("last_activity_at = %v, want %v", full.GetLastActivityAt().AsTime(), activity)
	}

	// NULL columns must come back unset, not as zero values — 0 score and "no score" differ.
	empty := resp.GetEntries()[1]
	if empty.PublicScore != nil || empty.Likes != nil || empty.LastActivityAt != nil {
		t.Errorf("expected unset optional fields, got %+v", empty)
	}
	// Language is always set, with empty strings when unknown.
	if empty.Language == nil || empty.GetLanguage().GetName() != "" || empty.GetLanguage().GetColor() != "" {
		t.Errorf("language = %v, want set with empty name/color", empty.Language)
	}
}

func TestGetRankingErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"invalid argument", fmt.Errorf("%w: page must not be negative", domain.ErrInvalidArgument), codes.InvalidArgument},
		{"canceled", fmt.Errorf("list ranking: %w", context.Canceled), codes.Canceled},
		{"deadline", fmt.Errorf("list ranking: %w", context.DeadlineExceeded), codes.DeadlineExceeded},
		{"internal", errors.New("connection refused"), codes.Internal},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewServer(&fakeService{err: tc.err}).GetRanking(context.Background(), &pb.GetRankingRequest{})
			if status.Code(err) != tc.want {
				t.Errorf("code = %v, want %v", status.Code(err), tc.want)
			}
		})
	}
}

func TestInternalErrorDoesNotLeakDetails(t *testing.T) {
	_, err := NewServer(&fakeService{err: errors.New("password authentication failed for user")}).GetRanking(context.Background(), &pb.GetRankingRequest{})

	if msg := status.Convert(err).Message(); msg != "internal error" {
		t.Errorf("message = %q, want generic", msg)
	}
}

func TestGetPublicScore(t *testing.T) {
	score := int16(64)
	resp, err := NewServer(&fakeService{score: &score}).GetPublicScore(context.Background(), &pb.GetPublicScoreRequest{OrgSlug: "o", RepoSlug: "r"})
	if err != nil || resp.PublicScore == nil || *resp.PublicScore != 64 {
		t.Fatalf("resp=%v err=%v", resp, err)
	}

	resp, err = NewServer(&fakeService{}).GetPublicScore(context.Background(), &pb.GetPublicScoreRequest{OrgSlug: "o", RepoSlug: "r"})
	if err != nil || resp.PublicScore != nil {
		t.Fatalf("ungraded: resp=%v err=%v, want unset score", resp, err)
	}

	_, err = NewServer(&fakeService{err: domain.ErrNotFound}).GetPublicScore(context.Background(), &pb.GetPublicScoreRequest{OrgSlug: "o", RepoSlug: "r"})
	if status.Code(err) != codes.NotFound {
		t.Errorf("unknown repo code = %v, want NotFound", status.Code(err))
	}
}

func TestGetPublicSummaryMapping(t *testing.T) {
	score, activity := int16(71), 90
	gradedAt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	svc := &fakeService{summary: domain.PublicSummary{
		Name:        "demo",
		WebURL:      "https://sourcecraft.dev/o/demo",
		Score:       &score,
		Categories:  []domain.CategoryScore{{ID: "activity", Score: &activity}, {ID: "code_health"}},
		MissingData: []string{"code_health"},
		GradedAt:    &gradedAt,
	}}

	resp, err := NewServer(svc).GetPublicSummary(context.Background(), &pb.GetPublicSummaryRequest{OrgSlug: "o", RepoSlug: "demo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Name != "demo" || resp.WebUrl != "https://sourcecraft.dev/o/demo" || resp.PublicScore == nil || *resp.PublicScore != 71 {
		t.Errorf("resp = %v", resp)
	}
	if len(resp.Categories) != 2 || resp.Categories[0].Score == nil || *resp.Categories[0].Score != 90 || resp.Categories[1].Score != nil {
		t.Errorf("categories = %v, want activity=90 and code_health unset", resp.Categories)
	}
	if !resp.GradedAt.AsTime().Equal(gradedAt) || len(resp.MissingData) != 1 {
		t.Errorf("graded_at=%v missing=%v", resp.GradedAt, resp.MissingData)
	}
}

func TestGetPublicSummaryUngradedAndErrors(t *testing.T) {
	resp, err := NewServer(&fakeService{}).GetPublicSummary(context.Background(), &pb.GetPublicSummaryRequest{OrgSlug: "o", RepoSlug: "r"})
	if err != nil || resp.PublicScore != nil || resp.GradedAt != nil || len(resp.Categories) != 0 {
		t.Fatalf("ungraded: resp=%v err=%v", resp, err)
	}

	_, err = NewServer(&fakeService{summaryErr: domain.ErrNotFound}).GetPublicSummary(context.Background(), &pb.GetPublicSummaryRequest{OrgSlug: "o", RepoSlug: "r"})
	if status.Code(err) != codes.NotFound {
		t.Errorf("unknown repo code = %v, want NotFound", status.Code(err))
	}
}

func TestRankingEntryConfidenceMapping(t *testing.T) {
	flagged := &domain.Confidence{Low: true, Reasons: []domain.ConfidenceReason{{Code: "LOW_CODE", Text: "t"}}}
	clear := &domain.Confidence{Low: false, Reasons: []domain.ConfidenceReason{}}
	svc := &fakeService{page: domain.RankingPage{Entries: []domain.RankingEntry{
		{Place: 1, RankingRow: domain.RankingRow{SourcecraftUUID: "a", Confidence: flagged}},
		{Place: 2, RankingRow: domain.RankingRow{SourcecraftUUID: "b", Confidence: clear}},
		{Place: 3, RankingRow: domain.RankingRow{SourcecraftUUID: "c"}},
	}}}

	resp, err := NewServer(svc).GetRanking(context.Background(), &pb.GetRankingRequest{HideLowConfidence: true})
	if err != nil {
		t.Fatal(err)
	}

	if !svc.req.HideLowConfidence {
		t.Error("hide_low_confidence was not forwarded")
	}
	a, b, c := resp.Entries[0], resp.Entries[1], resp.Entries[2]
	if a.LowConfidence == nil || !*a.LowConfidence || len(a.ConfidenceReasons) != 1 || a.ConfidenceReasons[0].Code != "LOW_CODE" || a.ConfidenceReasons[0].Text != "t" {
		t.Errorf("flagged entry = %v", a)
	}
	if b.LowConfidence == nil || *b.LowConfidence || len(b.ConfidenceReasons) != 0 {
		t.Errorf("known-not-low entry = %v, want low_confidence=false", b)
	}
	if c.LowConfidence != nil {
		t.Errorf("unknown entry = %v, want low_confidence unset", c)
	}
}

func TestPublicSummaryConfidenceMapping(t *testing.T) {
	svc := &fakeService{summary: domain.PublicSummary{Name: "demo", Confidence: &domain.Confidence{Low: true, Reasons: []domain.ConfidenceReason{{Code: "LOW_HISTORY", Text: "h"}}}}}

	resp, err := NewServer(svc).GetPublicSummary(context.Background(), &pb.GetPublicSummaryRequest{OrgSlug: "o", RepoSlug: "demo"})
	if err != nil || resp.LowConfidence == nil || !*resp.LowConfidence || len(resp.ConfidenceReasons) != 1 || resp.ConfidenceReasons[0].Code != "LOW_HISTORY" {
		t.Fatalf("resp = %v err = %v", resp, err)
	}

	resp, err = NewServer(&fakeService{}).GetPublicSummary(context.Background(), &pb.GetPublicSummaryRequest{OrgSlug: "o", RepoSlug: "demo"})
	if err != nil || resp.LowConfidence != nil {
		t.Errorf("unknown flag: resp = %v err = %v, want low_confidence unset", resp, err)
	}
}

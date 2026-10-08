package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gateway-service/internal/domain"
	"gateway-service/internal/transport/rpc/gen/rank"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeStatsRating struct {
	rank.RatingClient

	stats    *rank.GetPlatformAnalyticsResponse
	pages    []*rank.ListPublicGradesResponse
	pageErrs map[int32]error
	asked    []int32
	sizes    []int32
}

func (f *fakeStatsRating) GetPlatformAnalytics(ctx context.Context, in *rank.GetPlatformAnalyticsRequest, opts ...grpc.CallOption) (*rank.GetPlatformAnalyticsResponse, error) {
	return f.stats, nil
}

func (f *fakeStatsRating) ListPublicGrades(ctx context.Context, in *rank.ListPublicGradesRequest, opts ...grpc.CallOption) (*rank.ListPublicGradesResponse, error) {
	f.asked = append(f.asked, in.GetPage())
	f.sizes = append(f.sizes, in.GetPageSize())

	if err := f.pageErrs[in.GetPage()]; err != nil {
		return nil, err
	}

	return f.pages[in.GetPage()-1], nil
}

func statsService(rating *fakeStatsRating) *GatewayService {
	return NewGatewayService(nil, rating, nil, allowAll{}, nil, nil, time.Second, time.Second, time.Second)
}

func TestGetPlatformStatsMapsEverythingAndKeepsEmptyListsNonNil(t *testing.T) {
	week := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	rating := &fakeStatsRating{stats: &rank.GetPlatformAnalyticsResponse{
		GradedRepos:  40,
		AvgScore:     71.5,
		Distribution: []*rank.ScoreBucket{{MinScore: 90, MaxScore: 100, Repos: 4}},
		Languages:    []*rank.LanguageStat{{Name: "Go", Color: "#00ADD8", Repos: 9, AvgScore: 80.2}},
		TopAdvice:    []*rank.AdviceStat{{CategoryId: "documentation", MetricId: "readme", Priority: "high", Repos: 21}},
		Trend:        []*rank.TrendPoint{{WeekStart: timestamppb.New(week), AvgScore: 70.1, Repos: 15}},
		ImprovedDays: 7,
		GeneratedAt:  timestamppb.New(week),
	}}

	got, err := statsService(rating).GetPlatformStats(context.Background(), "1.2.3.4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.GradedRepos != 40 || got.AvgScore != 71.5 || got.ImprovedDays != 7 {
		t.Errorf("totals = %+v", got)
	}

	if len(got.Distribution) != 1 || got.Distribution[0].Min != 90 || got.Distribution[0].Max != 100 {
		t.Errorf("distribution = %+v", got.Distribution)
	}

	if len(got.Languages) != 1 || got.Languages[0].Name != "Go" || got.Languages[0].AvgScore != 80.2 {
		t.Errorf("languages = %+v", got.Languages)
	}

	if len(got.TopAdvice) != 1 || got.TopAdvice[0].MetricID != "readme" {
		t.Errorf("advice = %+v", got.TopAdvice)
	}

	if len(got.Trend) != 1 || got.Trend[0].WeekStart == nil || !got.Trend[0].WeekStart.Equal(week) {
		t.Errorf("trend = %+v", got.Trend)
	}

	if got.Levels == nil || got.Categories == nil || got.MostImproved == nil {
		t.Error("empty lists must be non-nil so they encode as [], not null")
	}
}

func TestGetPlatformStatsUsesPublicLimiter(t *testing.T) {
	svc := statsService(&fakeStatsRating{stats: &rank.GetPlatformAnalyticsResponse{}})
	limiter := &countingLimiter{allow: false}
	svc.SetPublicAPILimiter(limiter)

	if _, err := svc.GetPlatformStats(context.Background(), "9.9.9.9"); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("err = %v, want ErrLimitExceeded", err)
	}

	if len(limiter.keys) != 1 || limiter.keys[0] != "rate_limit:gateway:public:ip:9.9.9.9" {
		t.Errorf("limiter keys = %v", limiter.keys)
	}
}

func gradePage(total, size int32, orgs ...string) *rank.ListPublicGradesResponse {
	resp := &rank.ListPublicGradesResponse{TotalCount: total, PageSize: size}
	for _, org := range orgs {
		resp.Entries = append(resp.Entries, &rank.PublicGradeEntry{OrgSlug: org, Name: "r", Score: 50})
	}

	return resp
}

func TestExportPublicGradesWalksAllPagesAndChecksLimiterOnce(t *testing.T) {
	rating := &fakeStatsRating{pages: []*rank.ListPublicGradesResponse{
		gradePage(5, 2, "a", "b"), gradePage(5, 2, "c", "d"), gradePage(5, 2, "e"),
	}}
	svc := statsService(rating)
	limiter := &countingLimiter{allow: true}
	svc.SetExportLimiter(limiter)

	var seen []string

	err := svc.ExportPublicGrades(context.Background(), "1.1.1.1", func(page domain.ExportPage) error {
		for _, grade := range page.Grades {
			seen = append(seen, grade.OrgSlug)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := len(seen); got != 5 || seen[0] != "a" || seen[4] != "e" {
		t.Errorf("seen = %v", seen)
	}

	if len(rating.asked) != 3 || rating.sizes[0] != exportPageSize {
		t.Errorf("pages asked = %v sizes %v", rating.asked, rating.sizes)
	}

	if len(limiter.keys) != 1 || limiter.keys[0] != "rate_limit:gateway:export:ip:1.1.1.1" {
		t.Errorf("limiter keys = %v (want one check for the whole export)", limiter.keys)
	}
}

func TestExportPublicGradesEmptyPlatformStillEmitsOnePage(t *testing.T) {
	rating := &fakeStatsRating{pages: []*rank.ListPublicGradesResponse{gradePage(0, 1000)}}
	emitted := 0

	err := statsService(rating).ExportPublicGrades(context.Background(), "ip", func(page domain.ExportPage) error {
		emitted++
		return nil
	})
	if err != nil || emitted != 1 {
		t.Fatalf("emitted %d, err %v (an empty export must still produce a valid empty file)", emitted, err)
	}
}

func TestExportPublicGradesLimitedAndErrors(t *testing.T) {
	svc := statsService(&fakeStatsRating{})
	svc.SetExportLimiter(&countingLimiter{allow: false})

	called := false
	err := svc.ExportPublicGrades(context.Background(), "ip", func(domain.ExportPage) error { called = true; return nil })

	if !errors.Is(err, domain.ErrLimitExceeded) || called {
		t.Fatalf("limited export: err %v, emitted %v", err, called)
	}

	boom := errors.New("rating down")
	rating := &fakeStatsRating{
		pages:    []*rank.ListPublicGradesResponse{gradePage(4, 2, "a", "b")},
		pageErrs: map[int32]error{2: boom},
	}
	emitted := 0

	err = statsService(rating).ExportPublicGrades(context.Background(), "ip", func(domain.ExportPage) error { emitted++; return nil })
	if err == nil || emitted != 1 {
		t.Fatalf("mid-export failure: emitted %d, err %v", emitted, err)
	}

	stop := errors.New("client gone")
	rating = &fakeStatsRating{pages: []*rank.ListPublicGradesResponse{gradePage(4, 2, "a", "b"), gradePage(4, 2, "c", "d")}}

	if err := statsService(rating).ExportPublicGrades(context.Background(), "ip", func(domain.ExportPage) error { return stop }); !errors.Is(err, stop) || len(rating.asked) != 1 {
		t.Fatalf("emit error must stop the walk: err %v, pages %v", err, rating.asked)
	}
}

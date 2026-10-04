package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gateway-service/internal/domain"
	"gateway-service/internal/transport/rpc/gen/rank"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeRating struct {
	rank.RatingClient

	resp *rank.GetPublicSummaryResponse
	err  error
	reqs []*rank.GetPublicSummaryRequest
}

func (f *fakeRating) GetPublicSummary(ctx context.Context, in *rank.GetPublicSummaryRequest, opts ...grpc.CallOption) (*rank.GetPublicSummaryResponse, error) {
	f.reqs = append(f.reqs, in)
	return f.resp, f.err
}

type countingLimiter struct {
	allow bool
	err   error
	keys  []string
}

func (l *countingLimiter) Allow(ctx context.Context, key string) (bool, error) {
	l.keys = append(l.keys, key)
	return l.allow, l.err
}

func newRatingService(rating *fakeRating) *GatewayService {
	return NewGatewayService(nil, rating, nil, allowAll{}, nil, nil, time.Second, time.Second, time.Second)
}

func int32Ptr(v int32) *int32 { return &v }

func TestGetPublicSummaryMapsResponse(t *testing.T) {
	gradedAt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	rating := &fakeRating{resp: &rank.GetPublicSummaryResponse{
		Name:        "demo",
		WebUrl:      "https://sourcecraft.dev/o/demo",
		PublicScore: int32Ptr(71),
		Categories: []*rank.CategoryScore{
			{Id: "activity", Score: int32Ptr(90)},
			{Id: "code_health"},
		},
		MissingData: []string{"code_health"},
		GradedAt:    timestamppb.New(gradedAt),
	}}

	got, err := newRatingService(rating).GetPublicSummary(context.Background(), "o", "demo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rating.reqs) != 1 || rating.reqs[0].GetOrgSlug() != "o" || rating.reqs[0].GetRepoSlug() != "demo" {
		t.Errorf("request = %v", rating.reqs)
	}
	if got.Name != "demo" || got.Score == nil || *got.Score != 71 || got.GradedAt == nil || !got.GradedAt.Equal(gradedAt) {
		t.Errorf("summary = %+v", got)
	}
	if len(got.Categories) != 2 || got.Categories[0].Score == nil || *got.Categories[0].Score != 90 || got.Categories[1].Score != nil {
		t.Errorf("categories = %+v, want activity=90 and code_health unset", got.Categories)
	}
}

func TestGetPublicSummaryUngraded(t *testing.T) {
	got, err := newRatingService(&fakeRating{resp: &rank.GetPublicSummaryResponse{Name: "demo"}}).GetPublicSummary(context.Background(), "o", "demo")
	if err != nil || got.Score != nil || got.GradedAt != nil || len(got.Categories) != 0 {
		t.Fatalf("got=%+v err=%v, want an empty ungraded summary", got, err)
	}
}

func TestGetPublicSummaryMapsNotFound(t *testing.T) {
	rating := &fakeRating{err: status.Error(codes.NotFound, "repository not found")}

	_, err := newRatingService(rating).GetPublicSummary(context.Background(), "o", "r")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetScoreUsesItsOwnLimiter(t *testing.T) {
	rating := &fakeRating{resp: &rank.GetPublicSummaryResponse{}}
	limiter := &countingLimiter{allow: true}
	svc := newRatingService(rating)
	svc.SetPublicAPILimiter(limiter)

	if _, err := svc.GetScore(context.Background(), "203.0.113.9", "o", "r"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(limiter.keys) != 1 || limiter.keys[0] != "rate_limit:gateway:public:ip:203.0.113.9" {
		t.Errorf("limiter keys = %v", limiter.keys)
	}
}

func TestGetScoreLimitedSkipsLookup(t *testing.T) {
	rating := &fakeRating{resp: &rank.GetPublicSummaryResponse{}}
	svc := newRatingService(rating)
	svc.SetPublicAPILimiter(&countingLimiter{allow: false})

	_, err := svc.GetScore(context.Background(), "ip", "o", "r")
	if !errors.Is(err, domain.ErrLimitExceeded) || len(rating.reqs) != 0 {
		t.Errorf("err = %v, lookups = %d, want ErrLimitExceeded without a lookup", err, len(rating.reqs))
	}
}

func TestGetScoreLimiterFailureFailsClosed(t *testing.T) {
	rating := &fakeRating{resp: &rank.GetPublicSummaryResponse{}}
	svc := newRatingService(rating)
	svc.SetPublicAPILimiter(&countingLimiter{err: errors.New("redis down")})

	if _, err := svc.GetScore(context.Background(), "ip", "o", "r"); err == nil || len(rating.reqs) != 0 {
		t.Errorf("err = %v, lookups = %d, want an error without a lookup", err, len(rating.reqs))
	}
}

func TestGetPublicSummaryIsNeverLimited(t *testing.T) {
	limiter := &countingLimiter{allow: false}
	svc := newRatingService(&fakeRating{resp: &rank.GetPublicSummaryResponse{}})
	svc.SetPublicAPILimiter(limiter)

	if _, err := svc.GetPublicSummary(context.Background(), "o", "r"); err != nil || len(limiter.keys) != 0 {
		t.Errorf("err = %v, limiter calls = %d: badges and the widget must not be limited", err, len(limiter.keys))
	}
}

func TestGetScoreWithoutLimiterIsUnlimited(t *testing.T) {
	if _, err := newRatingService(&fakeRating{resp: &rank.GetPublicSummaryResponse{}}).GetScore(context.Background(), "ip", "o", "r"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMapConfidence(t *testing.T) {
	if got := mapConfidence(nil, nil); got != nil {
		t.Errorf("unset flag = %+v, want nil (unknown)", got)
	}

	yes, no := true, false
	got := mapConfidence(&yes, []*rank.ConfidenceReason{{Code: "LOW_CODE", Text: "t"}})
	if got == nil || !got.Low || len(got.Reasons) != 1 || got.Reasons[0] != (domain.ConfidenceReason{Code: "LOW_CODE", Text: "t"}) {
		t.Errorf("flagged = %+v", got)
	}

	got = mapConfidence(&no, nil)
	if got == nil || got.Low || got.Reasons == nil || len(got.Reasons) != 0 {
		t.Errorf("known-not-low = %+v, want false with an empty non-nil list", got)
	}
}

func TestGetPublicSummaryAndRankingCarryTheFlag(t *testing.T) {
	yes := true
	rating := &fakeRating{resp: &rank.GetPublicSummaryResponse{LowConfidence: &yes, ConfidenceReasons: []*rank.ConfidenceReason{{Code: "LOW_COVERAGE", Text: "c"}}}}

	got, err := newRatingService(rating).GetPublicSummary(context.Background(), "o", "r")
	if err != nil || got.Confidence == nil || !got.Confidence.Low || got.Confidence.Reasons[0].Code != "LOW_COVERAGE" {
		t.Errorf("summary flag = %+v err %v", got.Confidence, err)
	}

	entry, err := mapRankingEntry(&rank.RankingEntry{SourcecraftUuid: "11111111-1111-1111-1111-111111111111", LowConfidence: &yes})
	if err != nil || entry.Confidence == nil || !entry.Confidence.Low {
		t.Errorf("ranking entry flag = %+v err %v", entry.Confidence, err)
	}
}

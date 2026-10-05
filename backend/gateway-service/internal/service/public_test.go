package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gateway-service/internal/domain"
	"gateway-service/internal/transport/rpc/gen/agg"
	"gateway-service/internal/transport/rpc/gen/rank"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeRating struct {
	rank.RatingClient

	resp *rank.GetPublicSummaryResponse
	err  error
	reqs []*rank.GetPublicSummaryRequest

	history     *rank.GetScoreHistoryResponse
	explanation *rank.GetPositionExplanationResponse
	otherReqs   []string
}

func (f *fakeRating) GetPublicSummary(ctx context.Context, in *rank.GetPublicSummaryRequest, opts ...grpc.CallOption) (*rank.GetPublicSummaryResponse, error) {
	f.reqs = append(f.reqs, in)
	return f.resp, f.err
}

func (f *fakeRating) GetScoreHistory(ctx context.Context, in *rank.GetScoreHistoryRequest, opts ...grpc.CallOption) (*rank.GetScoreHistoryResponse, error) {
	f.otherReqs = append(f.otherReqs, "history:"+in.GetOrgSlug()+"/"+in.GetRepoSlug())
	return f.history, f.err
}

func (f *fakeRating) GetPositionExplanation(ctx context.Context, in *rank.GetPositionExplanationRequest, opts ...grpc.CallOption) (*rank.GetPositionExplanationResponse, error) {
	f.otherReqs = append(f.otherReqs, "position:"+in.GetOrgSlug()+"/"+in.GetRepoSlug())
	return f.explanation, f.err
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

type fakeAggAnalytics struct {
	agg.AggregatorClient

	resp *agg.GetAnalyticsResponse
	err  error
	reqs []*agg.GetAnalyticsRequest
	auth []string
}

func (f *fakeAggAnalytics) GetAnalytics(ctx context.Context, in *agg.GetAnalyticsRequest, opts ...grpc.CallOption) (*agg.GetAnalyticsResponse, error) {
	f.reqs = append(f.reqs, in)
	md, _ := metadata.FromOutgoingContext(ctx)
	f.auth = md.Get("authorization")
	return f.resp, f.err
}

func TestGetAnalyticsForwardsTheCallerPATAndMapsTheResponse(t *testing.T) {
	gradedAt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	aggregator := &fakeAggAnalytics{resp: &agg.GetAnalyticsResponse{Analytics: []byte(`{"version":1}`), GradedAt: timestamppb.New(gradedAt)}}
	svc := NewGatewayService(aggregator, nil, nil, allowAll{}, nil, nil, time.Second, time.Second, time.Second)

	got, err := svc.GetAnalytics(context.Background(), "ip", "owner-pat", "org", "demo")
	if err != nil {
		t.Fatal(err)
	}

	if string(got.Analytics) != `{"version":1}` || got.GradedAt == nil || !got.GradedAt.Equal(gradedAt) {
		t.Errorf("got %+v", got)
	}
	if len(aggregator.reqs) != 1 || aggregator.reqs[0].GetOrgSlug() != "org" || aggregator.reqs[0].GetRepoSlug() != "demo" {
		t.Errorf("request = %v", aggregator.reqs)
	}
	if len(aggregator.auth) != 1 || aggregator.auth[0] != "Bearer owner-pat" {
		t.Errorf("authorization = %v, want the PAT as a bearer", aggregator.auth)
	}

	if _, err := svc.GetAnalytics(context.Background(), "ip", "", "org", "demo"); err != nil || len(aggregator.auth) != 0 {
		t.Errorf("without a PAT: err %v auth %v, want no authorization at all", err, aggregator.auth)
	}
}

func TestGetAnalyticsEmptyResponseIsNotAnError(t *testing.T) {
	svc := NewGatewayService(&fakeAggAnalytics{resp: &agg.GetAnalyticsResponse{}}, nil, nil, allowAll{}, nil, nil, time.Second, time.Second, time.Second)

	got, err := svc.GetAnalytics(context.Background(), "ip", "", "org", "demo")
	if err != nil || len(got.Analytics) != 0 || got.GradedAt != nil {
		t.Errorf("got %+v err %v, want empty analytics", got, err)
	}
}

func TestGetAnalyticsValidatesSlugsAndMapsErrors(t *testing.T) {
	aggregator := &fakeAggAnalytics{err: status.Error(codes.PermissionDenied, "no access")}
	svc := NewGatewayService(aggregator, nil, nil, allowAll{}, nil, nil, time.Second, time.Second, time.Second)

	if _, err := svc.GetAnalytics(context.Background(), "ip", "", "", "demo"); !errors.Is(err, domain.ErrInvalidRequest) || len(aggregator.reqs) != 0 {
		t.Errorf("empty org: err %v calls %d, want ErrInvalidRequest without a call", err, len(aggregator.reqs))
	}
	if _, err := svc.GetAnalytics(context.Background(), "ip", "", "org", "demo"); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("permission denied: err %v, want ErrForbidden", err)
	}
}

func TestGetScoreHistoryMapsPointsAndAppliesTheLimiter(t *testing.T) {
	when := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	low := true
	rating := &fakeRating{history: &rank.GetScoreHistoryResponse{Points: []*rank.HistoryPoint{
		{GradedAt: timestamppb.New(when), Score: 66, Categories: []*rank.CategoryScore{{Id: "activity", Score: int32Ptr(70)}, {Id: "issues"}}, LowConfidence: &low},
	}}}

	limiter := &countingLimiter{allow: true}
	svc := newRatingService(rating)
	svc.publicAPILimiter = limiter

	points, err := svc.GetScoreHistory(context.Background(), "203.0.113.9", "o", "demo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(limiter.keys) != 1 || limiter.keys[0] != "rate_limit:gateway:public:ip:203.0.113.9" || len(rating.otherReqs) != 1 || rating.otherReqs[0] != "history:o/demo" {
		t.Errorf("limiter keys = %v, requests = %v", limiter.keys, rating.otherReqs)
	}
	if len(points) != 1 || points[0].Score != 66 || !points[0].GradedAt.Equal(when) || points[0].LowConfidence == nil || !*points[0].LowConfidence {
		t.Fatalf("points = %+v", points)
	}
	if c := points[0].Categories; len(c) != 2 || c[0].Score == nil || *c[0].Score != 70 || c[1].Score != nil {
		t.Errorf("categories = %+v, want 70 and a nil no-data score", c)
	}

	limiter.allow = false
	if _, err := svc.GetScoreHistory(context.Background(), "ip", "o", "demo"); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Errorf("err = %v, want ErrLimitExceeded", err)
	}
	if len(rating.otherReqs) != 1 {
		t.Error("a rate-limited request must not reach the rating service")
	}

	rating.err = status.Error(codes.NotFound, "repository not found")
	svc.publicAPILimiter = nil
	if _, err := svc.GetScoreHistory(context.Background(), "ip", "o", "demo"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetPositionExplanationMapsAndHandlesUnranked(t *testing.T) {
	median, gap := int32(65), int32(-5)
	rating := &fakeRating{explanation: &rank.GetPositionExplanationResponse{
		Ranked: true, Place: 3, Total: 5, Score: 70, WeakestCategory: "activity",
		Above:      &rank.Neighbour{Place: 2, OrgSlug: "o", Name: "r2", Score: 80, Points: 10},
		Categories: []*rank.CategoryComparison{{Id: "activity", Score: 60, Top10Median: &median, Gap: &gap}, {Id: "documentation", Score: 80}},
	}}
	svc := newRatingService(rating)

	got, err := svc.GetPositionExplanation(context.Background(), "ip", "o", "me")
	if err != nil {
		t.Fatal(err)
	}

	if !got.Ranked || got.Place != 3 || got.Total != 5 || got.Score != 70 || got.Above == nil || got.Above.Name != "r2" || got.Above.Points != 10 || got.Below != nil || got.WeakestCategory != "activity" {
		t.Errorf("explanation = %+v", got)
	}
	if a := got.Categories[0]; a.Top10Median == nil || *a.Top10Median != 65 || *a.Gap != -5 {
		t.Errorf("activity = %+v", a)
	}
	if d := got.Categories[1]; d.Top10Median != nil || d.Gap != nil {
		t.Errorf("documentation = %+v, want nil median and gap", d)
	}

	rating.explanation = &rank.GetPositionExplanationResponse{Ranked: false}
	if got, err := svc.GetPositionExplanation(context.Background(), "ip", "o", "x"); err != nil || got.Ranked || got.Place != 0 {
		t.Errorf("unranked = %+v err %v, want ranked=false and nothing else", got, err)
	}
}

func TestMapGetRankingResponseCarriesDynamics(t *testing.T) {
	resp, err := mapGetRankingResponse(&rank.GetRankingResponse{
		DynamicsDays: 7,
		Entries: []*rank.RankingEntry{
			{Place: 1, SourcecraftUuid: "0192b0a8-0000-7000-8000-000000000001", ScoreChange: int32Ptr(4), PlaceChange: int32Ptr(-2), GradeCount: 3},
			{Place: 2, SourcecraftUuid: "0192b0a8-0000-7000-8000-000000000002"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.DynamicsDays != 7 {
		t.Errorf("dynamics days = %d, want 7", resp.DynamicsDays)
	}
	if e := resp.Entries[0]; e.ScoreChange == nil || *e.ScoreChange != 4 || e.PlaceChange == nil || *e.PlaceChange != -2 {
		t.Errorf("first = %+v", e)
	}
	if resp.Entries[0].GradeCount != 3 || resp.Entries[1].GradeCount != 0 {
		t.Errorf("grade counts = %d, %d, want 3 and 0", resp.Entries[0].GradeCount, resp.Entries[1].GradeCount)
	}
	if e := resp.Entries[1]; e.ScoreChange != nil || e.PlaceChange != nil {
		t.Errorf("second = %+v, want nil changes", e)
	}
}

func TestGetRankingPassesTheFactFiltersToTheRatingService(t *testing.T) {
	yes, no := true, false
	rating := &captureRanking{}
	svc := newRatingService(&fakeRating{})
	svc.ratingClient = rating

	_, err := svc.GetRanking(context.Background(), "ip", domain.GetRankingDTO{Size: "large", HasCI: &yes, HasSecurityPolicy: &no, Topic: "api"})
	if err != nil {
		t.Fatal(err)
	}

	got := rating.req
	if got.GetSize() != "large" || got.GetTopic() != "api" || got.HasCi == nil || !*got.HasCi || got.HasSecurityPolicy == nil || *got.HasSecurityPolicy {
		t.Errorf("request = %+v, want large, api, has_ci=true and has_security_policy=false", got)
	}

	if _, err := svc.GetRanking(context.Background(), "ip", domain.GetRankingDTO{}); err != nil {
		t.Fatal(err)
	}
	if rating.req.HasCi != nil || rating.req.HasSecurityPolicy != nil || rating.req.GetSize() != "" {
		t.Errorf("an empty request must leave the filters unset: %+v", rating.req)
	}
}

type captureRanking struct {
	rank.RatingClient
	req *rank.GetRankingRequest
}

func (c *captureRanking) GetRanking(ctx context.Context, in *rank.GetRankingRequest, opts ...grpc.CallOption) (*rank.GetRankingResponse, error) {
	c.req = in
	return &rank.GetRankingResponse{}, nil
}

func TestGetRegradeStatusMapsScheduleAndAppliesTheLimiter(t *testing.T) {
	next := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	aggregator := &fakeAggregator{regrade: &agg.GetRegradeStatusResponse{
		Enabled: true, IntervalSeconds: 259200, NextRunAt: timestamppb.New(next), Running: true, Total: 20, Done: 4,
	}}
	limiter := &countingLimiter{allow: true}
	svc := newService(aggregator)
	svc.publicAPILimiter = limiter

	status, err := svc.GetRegradeStatus(context.Background(), "203.0.113.9")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !status.Enabled || status.IntervalSeconds != 259200 || status.NextRunAt == nil || !status.NextRunAt.Equal(next) ||
		!status.Running || status.Total != 20 || status.Done != 4 || status.StartedAt != nil || status.LastFinishedAt != nil {
		t.Errorf("status = %+v", status)
	}
	if len(limiter.keys) != 1 {
		t.Errorf("limiter keys = %v, want one public API check", limiter.keys)
	}

	limiter.allow = false
	calls := aggregator.calls
	if _, err := svc.GetRegradeStatus(context.Background(), "ip"); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Errorf("err = %v, want ErrLimitExceeded", err)
	}
	if aggregator.calls != calls {
		t.Error("a rate-limited request must not reach the aggregator")
	}
}

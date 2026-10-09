package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gateway-service/internal/domain"
	"gateway-service/internal/transport/rpc/gen/agg"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type allowAll struct{}

func (allowAll) Allow(ctx context.Context, key string) (bool, error) { return true, nil }

type fakeAggregator struct {
	regrade *agg.GetRegradeStatusResponse
	err     error
	grade   *agg.RepoGrade
	calls   int
	authMD  []string

	myOrgs     *agg.GetMyOrgsResponse
	orgRepos   *agg.GetOrgReposResponse
	orgReposIn *agg.GetOrgReposRequest
}

func (f *fakeAggregator) record(ctx context.Context) {
	f.calls++
	md, _ := metadata.FromOutgoingContext(ctx)
	f.authMD = md.Get("authorization")
}

func (f *fakeAggregator) GetMyRepos(ctx context.Context, in *agg.GetMyReposRequest, opts ...grpc.CallOption) (*agg.GetMyReposResponse, error) {
	f.record(ctx)
	return &agg.GetMyReposResponse{}, f.err
}

func (f *fakeAggregator) GetMyOrgs(ctx context.Context, in *agg.GetMyOrgsRequest, opts ...grpc.CallOption) (*agg.GetMyOrgsResponse, error) {
	f.record(ctx)
	if f.err != nil {
		return nil, f.err
	}
	return f.myOrgs, nil
}

func (f *fakeAggregator) GetOrgRepos(ctx context.Context, in *agg.GetOrgReposRequest, opts ...grpc.CallOption) (*agg.GetOrgReposResponse, error) {
	f.record(ctx)
	f.orgReposIn = in
	if f.err != nil {
		return nil, f.err
	}
	return f.orgRepos, nil
}

func (f *fakeAggregator) GetGrading(ctx context.Context, in *agg.GetGradingRequest, opts ...grpc.CallOption) (*agg.GetGradingResponse, error) {
	f.record(ctx)
	if f.err != nil {
		return nil, f.err
	}
	return &agg.GetGradingResponse{Grade: f.grade}, nil
}

func (f *fakeAggregator) GetAnalytics(ctx context.Context, in *agg.GetAnalyticsRequest, opts ...grpc.CallOption) (*agg.GetAnalyticsResponse, error) {
	f.record(ctx)
	return &agg.GetAnalyticsResponse{}, f.err
}

func (f *fakeAggregator) GetRegradeStatus(ctx context.Context, in *agg.GetRegradeStatusRequest, opts ...grpc.CallOption) (*agg.GetRegradeStatusResponse, error) {
	f.record(ctx)
	if f.regrade == nil {
		return &agg.GetRegradeStatusResponse{}, f.err
	}
	return f.regrade, f.err
}

func (f *fakeAggregator) RequestLLMRecommendations(ctx context.Context, in *agg.RequestLLMRecommendationsRequest, opts ...grpc.CallOption) (*agg.RequestLLMRecommendationsResponse, error) {
	f.record(ctx)
	return nil, f.err
}

func (f *fakeAggregator) RecordAudit(ctx context.Context, in *agg.RecordAuditRequest, opts ...grpc.CallOption) (*agg.RecordAuditResponse, error) {
	return &agg.RecordAuditResponse{}, f.err
}

func (f *fakeAggregator) PreviewGradingBatch(ctx context.Context, in *agg.PreviewGradingBatchRequest, opts ...grpc.CallOption) (*agg.PreviewGradingBatchResponse, error) {
	return &agg.PreviewGradingBatchResponse{}, f.err
}

func (f *fakeAggregator) StartGradingBatch(ctx context.Context, in *agg.StartGradingBatchRequest, opts ...grpc.CallOption) (*agg.StartGradingBatchResponse, error) {
	return &agg.StartGradingBatchResponse{}, f.err
}

func (f *fakeAggregator) ControlGradingBatch(ctx context.Context, in *agg.ControlGradingBatchRequest, opts ...grpc.CallOption) (*agg.ControlGradingBatchResponse, error) {
	return &agg.ControlGradingBatchResponse{}, f.err
}

func (f *fakeAggregator) GetGradingBatch(ctx context.Context, in *agg.GetGradingBatchRequest, opts ...grpc.CallOption) (*agg.GetGradingBatchResponse, error) {
	return &agg.GetGradingBatchResponse{}, f.err
}

func (f *fakeAggregator) ListGradingBatches(ctx context.Context, in *agg.ListGradingBatchesRequest, opts ...grpc.CallOption) (*agg.ListGradingBatchesResponse, error) {
	return &agg.ListGradingBatchesResponse{}, f.err
}

func (f *fakeAggregator) GetCatalogStatus(ctx context.Context, in *agg.GetCatalogStatusRequest, opts ...grpc.CallOption) (*agg.GetCatalogStatusResponse, error) {
	return &agg.GetCatalogStatusResponse{}, f.err
}

func (f *fakeAggregator) RefreshCatalog(ctx context.Context, in *agg.RefreshCatalogRequest, opts ...grpc.CallOption) (*agg.RefreshCatalogResponse, error) {
	return &agg.RefreshCatalogResponse{Refresh: &agg.CatalogRefresh{Id: 1, Status: "running", ActorHash: in.GetActorHash()}}, f.err
}

func (f *fakeAggregator) CancelCatalogRefresh(ctx context.Context, in *agg.CancelCatalogRefreshRequest, opts ...grpc.CallOption) (*agg.CancelCatalogRefreshResponse, error) {
	return &agg.CancelCatalogRefreshResponse{Refresh: &agg.CatalogRefresh{Id: 1, Status: "cancelled"}}, f.err
}

func (f *fakeAggregator) ListAudit(ctx context.Context, in *agg.ListAuditRequest, opts ...grpc.CallOption) (*agg.ListAuditResponse, error) {
	return &agg.ListAuditResponse{}, f.err
}

func newService(aggregator *fakeAggregator) *GatewayService {
	return NewGatewayService(aggregator, nil, nil, allowAll{}, nil, nil, time.Second, time.Second, time.Second)
}

func TestGetGradingForwardsPATAsBearer(t *testing.T) {
	aggregator := &fakeAggregator{grade: &agg.RepoGrade{
		SourcecraftUuid: uuid.NewString(),
		GradedAt:        timestamppb.Now(),
	}}

	_, err := newService(aggregator).GetGrading(context.Background(), "ip", "user-pat", domain.GetGradingDTO{OrgSlug: "o", RepoSlug: "r"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(aggregator.authMD) != 1 || aggregator.authMD[0] != "Bearer user-pat" {
		t.Errorf("authorization = %v, want the caller's PAT", aggregator.authMD)
	}
}

func TestGetGradingWithoutPATSendsNoToken(t *testing.T) {
	aggregator := &fakeAggregator{grade: &agg.RepoGrade{SourcecraftUuid: uuid.NewString(), GradedAt: timestamppb.Now()}}

	_, _ = newService(aggregator).GetGrading(context.Background(), "ip", "", domain.GetGradingDTO{OrgSlug: "o", RepoSlug: "r"})
	if len(aggregator.authMD) != 0 {
		t.Errorf("authorization = %v, want none for an anonymous caller", aggregator.authMD)
	}
}

func TestGetGradingRequiresSlugs(t *testing.T) {
	aggregator := &fakeAggregator{}

	_, err := newService(aggregator).GetGrading(context.Background(), "ip", "", domain.GetGradingDTO{OrgSlug: "o"})
	if !errors.Is(err, domain.ErrInvalidRequest) || aggregator.calls != 0 {
		t.Errorf("err = %v calls = %d, want ErrInvalidRequest without an RPC", err, aggregator.calls)
	}
}

func TestGetMyReposRequiresPAT(t *testing.T) {
	aggregator := &fakeAggregator{}

	_, err := newService(aggregator).GetMyRepos(context.Background(), "ip", "")
	if !errors.Is(err, domain.ErrUnauthenticated) || aggregator.calls != 0 {
		t.Errorf("err = %v calls = %d, want ErrUnauthenticated without an RPC", err, aggregator.calls)
	}
}

func TestUpstreamCodesMapToDomainErrors(t *testing.T) {
	cases := map[codes.Code]error{
		codes.FailedPrecondition: domain.ErrPending,
		codes.NotFound:           domain.ErrNotFound,
		codes.PermissionDenied:   domain.ErrForbidden,
		codes.Unauthenticated:    domain.ErrUnauthenticated,
		codes.InvalidArgument:    domain.ErrInvalidRequest,
		codes.Aborted:            domain.ErrConflict,
		codes.Unavailable:        domain.ErrUnavailable,
		codes.DeadlineExceeded:   domain.ErrTimeout,
		codes.Internal:           domain.ErrRequestFailed,
	}
	for code, want := range cases {
		t.Run(code.String(), func(t *testing.T) {
			aggregator := &fakeAggregator{err: status.Error(code, "x")}

			_, err := newService(aggregator).GetGrading(context.Background(), "ip", "", domain.GetGradingDTO{OrgSlug: "o", RepoSlug: "r"})
			if !errors.Is(err, want) {
				t.Errorf("err = %v, want %v", err, want)
			}
		})
	}
}

func TestGradingInterruptedIsRecognised(t *testing.T) {
	aggregator := &fakeAggregator{err: status.Error(codes.Aborted, gradingInterruptedMessage)}

	_, err := newService(aggregator).GetGrading(context.Background(), "ip", "", domain.GetGradingDTO{OrgSlug: "o", RepoSlug: "r"})
	if !errors.Is(err, domain.ErrGradingInterrupted) {
		t.Fatalf("err = %v, want ErrGradingInterrupted", err)
	}
}

func TestGradingLimitCarriesRetryAfter(t *testing.T) {
	aggregator := &fakeAggregator{err: status.Error(codes.ResourceExhausted, gradingRateLimitedPrefix+"360")}

	_, err := newService(aggregator).GetGrading(context.Background(), "ip", "", domain.GetGradingDTO{OrgSlug: "o", RepoSlug: "r"})

	var limit domain.GradingLimitError
	if !errors.As(err, &limit) || limit.RetryAfter != 6*time.Minute || !errors.Is(err, domain.ErrLimitExceeded) {
		t.Fatalf("err = %v, want GradingLimitError with 6m wait", err)
	}
}

func TestRecommendationsPendingPassesThrough(t *testing.T) {
	aggregator := &fakeAggregator{err: status.Error(codes.FailedPrecondition, "grading is pending")}

	_, err := newService(aggregator).RequestLLMRecommendations(context.Background(), "ip", "", domain.RequestLLMRecommendationsDTO{OrgSlug: "o", RepoSlug: "r"})
	if !errors.Is(err, domain.ErrPending) {
		t.Errorf("err = %v, want ErrPending", err)
	}
}

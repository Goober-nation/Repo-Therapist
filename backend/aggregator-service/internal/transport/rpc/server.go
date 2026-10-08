package grpc

import (
	"aggregator-service/internal/domain"
	"aggregator-service/internal/transport/rpc/gen/server"
	"context"
	"errors"
	"math"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type AggregatorService interface {
	GetMyRepos(
		ctx context.Context,
	) (domain.MyRepositoriesResponse, error)

	GetMyOrgs(ctx context.Context) (domain.MyOrganizations, error)

	GetOrgRepos(
		ctx context.Context,
		orgSlug string,
		pageToken string,
	) (domain.MyRepositoriesResponse, error)

	GetGrading(
		ctx context.Context,
		orgSlug string,
		repoSlug string,
	) (*domain.RepoGrade, error)

	RequestLLMRecommendations(
		ctx context.Context,
		orgSlug string,
		repoSlug string,
	) (domain.RepoLLMResponse, error)

	GetAnalytics(
		ctx context.Context,
		orgSlug string,
		repoSlug string,
	) (domain.RepoAnalytics, error)

	GetRegradeStatus(ctx context.Context) domain.RegradeStatus

	RecordAudit(ctx context.Context, event domain.AuditEvent) error
	ListAudit(ctx context.Context, filter domain.AuditFilter) (domain.AuditPage, error)

	PreviewBatch(ctx context.Context, filter domain.BatchFilter) (domain.BatchPreview, error)
	StartBatch(ctx context.Context, actorHash string, filter domain.BatchFilter) (domain.Batch, error)
	ControlBatch(ctx context.Context, actorHash string, id int64, action string) (domain.Batch, error)
	BatchStatus(ctx context.Context, id int64) (domain.Batch, []domain.BatchItem, error)
	ListBatches(ctx context.Context, limit int) ([]domain.Batch, error)
}

type Server struct {
	server.UnimplementedAggregatorServer

	service AggregatorService
}

func NewServer(service AggregatorService) *Server {
	return &Server{
		service: service,
	}
}

func (s *Server) GetMyRepos(
	ctx context.Context,
	req *server.GetMyReposRequest,
) (*server.GetMyReposResponse, error) {
	response, err := s.service.GetMyRepos(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	repositories := make([]*server.Repository, 0, len(response.Repositories))

	for _, repo := range response.Repositories {
		pbRepo, err := repoToProto(repo)
		if err != nil {
			return nil, err
		}

		repositories = append(repositories, pbRepo)
	}

	return &server.GetMyReposResponse{
		Repositories:  repositories,
		NextPageToken: response.NextPageToken,
	}, nil
}

func (s *Server) GetMyOrgs(
	ctx context.Context,
	req *server.GetMyOrgsRequest,
) (*server.GetMyOrgsResponse, error) {
	response, err := s.service.GetMyOrgs(ctx)
	if err != nil {
		return nil, mapError(err)
	}

	orgs := make([]*server.UserOrganization, 0, len(response.Organizations))
	for _, org := range response.Organizations {
		orgs = append(orgs, &server.UserOrganization{
			Slug:        org.Slug,
			DisplayName: org.DisplayName,
			Personal:    org.Personal,
		})
	}

	return &server.GetMyOrgsResponse{
		Organizations:  orgs,
		DefaultOrgSlug: response.DefaultOrgSlug,
	}, nil
}

func (s *Server) GetOrgRepos(
	ctx context.Context,
	req *server.GetOrgReposRequest,
) (*server.GetOrgReposResponse, error) {
	if req.GetOrgSlug() == "" {
		return nil, status.Error(codes.InvalidArgument, "org_slug is required")
	}

	response, err := s.service.GetOrgRepos(ctx, req.GetOrgSlug(), req.GetPageToken())
	if err != nil {
		return nil, mapError(err)
	}

	repositories := make([]*server.Repository, 0, len(response.Repositories))
	for _, repo := range response.Repositories {
		pbRepo, err := repoToProto(repo)
		if err != nil {
			return nil, err
		}
		repositories = append(repositories, pbRepo)
	}

	return &server.GetOrgReposResponse{
		Repositories:  repositories,
		NextPageToken: response.NextPageToken,
	}, nil
}

func (s *Server) GetGrading(
	ctx context.Context,
	req *server.GetGradingRequest,
) (*server.GetGradingResponse, error) {
	if req.GetOrgSlug() == "" || req.GetRepoSlug() == "" {
		return nil, status.Error(
			codes.InvalidArgument,
			"org_slug and repo_slug are required",
		)
	}

	grade, err := s.service.GetGrading(
		ctx,
		req.GetOrgSlug(),
		req.GetRepoSlug(),
	)
	if err != nil {
		return nil, mapError(err)
	}

	return &server.GetGradingResponse{
		Grade: gradeToProto(*grade),
	}, nil
}

func (s *Server) GetAnalytics(
	ctx context.Context,
	req *server.GetAnalyticsRequest,
) (*server.GetAnalyticsResponse, error) {
	if req.GetOrgSlug() == "" || req.GetRepoSlug() == "" {
		return nil, status.Error(
			codes.InvalidArgument,
			"org_slug and repo_slug are required",
		)
	}

	analytics, err := s.service.GetAnalytics(ctx, req.GetOrgSlug(), req.GetRepoSlug())
	if err != nil {
		return nil, mapError(err)
	}

	response := &server.GetAnalyticsResponse{Analytics: analytics.Analytics}
	if analytics.GradedAt != nil {
		response.GradedAt = timestamppb.New(*analytics.GradedAt)
	}

	return response, nil
}

// RecordAudit stores one audit event reported by the Gateway.
func (s *Server) RecordAudit(
	ctx context.Context,
	req *server.RecordAuditRequest,
) (*server.RecordAuditResponse, error) {
	err := s.service.RecordAudit(ctx, domain.AuditEvent{
		ActorKind:  req.GetActorKind(),
		ActorHash:  req.GetActorHash(),
		Action:     req.GetAction(),
		TargetKind: req.GetTargetKind(),
		Target:     req.GetTarget(),
		Outcome:    req.GetOutcome(),
		Details:    req.GetDetails(),
	})
	if err != nil {
		return nil, mapError(err)
	}

	return &server.RecordAuditResponse{}, nil
}

// ListAudit returns one page of the audit log, newest first.
func (s *Server) ListAudit(
	ctx context.Context,
	req *server.ListAuditRequest,
) (*server.ListAuditResponse, error) {
	page, err := s.service.ListAudit(ctx, domain.AuditFilter{
		Limit:    int(req.GetLimit()),
		BeforeID: req.GetBeforeId(),
		Action:   req.GetAction(),
		Outcome:  req.GetOutcome(),
		Scope:    req.GetScope(),
	})
	if err != nil {
		return nil, mapError(err)
	}

	response := &server.ListAuditResponse{
		Entries:      make([]*server.AuditEntry, 0, len(page.Entries)),
		NextBeforeId: page.NextBeforeID,
	}

	for _, entry := range page.Entries {
		response.Entries = append(response.Entries, &server.AuditEntry{
			Id:         entry.ID,
			OccurredAt: timestamppb.New(entry.OccurredAt),
			ActorKind:  entry.ActorKind,
			ActorHash:  entry.ActorHash,
			Action:     entry.Action,
			TargetKind: entry.TargetKind,
			Target:     entry.Target,
			Outcome:    entry.Outcome,
			Details:    entry.Details,
		})
	}

	return response, nil
}

func (s *Server) GetRegradeStatus(
	ctx context.Context,
	_ *server.GetRegradeStatusRequest,
) (*server.GetRegradeStatusResponse, error) {
	status := s.service.GetRegradeStatus(ctx)

	response := &server.GetRegradeStatusResponse{
		Enabled:         status.Enabled,
		IntervalSeconds: int64(status.Interval.Seconds()),
		Running:         status.Running,
		Total:           int32(status.Total),
		Done:            int32(status.Done),
	}

	if status.NextRunAt != nil {
		response.NextRunAt = timestamppb.New(*status.NextRunAt)
	}

	if status.StartedAt != nil {
		response.StartedAt = timestamppb.New(*status.StartedAt)
	}

	if status.LastFinishedAt != nil {
		response.LastFinishedAt = timestamppb.New(*status.LastFinishedAt)
	}

	return response, nil
}

func (s *Server) RequestLLMRecommendations(
	ctx context.Context,
	req *server.RequestLLMRecommendationsRequest,
) (*server.RequestLLMRecommendationsResponse, error) {
	if req.GetOrgSlug() == "" || req.GetRepoSlug() == "" {
		return nil, status.Error(
			codes.InvalidArgument,
			"org_slug and repo_slug are required",
		)
	}

	response, err := s.service.RequestLLMRecommendations(
		ctx,
		req.GetOrgSlug(),
		req.GetRepoSlug(),
	)
	if err != nil {
		return nil, mapError(err)
	}

	result := &server.RequestLLMRecommendationsResponse{
		RawResponse: response.RawResponse,
		ModelUsed:   response.ModelUsed,
	}

	if response.EvaluatedAt != nil {
		result.EvaluatedAt = timestamppb.New(*response.EvaluatedAt)
	}

	return result, nil
}

func repoToProto(repo domain.Repo) (*server.Repository, error) {
	//var reactions []domain.ReactionCount

	//if len(repo.RatingReactions) > 0 {
	//	if err := json.Unmarshal(repo.RatingReactions, &reactions); err != nil {
	//		return nil, status.Errorf(
	//			codes.Internal,
	//			"invalid rating_reactions for repository %s: %v",
	//			repo.SourcecraftUUID,
	//			err,
	//		)
	//	}
	//}

	result := &server.Repository{
		Id: repo.SourcecraftUUID.String(),
		Organization: &server.Organization{
			Slug: repo.OrgSlug,
		},
		Slug:       repo.RepoSlug,
		Visibility: repo.Visibility,
		Rating:     repo.RatingValue,
	}

	if repo.LastActivityAt != nil {
		result.LastActivityAt = repo.LastActivityAt.Format(time.RFC3339Nano)
	}

	//if repo.RatingValue != nil {
	//	result.Rating.Value = *repo.RatingValue
	//}
	//
	//if repo.RatingPercentile != nil {
	//	result.Rating.Percentile = *repo.RatingPercentile
	//}

	//for _, reaction := range reactions {
	//	result.Rating.ReactionCounts = append(
	//		result.Rating.ReactionCounts,
	//		&client.ReactionCount{
	//			Type:  reaction.Type,
	//			Count: reaction.Count,
	//		},
	//	)
	//}

	return result, nil
}

func gradeToProto(grade domain.RepoGrade) *server.RepoGrade {
	result := &server.RepoGrade{
		SourcecraftUuid: grade.SourcecraftUUID.String(),
		CategoryScores:  grade.CategoryScores,
		Advice:          grade.Advice,

		MissingData: grade.MissingData,
	}

	if grade.Score != nil {
		value := int32(*grade.Score)
		result.Score = &value
	}

	if grade.GradedAt != nil {
		result.GradedAt = timestamppb.New(*grade.GradedAt)
	}

	return result
}

// GradingRateLimitedPrefix starts the ResourceExhausted message for the service-wide grading
// limit; the number after it is the wait in seconds.
const GradingRateLimitedPrefix = "grading rate limited, retry after seconds: "

// GradingInterruptedMessage is the Aborted message for a grading that failed or was cut short.
const GradingInterruptedMessage = "grading interrupted"

// Reasons of a refused batch request; the Gateway reads them from the status message.
const (
	BatchReasonNothing  = "nothing_selected"
	BatchReasonTooMany  = "too_many"
	BatchReasonInvalid  = "invalid_filter"
	BatchReasonActive   = "batch_active"
	BatchReasonState    = "bad_state"
	BatchReasonNotFound = "batch_not_found"
)

func mapError(err error) error {
	var limited domain.RateLimitedError

	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrGradingInterrupted):
		// The gateway recognises this message and reports the failure to the user.
		return status.Error(
			codes.Aborted,
			GradingInterruptedMessage,
		)
	case errors.As(err, &limited):
		// The gateway reads the wait from this message to set Retry-After.
		return status.Errorf(
			codes.ResourceExhausted,
			"%s%d",
			GradingRateLimitedPrefix,
			int(math.Ceil(limited.RetryAfter.Seconds())),
		)
	case errors.Is(err, domain.ErrTimeout):
		return status.Error(
			codes.DeadlineExceeded,
			"wrapper service timeout",
		)
	case errors.Is(err, domain.ErrNoAccess):
		return status.Error(
			codes.PermissionDenied,
			"no access rights",
		)

	case errors.Is(err, domain.ErrUnauthenticated):
		return status.Error(
			codes.Unauthenticated,
			"missing or invalid user token",
		)

	case errors.Is(err, domain.ErrRepositoryNotFound):
		return status.Error(
			codes.NotFound,
			"repository not found",
		)

	case errors.Is(err, domain.ErrGradeNotFound),
		errors.Is(err, domain.ErrNoGrade):
		return status.Error(
			codes.NotFound,
			"grade not found",
		)

	case errors.Is(err, domain.GradingPending):
		return status.Error(
			codes.FailedPrecondition,
			"grading is pending",
		)

	case errors.Is(err, domain.ErrGradeOutdated):
		return status.Error(
			codes.FailedPrecondition,
			"grade is outdated",
		)

	case errors.Is(err, domain.ErrInvalidAudit):
		return status.Error(
			codes.InvalidArgument,
			err.Error(),
		)

	// The batch errors carry a fixed reason word, not text: the Gateway passes it to the admin UI.
	case errors.Is(err, domain.ErrBatchNothing):
		return status.Error(codes.InvalidArgument, BatchReasonNothing)

	case errors.Is(err, domain.ErrBatchTooMany):
		return status.Error(codes.InvalidArgument, BatchReasonTooMany)

	case errors.Is(err, domain.ErrInvalidBatch):
		return status.Error(codes.InvalidArgument, BatchReasonInvalid)

	case errors.Is(err, domain.ErrBatchActive):
		return status.Error(codes.AlreadyExists, BatchReasonActive)

	case errors.Is(err, domain.ErrBatchState):
		return status.Error(codes.Aborted, BatchReasonState)

	case errors.Is(err, domain.ErrBatchNotFound):
		return status.Error(codes.NotFound, BatchReasonNotFound)

	case errors.Is(err, domain.ErrRepoLocked):
		return status.Error(
			codes.Aborted,
			"repository is locked",
		)

	case errors.Is(err, domain.ErrLLMUnavailable):
		return status.Error(
			codes.Unavailable,
			"llm service unavailable",
		)

	case errors.Is(err, domain.ErrSourceCraftUnavailable),
		errors.Is(err, domain.ErrExternalAPI):
		return status.Error(
			codes.Unavailable,
			"sourcecraft service unavailable",
		)

	case errors.Is(err, domain.ErrGradingFailed):
		return status.Error(
			codes.Internal,
			"grading failed",
		)

	case errors.Is(err, domain.ErrRecommendationsFailed):
		return status.Error(
			codes.Internal,
			"recommendations failed",
		)

	default:
		return status.Error(
			codes.Internal,
			"internal server error",
		)
	}
}

func batchFilterFromProto(f *server.BatchFilter) domain.BatchFilter {
	filter := domain.BatchFilter{
		Languages: f.GetLanguages(),
		Repos:     f.GetRepos(),
		All:       f.GetAll(),
		Force:     f.GetForce(),
	}

	if f.TopRated != nil {
		v := int(f.GetTopRated())
		filter.TopRated = &v
	}

	if f.MinScore != nil {
		v := int(f.GetMinScore())
		filter.MinScore = &v
	}

	if f.MaxScore != nil {
		v := int(f.GetMaxScore())
		filter.MaxScore = &v
	}

	if f.OlderThanDays != nil {
		v := int(f.GetOlderThanDays())
		filter.OlderThanDays = &v
	}

	return filter
}

func batchFilterToProto(f domain.BatchFilter) *server.BatchFilter {
	out := &server.BatchFilter{Languages: f.Languages, Repos: f.Repos, All: f.All, Force: f.Force}

	if f.TopRated != nil {
		v := int32(*f.TopRated)
		out.TopRated = &v
	}

	if f.MinScore != nil {
		v := int32(*f.MinScore)
		out.MinScore = &v
	}

	if f.MaxScore != nil {
		v := int32(*f.MaxScore)
		out.MaxScore = &v
	}

	if f.OlderThanDays != nil {
		v := int32(*f.OlderThanDays)
		out.OlderThanDays = &v
	}

	return out
}

func batchToProto(b domain.Batch) *server.RegradeBatch {
	out := &server.RegradeBatch{
		Id:        b.ID,
		CreatedAt: timestamppb.New(b.CreatedAt),
		Status:    b.Status,
		ActorHash: b.ActorHash,
		Filter:    batchFilterToProto(b.Filter),
		Total:     int32(b.Total),
		Pending:   int32(b.Pending),
		Running:   int32(b.Running),
		Ok:        int32(b.OK),
		Failed:    int32(b.Failed),
		Skipped:   int32(b.Skipped),
	}

	if b.FinishedAt != nil {
		out.FinishedAt = timestamppb.New(*b.FinishedAt)
	}

	return out
}

// PreviewRegradeBatch is the dry run of a batch regrade.
func (s *Server) PreviewRegradeBatch(
	ctx context.Context,
	req *server.PreviewRegradeBatchRequest,
) (*server.PreviewRegradeBatchResponse, error) {
	preview, err := s.service.PreviewBatch(ctx, batchFilterFromProto(req.GetFilter()))
	if err != nil {
		return nil, mapError(err)
	}

	return &server.PreviewRegradeBatchResponse{
		Count:           int32(preview.Count),
		Capped:          preview.Capped,
		Sample:          preview.Sample,
		Unmatched:       preview.Unmatched,
		EstimateSeconds: int32(preview.EstimateSeconds),
		Inactive:        int32(preview.Inactive),
	}, nil
}

// StartRegradeBatch creates a batch and starts it.
func (s *Server) StartRegradeBatch(
	ctx context.Context,
	req *server.StartRegradeBatchRequest,
) (*server.StartRegradeBatchResponse, error) {
	batch, err := s.service.StartBatch(ctx, req.GetActorHash(), batchFilterFromProto(req.GetFilter()))
	if err != nil {
		return nil, mapError(err)
	}

	return &server.StartRegradeBatchResponse{Batch: batchToProto(batch)}, nil
}

// ControlRegradeBatch pauses, resumes or cancels a batch.
func (s *Server) ControlRegradeBatch(
	ctx context.Context,
	req *server.ControlRegradeBatchRequest,
) (*server.ControlRegradeBatchResponse, error) {
	batch, err := s.service.ControlBatch(ctx, req.GetActorHash(), req.GetBatchId(), req.GetAction())
	if err != nil {
		return nil, mapError(err)
	}

	return &server.ControlRegradeBatchResponse{Batch: batchToProto(batch)}, nil
}

// GetRegradeBatch returns a batch and its failed repos.
func (s *Server) GetRegradeBatch(
	ctx context.Context,
	req *server.GetRegradeBatchRequest,
) (*server.GetRegradeBatchResponse, error) {
	batch, failed, err := s.service.BatchStatus(ctx, req.GetBatchId())
	if err != nil {
		return nil, mapError(err)
	}

	response := &server.GetRegradeBatchResponse{
		Batch:    batchToProto(batch),
		Failures: make([]*server.RegradeBatchFailure, 0, len(failed)),
	}

	for _, item := range failed {
		response.Failures = append(response.Failures, &server.RegradeBatchFailure{
			OrgSlug: item.OrgSlug, RepoSlug: item.RepoSlug, Error: item.Error,
		})
	}

	return response, nil
}

// ListRegradeBatches returns the most recent batches.
func (s *Server) ListRegradeBatches(
	ctx context.Context,
	req *server.ListRegradeBatchesRequest,
) (*server.ListRegradeBatchesResponse, error) {
	batches, err := s.service.ListBatches(ctx, int(req.GetLimit()))
	if err != nil {
		return nil, mapError(err)
	}

	response := &server.ListRegradeBatchesResponse{Batches: make([]*server.RegradeBatch, 0, len(batches))}
	for _, batch := range batches {
		response.Batches = append(response.Batches, batchToProto(batch))
	}

	return response, nil
}

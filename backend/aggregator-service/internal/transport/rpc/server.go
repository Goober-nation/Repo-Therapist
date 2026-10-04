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

package service

import (
	"aggregator-service/internal/domain"
	"aggregator-service/internal/transport/rpc/gen/client"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type AggregatorService struct {
	ReposRepo        domain.RepoRepository
	GradesRepo       domain.GradesRepository
	LLMRepo          domain.LLMRepository
	ticker           *time.Ticker
	clientRPC        client.CodeHealthClient
	gradingPublisher domain.GradingPublisher
	llmPublisher     domain.LLMRecommendationsPublisher
	RPCTimeout       time.Duration
}

func NewAggregatorService(reposRepo domain.RepoRepository, gradesRepo domain.GradesRepository, llmRepo domain.LLMRepository,
	clientRPC client.CodeHealthClient, gradingPublisher domain.GradingPublisher, llmPublisher domain.LLMRecommendationsPublisher,
	rpcTimeout time.Duration) *AggregatorService {
	return &AggregatorService{
		ReposRepo:        reposRepo,
		GradesRepo:       gradesRepo,
		LLMRepo:          llmRepo,
		clientRPC:        clientRPC,
		gradingPublisher: gradingPublisher,
		llmPublisher:     llmPublisher,
		RPCTimeout:       rpcTimeout,
	}
}

func (as *AggregatorService) SaveLLMRecommendations(ctx context.Context, event domain.LLMRecommendationsReceivedEvent) error {
	if event.Error != "" {
		return fmt.Errorf(
			"%w: %v",
			domain.ErrLLMUnavailable,
			event.Error,
		)
	}
	switch event.Visibility {
	case "public":
		if err := as.LLMRepo.UpsertPublicResponse(ctx, event); err != nil {
			return fmt.Errorf(
				"%w: save public llm recommendations: %v",
				domain.ErrLLMUnavailable,
				err,
			)
		}

	case "private", "internal":
		if err := as.LLMRepo.UpsertPrivateResponse(ctx, event); err != nil {
			return fmt.Errorf(
				"%w: save private llm recommendations: %v",
				domain.ErrLLMUnavailable,
				err,
			)
		}

	default:
		return fmt.Errorf(
			"%w: unknown repository visibility %q",
			domain.ErrLLMUnavailable,
			event.Visibility,
		)
	}

	return nil
}

func (as *AggregatorService) RequestLLMRecommendations(
	ctx context.Context,
	orgSlug string,
	repoSlug string,
) (domain.RepoLLMResponse, error) {
	cont, cancel := context.WithTimeout(ctx, as.RPCTimeout)
	defer cancel()

	response, err := as.clientRPC.GetRepositoryBySlugs(
		cont,
		&client.GetRepositoryBySlugsRequest{
			OrgSlug:  orgSlug,
			RepoSlug: repoSlug,
		},
	)
	if err != nil {
		if status.Code(err) == codes.DeadlineExceeded {
			return domain.RepoLLMResponse{}, fmt.Errorf(
				"%w: wrapper RPC timeout: %v",
				domain.ErrTimeout,
				err,
			)
		}
		if status.Code(err) == codes.PermissionDenied {
			return domain.RepoLLMResponse{}, domain.ErrNoAccess
		}

		return domain.RepoLLMResponse{}, fmt.Errorf(
			"%w: get repository for llm: %v",
			domain.ErrLLMUnavailable,
			err,
		)
	}

	repoID, err := uuid.Parse(response.GetId())
	if err != nil {
		return domain.RepoLLMResponse{}, fmt.Errorf(
			"%w: invalid repository UUID: %v",
			domain.ErrRecommendationsFailed,
			err,
		)
	}

	var lastActivityAt *time.Time

	if value := response.GetLastActivityAt(); value != "" {
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return domain.RepoLLMResponse{}, fmt.Errorf(
				"%w: invalid last_activity_at: %v",
				domain.ErrRecommendationsFailed,
				err,
			)
		}
		lastActivityAt = &parsed
	}
	sourceRepo := domain.Repo{
		SourcecraftUUID: repoID,
		OrgSlug:         response.GetOrganization().GetSlug(),
		RepoSlug:        response.GetSlug(),
		Visibility:      response.GetVisibility(),
		LastActivityAt:  lastActivityAt,
	}
	repo, err := as.ReposRepo.GetRepositoryByID(ctx, repoID)
	if err != nil {
		if !errors.Is(err, domain.ErrRepositoryNotFound) {
			return domain.RepoLLMResponse{}, fmt.Errorf(
				"%w: get repository: %v",
				domain.ErrRecommendationsFailed,
				err,
			)
		}
		repo = sourceRepo
		if _, err := as.ReposRepo.CreateRepository(ctx, repo); err != nil {
			return domain.RepoLLMResponse{}, fmt.Errorf(
				"%w: create repository: %v",
				domain.ErrRecommendationsFailed,
				err,
			)
		}
	} else {
		repo.OrgSlug = sourceRepo.OrgSlug
		repo.RepoSlug = sourceRepo.RepoSlug
		repo.Visibility = sourceRepo.Visibility
		repo.LastActivityAt = sourceRepo.LastActivityAt
		if _, err := as.ReposRepo.UpdateRepository(ctx, repo); err != nil {
			return domain.RepoLLMResponse{}, fmt.Errorf(
				"%w: update repository: %v",
				domain.ErrRecommendationsFailed,
				err,
			)
		}
	}
	var grade domain.RepoGrade

	if as.hasPrivateAccess(ctx, sourceRepo) {
		grade, err = as.GradesRepo.GetPrivateGradeByRepoID(ctx, repoID)
		if err != nil {
			if errors.Is(err, domain.ErrGradeNotFound) {
				return domain.RepoLLMResponse{}, domain.ErrNoGrade
			}

			return domain.RepoLLMResponse{}, fmt.Errorf(
				"%w: get private grade: %v",
				domain.ErrLLMUnavailable,
				err,
			)
		}
	} else {
		grade, err = as.GradesRepo.GetPublicGradeByRepoID(ctx, repoID)
		if err != nil {
			if errors.Is(err, domain.ErrGradeNotFound) {
				return domain.RepoLLMResponse{}, domain.ErrNoGrade
			}

			return domain.RepoLLMResponse{}, fmt.Errorf(
				"%w: get public grade: %v",
				domain.ErrLLMUnavailable,
				err,
			)
		}
	}

	if grade.GradedAt == nil {
		return domain.RepoLLMResponse{}, domain.ErrNoGrade
	}

	if repo.LastActivityAt != nil && repo.LastActivityAt.After(*grade.GradedAt) {
		return domain.RepoLLMResponse{}, domain.ErrGradeOutdated
	}

	//switch repo.Visibility {
	//case "public":
	//	resp, err := as.LLMRepo.GetPublicResponseByRepoID(
	//		ctx,
	//		repoID,
	//	)
	//	if err == nil && llmResponseIsCurrent(
	//		resp.EvaluatedAt,
	//		repo.LastActivityAt,
	//	) {
	//		return domain.RepoLLMResponse{
	//			SourcecraftUUID: resp.SourcecraftUUID,
	//			RawResponse:     resp.RawResponse,
	//			ModelUsed:       resp.ModelUsed,
	//			EvaluatedAt:     resp.EvaluatedAt,
	//		}, nil
	//	}
	//
	//	if err != nil && !errors.Is(err, domain.ErrLLMResponseNotFound) {
	//		return domain.RepoLLMResponse{}, fmt.Errorf(
	//			"%w: get public llm response: %v",
	//			domain.ErrLLMUnavailable,
	//			err,
	//		)
	//	}
	//
	//case "private", "internal":
	//	resp, err := as.LLMRepo.GetPrivateResponseByRepoID(
	//		ctx,
	//		repoID,
	//	)
	//	if err == nil && llmResponseIsCurrent(
	//		resp.EvaluatedAt,
	//		repo.LastActivityAt,
	//	) {
	//		return domain.RepoLLMResponse{
	//			SourcecraftUUID: resp.SourcecraftUUID,
	//			RawResponse:     resp.RawResponse,
	//			ModelUsed:       resp.ModelUsed,
	//			EvaluatedAt:     resp.EvaluatedAt,
	//		}, nil
	//	}
	//
	//	if err != nil && !errors.Is(err, domain.ErrLLMResponseNotFound) {
	//		return domain.RepoLLMResponse{}, fmt.Errorf(
	//			"%w: get private llm response: %v",
	//			domain.ErrLLMUnavailable,
	//			err,
	//		)
	//	}
	//
	//default:
	//	return domain.RepoLLMResponse{}, fmt.Errorf(
	//		"%w: unknown repository visibility %q",
	//		domain.ErrRecommendationsFailed,
	//		repo.Visibility,
	//	)
	//}

	event := domain.LLMRecommendationsRequestedEvent{
		SourcecraftUUID: repoID,
		OrgSlug:         repo.OrgSlug,
		RepoSlug:        repo.RepoSlug,
		Grade:           grade,
		RequestedAt:     time.Now(),
		Visibility:      repo.Visibility,
	}

	if err := as.llmPublisher.Publish(ctx, event); err != nil {
		return domain.RepoLLMResponse{}, fmt.Errorf(
			"%w: publish llm recommendations request: %v",
			domain.ErrLLMUnavailable,
			err,
		)
	}

	return domain.RepoLLMResponse{}, domain.GradingPending
}

func llmResponseIsCurrent(evaluatedAt *time.Time, lastActivityAt *time.Time) bool {
	if evaluatedAt == nil || lastActivityAt == nil {
		return false
	}

	return !evaluatedAt.Before(*lastActivityAt)
}

func (as *AggregatorService) GetGrading(
	ctx context.Context,
	orgSlug string,
	repoSlug string,
) (*domain.RepoGrade, error) {
	cont, cancel := context.WithTimeout(ctx, as.RPCTimeout)
	defer cancel()
	response, err := as.clientRPC.GetRepositoryBySlugs(
		cont,
		&client.GetRepositoryBySlugsRequest{
			OrgSlug:  orgSlug,
			RepoSlug: repoSlug,
		},
	)
	if err != nil {
		if status.Code(err) == codes.DeadlineExceeded {
			return nil, fmt.Errorf(
				"%w: wrapper RPC timeout: %v",
				domain.ErrTimeout,
				err,
			)
		}
		if status.Code(err) == codes.PermissionDenied {
			return nil, domain.ErrNoAccess
		}

		return nil, fmt.Errorf(
			"%w: wrapper returned %s: %v",
			domain.ErrGradingFailed,
			status.Code(err),
			err,
		)
	}

	repoID, err := uuid.Parse(response.GetId())
	if err != nil {
		return nil, fmt.Errorf(
			"%w: invalid repository UUID: %v",
			domain.ErrGradingFailed,
			err,
		)
	}

	var lastActivityAt *time.Time

	if value := response.GetLastActivityAt(); value != "" {
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: invalid last_activity_at: %v",
				domain.ErrGradingFailed,
				err,
			)
		}

		lastActivityAt = &parsed
	}

	sourceRepo := domain.Repo{
		SourcecraftUUID: repoID,
		OrgSlug:         response.GetOrganization().GetSlug(),
		RepoSlug:        response.GetSlug(),
		Visibility:      response.GetVisibility(),
		LastActivityAt:  lastActivityAt,
		Language: domain.PrimaryLanguage{
			Name:  response.GetLanguage().Name,
			Color: response.GetLanguage().Color,
		},
		RatingValue: response.GetRating().Value,
		WebURL:      response.GetWebUrl(),
	}

	repo, err := as.ReposRepo.GetRepositoryByID(ctx, repoID)
	if err != nil {
		if !errors.Is(err, domain.ErrRepositoryNotFound) {
			return nil, fmt.Errorf(
				"%w: get repository: %v",
				domain.ErrGradingFailed,
				err,
			)
		}

		repo = sourceRepo

		if _, err := as.ReposRepo.CreateRepository(ctx, repo); err != nil {
			return nil, fmt.Errorf(
				"%w: create repository: %v",
				domain.ErrGradingFailed,
				err,
			)
		}
	} else {

		repo.OrgSlug = sourceRepo.OrgSlug
		repo.RepoSlug = sourceRepo.RepoSlug
		repo.Visibility = sourceRepo.Visibility
		repo.LastActivityAt = sourceRepo.LastActivityAt
		repo.Language = sourceRepo.Language
		repo.RatingValue = sourceRepo.RatingValue
		repo.WebURL = sourceRepo.WebURL

		if _, err := as.ReposRepo.UpdateRepository(ctx, repo); err != nil {
			return nil, fmt.Errorf(
				"%w: update repository: %v",
				domain.ErrGradingFailed,
				err,
			)
		}
	}
	var grade domain.RepoGrade
	if as.hasPrivateAccess(ctx, sourceRepo) {
		grade, err = as.GradesRepo.GetPrivateGradeByRepoID(ctx, repoID)
		if err != nil {
			if !errors.Is(err, domain.ErrGradeNotFound) {
				return nil, fmt.Errorf(
					"%w: get grade: %v",
					domain.ErrGradingFailed,
					err,
				)
			}
			grade = domain.RepoGrade{
				SourcecraftUUID: repoID,
			}
		}
	} else {
		grade, err = as.GradesRepo.GetPublicGradeByRepoID(ctx, repoID)
		if err != nil {
			if !errors.Is(err, domain.ErrGradeNotFound) {
				return nil, fmt.Errorf(
					"%w: get grade: %v",
					domain.ErrGradingFailed,
					err,
				)
			}
			grade = domain.RepoGrade{
				SourcecraftUUID: repoID,
			}
		}
	}
	if !needsGrading(repo, grade) {
		return &grade, nil
	}

	acquired, err := as.ReposRepo.AcquireRepositoryLock(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: acquire repository lock: %v",
			domain.ErrGradingFailed,
			err,
		)
	}

	if !acquired {
		return nil, domain.GradingPending
	}

	event := domain.GradingRequestedEvent{
		OrgSlug:     repo.OrgSlug,
		RepoSlug:    repo.RepoSlug,
		RequestedAt: time.Now(),
	}

	if err := as.gradingPublisher.Publish(ctx, event); err != nil {
		_ = as.ReposRepo.ReleaseRepositoryLock(ctx, repoID)

		return nil, fmt.Errorf(
			"%w: publish grading request: %v",
			domain.ErrGradingFailed,
			err,
		)
	}

	return nil, domain.GradingPending
}

func needsGrading(repo domain.Repo, grade domain.RepoGrade) bool {
	if grade.GradedAt == nil {
		return true
	}

	if repo.LastActivityAt == nil {
		return false
	}

	return repo.LastActivityAt.After(*grade.GradedAt)
}

func (as *AggregatorService) GetMyRepos(
	ctx context.Context,
) (domain.MyRepositoriesResponse, error) {
	cont, cancel := context.WithTimeout(ctx, as.RPCTimeout)
	defer cancel()

	response, err := as.clientRPC.GetMyRepos(
		cont,
		&client.GetMyReposRequest{},
	)
	if err != nil {
		if status.Code(err) == codes.DeadlineExceeded {
			return domain.MyRepositoriesResponse{}, fmt.Errorf(
				"%w: wrapper RPC timeout: %v",
				domain.ErrTimeout,
				err,
			)
		}
		return domain.MyRepositoriesResponse{}, fmt.Errorf(
			"%w: get my repositories: %v",
			domain.ErrGradingFailed,
			err,
		)
	}

	repositories := make([]domain.Repo, 0, len(response.Repositories))

	for _, repo := range response.Repositories {
		repoID, err := uuid.Parse(repo.GetId())
		if err != nil {
			return domain.MyRepositoriesResponse{}, fmt.Errorf(
				"%w: invalid repository UUID %q: %v",
				domain.ErrGradingFailed,
				repo.GetId(),
				err,
			)
		}

		var lastActivityAt *time.Time

		if value := repo.GetLastActivityAt(); value != "" {
			parsed, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				return domain.MyRepositoriesResponse{}, fmt.Errorf(
					"%w: invalid last_activity_at %q: %v",
					domain.ErrGradingFailed,
					value,
					err,
				)
			}

			lastActivityAt = &parsed
		}

		reactionCounts := make([]domain.ReactionCount, 0, len(
			repo.GetRating().GetReactionCounts(),
		))

		for _, reaction := range repo.GetRating().GetReactionCounts() {
			reactionCounts = append(
				reactionCounts,
				domain.ReactionCount{
					Type:  reaction.GetType(),
					Count: reaction.GetCount(),
				},
			)
		}

		repositories = append(repositories, domain.Repo{
			SourcecraftUUID: repoID,
			OrgSlug:         repo.GetOrganization().GetSlug(),
			RepoSlug:        repo.GetSlug(),
			Visibility:      repo.GetVisibility(),
			LastActivityAt:  lastActivityAt,
			Language: domain.PrimaryLanguage{
				Name:  repo.Language.Name,
				Color: repo.Language.Color,
			},
			WebURL:      repo.GetWebUrl(),
			RatingValue: repo.GetRating().Value,
		})
	}

	return domain.MyRepositoriesResponse{
		Repositories:  repositories,
		NextPageToken: response.GetNextPageToken(),
	}, nil
}

func (as *AggregatorService) StartPublicGrading(ctx context.Context, interval time.Duration) {
	as.ticker = time.NewTicker(interval)

	go func() {
		defer as.ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case <-as.ticker.C:
				if err := as.regradePublicRepos(ctx); err != nil {
					log.Println("error during regrading: ", err.Error())
				}
			}
		}
	}()
}

func (as *AggregatorService) SaveGradingResult(ctx context.Context,
	grade domain.RepoGrade, visibility string, error string) error {
	if error != "" {
		as.ReposRepo.ReleaseRepositoryLock(ctx, grade.SourcecraftUUID)
		return fmt.Errorf(
			"%w: %v",
			domain.ErrGradingFailed,
			error,
		)
	}
	switch visibility {
	case "public":
		if err := as.GradesRepo.UpsertPublicGrade(ctx, grade); err != nil {
			return fmt.Errorf(
				"%w: save public grade: %v",
				domain.ErrGradingFailed,
				err,
			)
		}
	case "private", "internal":
		if err := as.GradesRepo.UpsertPrivateGrade(ctx, grade); err != nil {
			return fmt.Errorf(
				"%w: save private grade: %v",
				domain.ErrGradingFailed,
				err,
			)
		}
	}

	if err := as.ReposRepo.ReleaseRepositoryLock(
		ctx,
		grade.SourcecraftUUID,
	); err != nil {
		return fmt.Errorf(
			"%w: release repository lock: %v",
			domain.ErrGradingFailed,
			err,
		)
	}

	return nil
}

func (as *AggregatorService) regradePublicRepos(ctx context.Context) error {
	cont, cancel := context.WithTimeout(ctx, as.RPCTimeout)
	defer cancel()
	response, err := as.clientRPC.ListAllPublicRepos(
		cont,
		&client.ListAllPublicReposRequest{},
	)
	if err != nil {
		if status.Code(err) == codes.DeadlineExceeded {
			return fmt.Errorf(
				"%w: wrapper RPC timeout: %v",
				domain.ErrTimeout,
				err,
			)
		}
		return fmt.Errorf(
			"%w: list public repositories: %v",
			domain.ErrGradingFailed,
			err,
		)
	}

	for _, pbRepo := range response.Repos {
		repoID, err := uuid.Parse(pbRepo.GetId())
		if err != nil {
			return fmt.Errorf(
				"%w: invalid repository UUID %q: %v",
				domain.ErrGradingFailed,
				pbRepo.GetId(),
				err,
			)
		}

		var lastActivityAt *time.Time

		if value := pbRepo.GetLastActivityAt(); value != "" {
			parsed, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				return fmt.Errorf(
					"%w: invalid last_activity_at for repository %s: %v",
					domain.ErrGradingFailed,
					repoID,
					err,
				)
			}

			lastActivityAt = &parsed
		}

		sourceRepo := domain.Repo{
			SourcecraftUUID: repoID,
			OrgSlug:         pbRepo.GetOrganization().GetSlug(),
			RepoSlug:        pbRepo.GetSlug(),
			Visibility:      pbRepo.GetVisibility(),
			LastActivityAt:  lastActivityAt,
			RatingValue:     pbRepo.Rating.Value,
			Language: domain.PrimaryLanguage{
				Name:  pbRepo.GetLanguage().Name,
				Color: pbRepo.GetLanguage().Color,
			},
			WebURL: pbRepo.GetWebUrl(),
		}

		repo, err := as.ReposRepo.GetRepositoryByID(ctx, repoID)
		if err != nil {
			if !errors.Is(err, domain.ErrRepositoryNotFound) {
				return fmt.Errorf(
					"%w: get public repository %s: %v",
					domain.ErrGradingFailed,
					repoID,
					err,
				)
			}

			repo = sourceRepo

			if _, err := as.ReposRepo.CreateRepository(ctx, repo); err != nil {
				return fmt.Errorf(
					"%w: create public repository %s: %v",
					domain.ErrGradingFailed,
					repoID,
					err,
				)
			}
		} else {

			repo.OrgSlug = sourceRepo.OrgSlug
			repo.RepoSlug = sourceRepo.RepoSlug
			repo.Visibility = sourceRepo.Visibility
			repo.LastActivityAt = sourceRepo.LastActivityAt
			repo.Language = sourceRepo.Language
			repo.RatingValue = sourceRepo.RatingValue
			repo.WebURL = sourceRepo.WebURL

			if _, err := as.ReposRepo.UpdateRepository(ctx, repo); err != nil {
				return fmt.Errorf(
					"%w: update public repository %s: %v",
					domain.ErrGradingFailed,
					repoID,
					err,
				)
			}
		}

		grade, err := as.GradesRepo.GetPublicGradeByRepoID(ctx, repoID)
		if err != nil && !errors.Is(err, domain.ErrGradeNotFound) {
			return fmt.Errorf(
				"%w: get grade %s: %v",
				domain.ErrGradingFailed,
				repoID,
				err,
			)
		}

		if err == nil && !needsGrading(repo, grade) {
			continue
		}

		acquired, err := as.ReposRepo.AcquireRepositoryLock(ctx, repoID)
		if err != nil {
			return fmt.Errorf(
				"%w: acquire repository lock %s: %v",
				domain.ErrGradingFailed,
				repoID,
				err,
			)
		}

		if !acquired {
			continue
		}

		event := domain.GradingRequestedEvent{
			OrgSlug:     repo.OrgSlug,
			RepoSlug:    repo.RepoSlug,
			RequestedAt: time.Now(),
		}

		if err := as.gradingPublisher.Publish(ctx, event); err != nil {

			_ = as.ReposRepo.ReleaseRepositoryLock(ctx, repoID)

			return fmt.Errorf(
				"%w: publish grading request for %s/%s: %v",
				domain.ErrGradingFailed,
				repo.OrgSlug,
				repo.RepoSlug,
				err,
			)
		}
	}

	return nil
}

func tokenFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}

	values := md.Get("authorization")
	if len(values) == 0 {
		return ""
	}
	token := strings.TrimSpace(values[0])
	if token == "" {
		return ""
	}

	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimPrefix(token, "bearer ")
	token = strings.TrimSpace(token)

	if token == "" {
		return ""
	}

	return token
}

func (as *AggregatorService) hasPrivateAccess(ctx context.Context, sourceRepo domain.Repo) bool {
	token := tokenFromContext(ctx)
	if token == "" {
		return false
	} else {
		cont, cancel := context.WithTimeout(ctx, as.RPCTimeout)
		defer cancel()
		response, err := as.clientRPC.GetMyRepos(
			cont,
			&client.GetMyReposRequest{},
		)
		if err != nil {
			return false
		}
		repos := response.GetRepositories()
		for i := 0; i < len(repos); i++ {
			if repos[i].GetSlug() == sourceRepo.RepoSlug && repos[i].GetOrganization().GetSlug() == sourceRepo.OrgSlug {
				return true
			}
		}
		return false
	}
}

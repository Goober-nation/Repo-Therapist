package service

import (
	"aggregator-service/internal/domain"
	pb "aggregator-service/internal/transport/rpc/gen"
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AggregatorService struct {
	ReposRepo        domain.RepoRepository
	GradesRepo       domain.GradesRepository
	LLMRepo          domain.LLMRepository
	ticker           *time.Ticker
	clientRPC        pb.CodeHealthClient
	gradingPublisher domain.GradingPublisher
	llmPublisher     domain.LLMRecommendationsPublisher
}

func NewAggregatorService(reposRepo domain.RepoRepository, gradesRepo domain.GradesRepository, llmRepo domain.LLMRepository,
	clientRPC pb.CodeHealthClient, gradingPublisher domain.GradingPublisher, llmPublisher domain.LLMRecommendationsPublisher) *AggregatorService {
	return &AggregatorService{
		ReposRepo:        reposRepo,
		GradesRepo:       gradesRepo,
		LLMRepo:          llmRepo,
		clientRPC:        clientRPC,
		gradingPublisher: gradingPublisher,
		llmPublisher:     llmPublisher,
	}
}

func (as *AggregatorService) SaveLLMRecommendations(ctx context.Context, event domain.LLMRecommendationsReceivedEvent) error {
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
	response, err := as.clientRPC.GetRepositoryBySlugs(
		ctx,
		&pb.GetRepositoryBySlugsRequest{
			OrgSlug:  orgSlug,
			RepoSlug: repoSlug,
		},
	)
	if err != nil {
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

	// SourceCraft/wrapper is the source of truth.
	sourceRepo := domain.Repo{
		SourcecraftUUID: repoID,
		OrgSlug:         response.GetOrganization().GetSlug(),
		RepoSlug:        response.GetSlug(),
		Visibility:      response.GetVisibility(),
		LastActivityAt:  lastActivityAt,
	}

	// Get repository from our database.
	repo, err := as.ReposRepo.GetRepositoryByID(ctx, repoID)
	if err != nil {
		if !errors.Is(err, domain.ErrRepositoryNotFound) {
			return domain.RepoLLMResponse{}, fmt.Errorf(
				"%w: get repository: %v",
				domain.ErrRecommendationsFailed,
				err,
			)
		}

		// Repository doesn't exist yet.
		// Create it using the latest data from SourceCraft.
		repo = sourceRepo

		if _, err := as.ReposRepo.CreateRepository(ctx, repo); err != nil {
			return domain.RepoLLMResponse{}, fmt.Errorf(
				"%w: create repository: %v",
				domain.ErrRecommendationsFailed,
				err,
			)
		}
	} else {
		// Repository exists.
		// Synchronize all fields that come from SourceCraft.
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

	grade, err := as.GradesRepo.GetGradeByRepoID(ctx, repoID)
	if err != nil {
		return domain.RepoLLMResponse{}, fmt.Errorf(
			"%w: %w",
			domain.ErrNoGrade,
			err,
		)
	}

	switch repo.Visibility {
	case "public":
		resp, err := as.LLMRepo.GetPublicResponseByRepoID(
			ctx,
			repoID,
		)
		if err == nil && llmResponseIsCurrent(
			resp.EvaluatedAt,
			repo.LastActivityAt,
		) {
			return domain.RepoLLMResponse{
				SourcecraftUUID: resp.SourcecraftUUID,
				RawResponse:     resp.RawResponse,
				ModelUsed:       resp.ModelUsed,
				EvaluatedAt:     resp.EvaluatedAt,
			}, nil
		}

		if err != nil && !errors.Is(err, domain.ErrLLMResponseNotFound) {
			return domain.RepoLLMResponse{}, fmt.Errorf(
				"%w: get public llm response: %v",
				domain.ErrLLMUnavailable,
				err,
			)
		}

	case "private", "internal":
		resp, err := as.LLMRepo.GetPrivateResponseByRepoID(
			ctx,
			repoID,
		)
		if err == nil && llmResponseIsCurrent(
			resp.EvaluatedAt,
			repo.LastActivityAt,
		) {
			return domain.RepoLLMResponse{
				SourcecraftUUID: resp.SourcecraftUUID,
				RawResponse:     resp.RawResponse,
				ModelUsed:       resp.ModelUsed,
				EvaluatedAt:     resp.EvaluatedAt,
			}, nil
		}

		if err != nil && !errors.Is(err, domain.ErrLLMResponseNotFound) {
			return domain.RepoLLMResponse{}, fmt.Errorf(
				"%w: get private llm response: %v",
				domain.ErrLLMUnavailable,
				err,
			)
		}

	default:
		return domain.RepoLLMResponse{}, fmt.Errorf(
			"%w: unknown repository visibility %q",
			domain.ErrRecommendationsFailed,
			repo.Visibility,
		)
	}

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
	response, err := as.clientRPC.GetRepositoryBySlugs(
		ctx,
		&pb.GetRepositoryBySlugsRequest{
			OrgSlug:  orgSlug,
			RepoSlug: repoSlug,
		},
	)
	if err != nil {
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

	// SourceCraft/wrapper is the source of truth.
	sourceRepo := domain.Repo{
		SourcecraftUUID: repoID,
		OrgSlug:         response.GetOrganization().GetSlug(),
		RepoSlug:        response.GetSlug(),
		Visibility:      response.GetVisibility(),
		LastActivityAt:  lastActivityAt}

	// Get repository from our database.
	repo, err := as.ReposRepo.GetRepositoryByID(ctx, repoID)
	if err != nil {
		if !errors.Is(err, domain.ErrRepositoryNotFound) {
			return nil, fmt.Errorf(
				"%w: get repository: %v",
				domain.ErrGradingFailed,
				err,
			)
		}

		// Repository doesn't exist yet.
		// Create it using the latest data from SourceCraft.
		repo = sourceRepo

		if _, err := as.ReposRepo.CreateRepository(ctx, repo); err != nil {
			return nil, fmt.Errorf(
				"%w: create repository: %v",
				domain.ErrGradingFailed,
				err,
			)
		}
	} else {
		// Repository exists.
		// Synchronize all fields that come from SourceCraft.
		repo.OrgSlug = sourceRepo.OrgSlug
		repo.RepoSlug = sourceRepo.RepoSlug
		repo.Visibility = sourceRepo.Visibility
		repo.LastActivityAt = sourceRepo.LastActivityAt

		if _, err := as.ReposRepo.UpdateRepository(ctx, repo); err != nil {
			return nil, fmt.Errorf(
				"%w: update repository: %v",
				domain.ErrGradingFailed,
				err,
			)
		}
	}

	grade, err := as.GradesRepo.GetGradeByRepoID(ctx, repoID)
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
	response, err := as.clientRPC.GetMyRepos(
		ctx,
		&pb.GetMyReposRequest{},
	)
	if err != nil {
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

func (as *AggregatorService) SaveGradingResult(ctx context.Context, grade domain.RepoGrade) error {
	if err := as.GradesRepo.UpsertGrade(ctx, grade); err != nil {
		return fmt.Errorf(
			"%w: save grade: %v",
			domain.ErrGradingFailed,
			err,
		)
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
	response, err := as.clientRPC.ListAllPublicRepos(
		ctx,
		&pb.ListAllPublicReposRequest{},
	)
	if err != nil {
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

		// SourceCraft/wrapper is the source of truth.
		sourceRepo := domain.Repo{
			SourcecraftUUID: repoID,
			OrgSlug:         pbRepo.GetOrganization().GetSlug(),
			RepoSlug:        pbRepo.GetSlug(),
			Visibility:      pbRepo.GetVisibility(),
			LastActivityAt:  lastActivityAt,
		}

		// Get repository from our database.
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

			// Repository doesn't exist yet.
			// Create it using the latest data from SourceCraft.
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
			// Repository exists.
			// Synchronize fields that come from SourceCraft.
			repo.OrgSlug = sourceRepo.OrgSlug
			repo.RepoSlug = sourceRepo.RepoSlug
			repo.Visibility = sourceRepo.Visibility
			repo.LastActivityAt = sourceRepo.LastActivityAt

			if _, err := as.ReposRepo.UpdateRepository(ctx, repo); err != nil {
				return fmt.Errorf(
					"%w: update public repository %s: %v",
					domain.ErrGradingFailed,
					repoID,
					err,
				)
			}
		}

		grade, err := as.GradesRepo.GetGradeByRepoID(ctx, repoID)
		if err != nil && !errors.Is(err, domain.ErrGradeNotFound) {
			return fmt.Errorf(
				"%w: get grade %s: %v",
				domain.ErrGradingFailed,
				repoID,
				err,
			)
		}

		// Grade exists and is still up to date.
		if err == nil && !needsGrading(repo, grade) {
			continue
		}

		// Try to acquire the lock.
		acquired, err := as.ReposRepo.AcquireRepositoryLock(ctx, repoID)
		if err != nil {
			return fmt.Errorf(
				"%w: acquire repository lock %s: %v",
				domain.ErrGradingFailed,
				repoID,
				err,
			)
		}

		// Another grading is already in progress.
		if !acquired {
			continue
		}

		event := domain.GradingRequestedEvent{
			OrgSlug:     repo.OrgSlug,
			RepoSlug:    repo.RepoSlug,
			RequestedAt: time.Now(),
		}

		if err := as.gradingPublisher.Publish(ctx, event); err != nil {
			// Do not leave repository locked if publishing failed.
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

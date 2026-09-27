package service

import (
	"context"
	"errors"
	"fmt"
	"gateway-service/internal/domain"
	"gateway-service/internal/transport/http"
	"gateway-service/internal/transport/rpc/gen/agg"
	"gateway-service/internal/transport/rpc/gen/rank"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GatewayService struct {
	aggregatorClient agg.GatewayClient
	ratingClient     rank.RatingClient

	aggregatorTimeout time.Duration
	ratingTimeout     time.Duration
	yandexTimeout     time.Duration

	rateLimiter  Limiter
	yandexClient http.YandexIDClient

	userRepository    domain.UserRepository
	sessionRepository domain.SessionRepository
}

func NewGatewayService(
	aggregatorClient agg.GatewayClient,
	ratingClient rank.RatingClient,
	yandexClient http.YandexIDClient,
	rateLimiter Limiter,
	userRepository domain.UserRepository,
	sessionRepository domain.SessionRepository,
	aggregatorTimeout time.Duration,
	ratingTimeout time.Duration,
	yandexTimeout time.Duration,
) *GatewayService {
	return &GatewayService{
		aggregatorClient:  aggregatorClient,
		ratingClient:      ratingClient,
		yandexClient:      yandexClient,
		rateLimiter:       rateLimiter,
		userRepository:    userRepository,
		sessionRepository: sessionRepository,
		ratingTimeout:     ratingTimeout,
		aggregatorTimeout: aggregatorTimeout,
		yandexTimeout:     yandexTimeout,
	}
}
func (s *GatewayService) checkRateLimit(
	ctx context.Context,
	clientIP string,
) error {

	allowed, err := s.rateLimiter.Allow(
		ctx,
		"rate_limit:gateway:ip:"+clientIP,
	)
	if err != nil {
		return fmt.Errorf("check rate limit: %w", err)
	}

	if !allowed {
		return domain.ErrLimitExceeded
	}

	return nil
}

func (s *GatewayService) authorizeSession(
	ctx context.Context,
	sessionID string,
) (uuid.UUID, error) {
	if sessionID == "" {
		return uuid.Nil, domain.ErrUnauthenticated
	}

	userID, err := s.sessionRepository.GetSession(
		ctx,
		sessionID,
	)
	if err != nil {
		if errors.Is(err, domain.ErrSessionNotFound) {
			return uuid.Nil, domain.ErrSessionExpired
		}

		return uuid.Nil, fmt.Errorf(
			"get session: %w",
			err,
		)
	}

	return userID, nil
}

func (s *GatewayService) GetMyRepos(
	ctx context.Context,
	sessionID string,
	clientIP string,
) (domain.GetMyReposDTO, error) {
	if err := s.checkRateLimit(ctx, clientIP); err != nil {
		return domain.GetMyReposDTO{}, err
	}
	_, err := s.authorizeSession(ctx, sessionID)
	if err != nil {
		return domain.GetMyReposDTO{}, err
	}
	cont, cancel := context.WithTimeout(ctx, s.aggregatorTimeout)
	defer cancel()
	resp, err := s.aggregatorClient.GetMyRepos(
		cont,
		&agg.GetMyReposRequest{},
	)
	if err != nil {
		if status.Code(err) == codes.DeadlineExceeded {
			return domain.GetMyReposDTO{}, fmt.Errorf(
				"get my repos rpc: %w",
				domain.ErrTimeout,
			)
		}
		return domain.GetMyReposDTO{}, fmt.Errorf(
			"get my repos rpc: %w",
			err,
		)
	}
	result, err := mapGetMyReposResponse(resp)
	if err != nil {
		return domain.GetMyReposDTO{}, err
	}
	return result, nil
}

func mapGetMyReposResponse(
	resp *agg.GetMyReposResponse,
) (domain.GetMyReposDTO, error) {
	repositories := make([]domain.Repo, 0, len(resp.Repositories))

	for _, repo := range resp.Repositories {
		rep, err := mapRepository(repo)
		if err != nil {
			return domain.GetMyReposDTO{}, err
		}
		repositories = append(
			repositories,
			rep,
		)
	}

	return domain.GetMyReposDTO{
		Repositories:  repositories,
		NextPageToken: resp.NextPageToken,
	}, nil
}

func mapRepository(repo *agg.Repository) (domain.Repo, error) {
	repoID, err := uuid.Parse(repo.Id)
	if err != nil {
		return domain.Repo{}, fmt.Errorf("%w: %v", domain.ErrRequestFailed, err)
	}
	result := domain.Repo{
		SourcecraftUUID: repoID,
		RepoSlug:        repo.Slug,
		WebURL:          repo.WebUrl,
		Visibility:      repo.Visibility,
		RatingValue:     repo.Rating,
	}
	if repo.LastActivityAt != "" {
		parsed, err := time.Parse(time.RFC3339Nano, repo.LastActivityAt)
		if err != nil {
			return domain.Repo{}, fmt.Errorf("%w: %v", domain.ErrRequestFailed, err)
		}
		result.LastActivityAt = &parsed
	}
	if repo.Organization != nil {
		result.OrgSlug = repo.Organization.Slug
	}

	if repo.Language != nil {
		result.Language = domain.PrimaryLanguage{
			Name:  repo.Language.Name,
			Color: repo.Language.Color,
		}
	}

	return result, nil
}

func (s *GatewayService) RequestLLMRecommendations(
	ctx context.Context,
	clientIP string,
	request domain.RequestLLMRecommendationsDTO,
) (domain.RequestLLMRecommendationsResponse, error) {
	if err := s.checkRateLimit(ctx, clientIP); err != nil {
		return domain.RequestLLMRecommendationsResponse{}, err
	}

	rpcCtx, cancel := context.WithTimeout(ctx, s.aggregatorTimeout)
	defer cancel()

	resp, err := s.aggregatorClient.RequestLLMRecommendations(
		rpcCtx,
		&agg.RequestLLMRecommendationsRequest{
			OrgSlug:  request.OrgSlug,
			RepoSlug: request.RepoSlug,
		},
	)
	if err != nil {
		if status.Code(err) == codes.DeadlineExceeded {
			return domain.RequestLLMRecommendationsResponse{}, fmt.Errorf(
				"request llm recommendations rpc: %w",
				domain.ErrTimeout,
			)
		}

		return domain.RequestLLMRecommendationsResponse{}, fmt.Errorf(
			"request llm recommendations rpc: %w",
			err,
		)
	}

	if resp == nil {
		return domain.RequestLLMRecommendationsResponse{}, fmt.Errorf(
			"request llm recommendations rpc: %w",
			domain.ErrRequestFailed,
		)
	}

	if resp.EvaluatedAt == nil {
		return domain.RequestLLMRecommendationsResponse{}, fmt.Errorf(
			"%w: evaluated_at is missing",
			domain.ErrRequestFailed,
		)
	}

	if err := resp.EvaluatedAt.CheckValid(); err != nil {
		return domain.RequestLLMRecommendationsResponse{}, fmt.Errorf(
			"%w: invalid evaluated_at: %v",
			domain.ErrRequestFailed,
			err,
		)
	}

	return domain.RequestLLMRecommendationsResponse{
		ModelUsed:   resp.ModelUsed,
		EvaluatedAt: resp.EvaluatedAt.AsTime(),
		RawResponse: append([]byte(nil), resp.RawResponse...),
	}, nil
}

func (s *GatewayService) GetGrading(
	ctx context.Context,
	clientIP string,
	request domain.GetGradingDTO,
) (domain.GetGradingResponse, error) {
	if err := s.checkRateLimit(ctx, clientIP); err != nil {
		return domain.GetGradingResponse{}, err
	}

	if request.OrgSlug == "" {
		return domain.GetGradingResponse{}, fmt.Errorf(
			"%w: org_slug is required",
			domain.ErrInvalidRequest,
		)
	}

	if request.RepoSlug == "" {
		return domain.GetGradingResponse{}, fmt.Errorf(
			"%w: repo_slug is required",
			domain.ErrInvalidRequest,
		)
	}

	rpcCtx, cancel := context.WithTimeout(ctx, s.aggregatorTimeout)
	defer cancel()

	resp, err := s.aggregatorClient.GetGrading(
		rpcCtx,
		&agg.GetGradingRequest{
			OrgSlug:  request.OrgSlug,
			RepoSlug: request.RepoSlug,
		},
	)
	if err != nil {
		if status.Code(err) == codes.DeadlineExceeded {
			return domain.GetGradingResponse{}, fmt.Errorf(
				"get grading rpc: %w",
				domain.ErrTimeout,
			)
		}

		return domain.GetGradingResponse{}, fmt.Errorf(
			"get grading rpc: %w",
			err,
		)
	}

	if resp == nil {
		return domain.GetGradingResponse{}, fmt.Errorf(
			"get grading rpc: %w",
			domain.ErrRequestFailed,
		)
	}

	grade, err := mapRepoGrade(resp.Grade)
	if err != nil {
		return domain.GetGradingResponse{}, err
	}

	return domain.GetGradingResponse{
		Grade: grade,
	}, nil
}

func mapRepoGrade(
	grade *agg.RepoGrade,
) (domain.RepoGrade, error) {
	if grade == nil {
		return domain.RepoGrade{}, fmt.Errorf(
			"%w: grade is nil",
			domain.ErrRequestFailed,
		)
	}

	sourcecraftUUID, err := uuid.Parse(grade.SourcecraftUuid)
	if err != nil {
		return domain.RepoGrade{}, fmt.Errorf(
			"%w: invalid sourcecraft uuid: %v",
			domain.ErrRequestFailed,
			err,
		)
	}

	if grade.GradedAt == nil {
		return domain.RepoGrade{}, fmt.Errorf(
			"%w: graded_at is missing",
			domain.ErrRequestFailed,
		)
	}

	if err := grade.GradedAt.CheckValid(); err != nil {
		return domain.RepoGrade{}, fmt.Errorf(
			"%w: invalid graded_at: %v",
			domain.ErrRequestFailed,
			err,
		)
	}

	return domain.RepoGrade{
		SourcecraftUUID: sourcecraftUUID,
		Score:           grade.Score,
		CategoryScores:  append([]byte(nil), grade.CategoryScores...),
		Advice:          append([]byte(nil), grade.Advice...),
		MissingData:     append([]string(nil), grade.MissingData...),
		GradedAt:        grade.GradedAt.AsTime(),
	}, nil
}

func (s *GatewayService) GetRanking(
	ctx context.Context,
	clientIP string,
	request domain.GetRankingDTO,
) (domain.GetRankingResponse, error) {
	if err := s.checkRateLimit(ctx, clientIP); err != nil {
		return domain.GetRankingResponse{}, err
	}

	rpcCtx, cancel := context.WithTimeout(
		ctx,
		s.ratingTimeout,
	)
	defer cancel()

	resp, err := s.ratingClient.GetRanking(
		rpcCtx,
		&rank.GetRankingRequest{
			Page:     request.Page,
			PageSize: request.PageSize,
			Language: request.Language,
			SortBy:   mapRankingSortBy(request.SortBy),
		},
	)
	if err != nil {
		if status.Code(err) == codes.DeadlineExceeded {
			return domain.GetRankingResponse{}, fmt.Errorf(
				"get ranking rpc: %w",
				domain.ErrTimeout,
			)
		}

		return domain.GetRankingResponse{}, fmt.Errorf(
			"get ranking rpc: %w",
			err,
		)
	}

	if resp == nil {
		return domain.GetRankingResponse{}, fmt.Errorf(
			"get ranking rpc: %w",
			domain.ErrRequestFailed,
		)
	}

	return mapGetRankingResponse(resp)
}

func mapRankingSortBy(
	sortBy domain.RankingSortBy,
) rank.SortBy {
	switch sortBy {
	case domain.RankingSortByLastActivity:
		return rank.SortBy_SORT_BY_LAST_ACTIVITY

	case domain.RankingSortByLikes:
		return rank.SortBy_SORT_BY_LIKES

	case domain.RankingSortByScore:
		fallthrough

	default:
		return rank.SortBy_SORT_BY_SCORE
	}
}

func mapGetRankingResponse(
	resp *rank.GetRankingResponse,
) (domain.GetRankingResponse, error) {
	entries := make(
		[]domain.RankingEntry,
		0,
		len(resp.Entries),
	)

	for _, entry := range resp.Entries {
		if entry == nil {
			return domain.GetRankingResponse{}, fmt.Errorf(
				"%w: ranking entry is nil",
				domain.ErrRequestFailed,
			)
		}

		mapped, err := mapRankingEntry(entry)
		if err != nil {
			return domain.GetRankingResponse{}, err
		}

		entries = append(entries, mapped)
	}

	return domain.GetRankingResponse{
		Entries:    entries,
		TotalCount: resp.TotalCount,
		Page:       resp.Page,
		PageSize:   resp.PageSize,
	}, nil
}

func mapRankingEntry(
	entry *rank.RankingEntry,
) (domain.RankingEntry, error) {
	sourcecraftUUID, err := uuid.Parse(
		entry.SourcecraftUuid,
	)
	if err != nil {
		return domain.RankingEntry{}, fmt.Errorf(
			"%w: invalid sourcecraft uuid %q: %v",
			domain.ErrRequestFailed,
			entry.SourcecraftUuid,
			err,
		)
	}

	result := domain.RankingEntry{
		Place:           entry.Place,
		SourcecraftUUID: sourcecraftUUID,
		Name:            entry.Name,
		WebURL:          entry.WebUrl,
	}

	if entry.PublicScore != nil {
		score := *entry.PublicScore
		result.PublicScore = &score
	}

	if entry.Likes != nil {
		likes := *entry.Likes
		result.Likes = &likes
	}

	if entry.Language != nil {
		result.Language = domain.PrimaryLanguage{
			Name:  entry.Language.Name,
			Color: entry.Language.Color,
		}
	}

	if entry.LastActivityAt != nil {
		if err := entry.LastActivityAt.CheckValid(); err != nil {
			return domain.RankingEntry{}, fmt.Errorf(
				"%w: invalid last_activity_at: %v",
				domain.ErrRequestFailed,
				err,
			)
		}

		lastActivityAt := entry.LastActivityAt.AsTime()
		result.LastActivityAt = &lastActivityAt
	}

	return result, nil
}

func (s *GatewayService) LoginWithYandex(
	ctx context.Context,
	request domain.YandexLoginDTO,
) (domain.LoginResponse, error) {
	if strings.TrimSpace(request.OAuthToken) == "" {
		return domain.LoginResponse{}, fmt.Errorf(
			"%w: oauth token is required",
			domain.ErrInvalidRequest,
		)
	}
	cont, cancel := context.WithTimeout(ctx, s.yandexTimeout)
	defer cancel()
	yandexUser, err := s.yandexClient.GetUser(
		cont,
		request.OAuthToken,
	)
	if err != nil {
		return domain.LoginResponse{}, fmt.Errorf(
			"get yandex user: %w",
			err,
		)
	}

	user, err := s.userRepository.GetUserByYID(
		ctx,
		yandexUser.ID,
	)
	if err != nil {
		if !errors.Is(err, domain.ErrUserNotFound) {
			return domain.LoginResponse{}, fmt.Errorf(
				"get user by yandex id: %w",
				err,
			)
		}

		user = domain.User{
			ID:        uuid.New(),
			YandexID:  yandexUser.ID,
			CreatedAt: time.Now(),
		}

		if err := s.userRepository.CreateUser(ctx, user); err != nil {
			if !errors.Is(err, domain.ErrUserAlreadyExists) {
				return domain.LoginResponse{}, fmt.Errorf(
					"create user: %w",
					err,
				)
			}

			user, err = s.userRepository.GetUserByYID(
				ctx,
				yandexUser.ID,
			)
			if err != nil {
				return domain.LoginResponse{}, fmt.Errorf(
					"get existing user after create conflict: %w",
					err,
				)
			}
		}
	}

	sessionID, err := s.sessionRepository.CreateSession(
		ctx,
		user.ID,
	)
	if err != nil {
		return domain.LoginResponse{}, fmt.Errorf(
			"create session: %w",
			err,
		)
	}

	return domain.LoginResponse{
		SessionID: sessionID,
	}, nil
}

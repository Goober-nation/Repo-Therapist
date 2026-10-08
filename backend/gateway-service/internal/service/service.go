package service

import (
	"context"
	"errors"
	"fmt"
	"gateway-service/internal/domain"
	"gateway-service/internal/transport/http"
	"gateway-service/internal/transport/rpc/gen/agg"
	"gateway-service/internal/transport/rpc/gen/rank"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type GatewayService struct {
	aggregatorClient agg.AggregatorClient
	ratingClient     rank.RatingClient

	aggregatorTimeout time.Duration
	ratingTimeout     time.Duration
	yandexTimeout     time.Duration

	rateLimiter  Limiter
	yandexClient http.YandexIDClient

	userRepository    domain.UserRepository
	sessionRepository domain.SessionRepository

	// publicAPILimiter limits the JSON score API per client IP; nil means no limit.
	publicAPILimiter Limiter
	// exportLimiter limits the public grades export per client IP; nil means no limit.
	exportLimiter Limiter

	// Admin access: the Yandex IDs of the admins, the key of the actor hash and the limiter of
	// recorded denials (see SetAdmin).
	adminIDs       map[string]struct{}
	auditSecret    []byte
	adminDeniedLog Limiter

	// Password unlock (see SetAdminUnlock).
	series        SeriesSource
	adminMetrics  AdminMetrics
	adminPassword string
	adminGrants   domain.AdminGrantStore
	attemptGuard  domain.AttemptGuard
	grantTTL      time.Duration
}

func NewGatewayService(
	aggregatorClient agg.AggregatorClient,
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

// SetPublicAPILimiter sets the limiter for the public JSON score API, which has its own, higher
// per-IP budget than the frontend routes. Badges and the widget are never limited.
func (s *GatewayService) SetPublicAPILimiter(limiter Limiter) {
	s.publicAPILimiter = limiter
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

// withPAT attaches the caller's SourceCraft PAT for the aggregator. Without one the aggregator
// answers with public data only.
func withPAT(ctx context.Context, pat string) context.Context {
	if pat == "" {
		return ctx
	}

	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+pat)
}

// gradingRateLimitedPrefix mirrors the aggregator's GradingRateLimitedPrefix: its
// ResourceExhausted message for the service-wide grading limit, followed by the wait in seconds.
const gradingRateLimitedPrefix = "grading rate limited, retry after seconds: "

// gradingInterruptedMessage mirrors the aggregator's GradingInterruptedMessage (codes.Aborted).
const gradingInterruptedMessage = "grading interrupted"

func gradingLimit(message string) (domain.GradingLimitError, bool) {
	rest, ok := strings.CutPrefix(message, gradingRateLimitedPrefix)
	if !ok {
		return domain.GradingLimitError{}, false
	}

	seconds, err := strconv.Atoi(rest)
	if err != nil || seconds < 0 {
		return domain.GradingLimitError{}, false
	}

	return domain.GradingLimitError{RetryAfter: time.Duration(seconds) * time.Second}, true
}

// rpcError maps an upstream gRPC status to the domain error the HTTP layer understands.
func rpcError(op string, err error) error {
	var target error

	switch status.Code(err) {
	case codes.DeadlineExceeded:
		target = domain.ErrTimeout
	case codes.InvalidArgument:
		target = domain.ErrInvalidRequest
	case codes.NotFound:
		target = domain.ErrNotFound
	case codes.PermissionDenied:
		target = domain.ErrForbidden
	case codes.Unauthenticated:
		target = domain.ErrUnauthenticated
	case codes.FailedPrecondition:
		target = domain.ErrPending
	case codes.AlreadyExists:
		target = domain.ErrConflict
	case codes.Aborted:
		target = domain.ErrConflict
		if status.Convert(err).Message() == gradingInterruptedMessage {
			target = domain.ErrGradingInterrupted
		}
	case codes.ResourceExhausted:
		target = domain.ErrLimitExceeded
		if limit, ok := gradingLimit(status.Convert(err).Message()); ok {
			target = limit
		}
	case codes.Unavailable:
		target = domain.ErrUnavailable
	default:
		target = domain.ErrRequestFailed
	}

	return fmt.Errorf("%s rpc: %w: %v", op, target, err)
}

func (s *GatewayService) GetMyRepos(
	ctx context.Context,
	clientIP string,
	pat string,
) (domain.GetMyReposDTO, error) {
	if err := s.checkRateLimit(ctx, clientIP); err != nil {
		return domain.GetMyReposDTO{}, err
	}

	if pat == "" {
		return domain.GetMyReposDTO{}, domain.ErrUnauthenticated
	}

	rpcCtx, cancel := context.WithTimeout(withPAT(ctx, pat), s.aggregatorTimeout)
	defer cancel()

	resp, err := s.aggregatorClient.GetMyRepos(
		rpcCtx,
		&agg.GetMyReposRequest{},
	)
	if err != nil {
		return domain.GetMyReposDTO{}, rpcError("get my repos", err)
	}

	return mapGetMyReposResponse(resp)
}

func mapGetMyReposResponse(
	resp *agg.GetMyReposResponse,
) (domain.GetMyReposDTO, error) {
	repositories := make([]domain.Repo, 0, len(resp.GetRepositories()))

	for _, repo := range resp.GetRepositories() {
		mapped, err := mapRepository(repo)
		if err != nil {
			return domain.GetMyReposDTO{}, err
		}

		repositories = append(repositories, mapped)
	}

	return domain.GetMyReposDTO{
		Repositories:  repositories,
		NextPageToken: resp.GetNextPageToken(),
	}, nil
}
func mapRepository(repo *agg.Repository) (domain.Repo, error) {
	repoID, err := uuid.Parse(repo.GetId())
	if err != nil {
		return domain.Repo{}, fmt.Errorf("%w: %v", domain.ErrRequestFailed, err)
	}

	result := domain.Repo{
		SourcecraftUUID: repoID,
		OrgSlug:         repo.GetOrganization().GetSlug(),
		RepoSlug:        repo.GetSlug(),
		WebURL:          repo.GetWebUrl(),
		Visibility:      repo.GetVisibility(),
		RatingValue:     repo.GetRating(),
		Language: domain.PrimaryLanguage{
			Name:  repo.GetLanguage().GetName(),
			Color: repo.GetLanguage().GetColor(),
		},
	}

	if value := repo.GetLastActivityAt(); value != "" {
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return domain.Repo{}, fmt.Errorf("%w: %v", domain.ErrRequestFailed, err)
		}

		result.LastActivityAt = &parsed
	}

	return result, nil
}

func validateSlugs(orgSlug, repoSlug string) error {
	if orgSlug == "" {
		return fmt.Errorf("%w: org_slug is required", domain.ErrInvalidRequest)
	}

	if repoSlug == "" {
		return fmt.Errorf("%w: repo_slug is required", domain.ErrInvalidRequest)
	}

	return nil
}

// RequestLLMRecommendations returns ErrPending while the recommendations (or the grade they
// need) are still being computed.
func (s *GatewayService) RequestLLMRecommendations(
	ctx context.Context,
	clientIP string,
	pat string,
	request domain.RequestLLMRecommendationsDTO,
) (domain.RequestLLMRecommendationsResponse, error) {
	if err := s.checkRateLimit(ctx, clientIP); err != nil {
		return domain.RequestLLMRecommendationsResponse{}, err
	}

	if err := validateSlugs(request.OrgSlug, request.RepoSlug); err != nil {
		return domain.RequestLLMRecommendationsResponse{}, err
	}

	rpcCtx, cancel := context.WithTimeout(withPAT(ctx, pat), s.aggregatorTimeout)
	defer cancel()

	resp, err := s.aggregatorClient.RequestLLMRecommendations(
		rpcCtx,
		&agg.RequestLLMRecommendationsRequest{
			OrgSlug:  request.OrgSlug,
			RepoSlug: request.RepoSlug,
		},
	)
	if err != nil {
		return domain.RequestLLMRecommendationsResponse{}, rpcError("request llm recommendations", err)
	}

	if resp.GetEvaluatedAt() == nil {
		return domain.RequestLLMRecommendationsResponse{}, fmt.Errorf(
			"%w: evaluated_at is missing",
			domain.ErrRequestFailed,
		)
	}

	if err := resp.GetEvaluatedAt().CheckValid(); err != nil {
		return domain.RequestLLMRecommendationsResponse{}, fmt.Errorf(
			"%w: invalid evaluated_at: %v",
			domain.ErrRequestFailed,
			err,
		)
	}

	return domain.RequestLLMRecommendationsResponse{
		ModelUsed:   resp.GetModelUsed(),
		EvaluatedAt: resp.GetEvaluatedAt().AsTime(),
		RawResponse: append([]byte(nil), resp.GetRawResponse()...),
	}, nil
}

// GetGrading returns ErrPending while the grade is still being computed.
func (s *GatewayService) GetGrading(
	ctx context.Context,
	clientIP string,
	pat string,
	request domain.GetGradingDTO,
) (domain.GetGradingResponse, error) {
	if err := s.checkRateLimit(ctx, clientIP); err != nil {
		return domain.GetGradingResponse{}, err
	}

	if err := validateSlugs(request.OrgSlug, request.RepoSlug); err != nil {
		return domain.GetGradingResponse{}, err
	}

	rpcCtx, cancel := context.WithTimeout(withPAT(ctx, pat), s.aggregatorTimeout)
	defer cancel()

	resp, err := s.aggregatorClient.GetGrading(
		rpcCtx,
		&agg.GetGradingRequest{
			OrgSlug:  request.OrgSlug,
			RepoSlug: request.RepoSlug,
		},
	)
	if err != nil {
		return domain.GetGradingResponse{}, rpcError("get grading", err)
	}

	grade, err := mapRepoGrade(resp.GetGrade())
	if err != nil {
		return domain.GetGradingResponse{}, err
	}

	return domain.GetGradingResponse{
		Grade: grade,
	}, nil
}

// GetAnalytics returns the stored analytics of the repo for the view the caller may see. It never
// starts a grading: a repo without analytics comes back empty, which the caller reports as no data.
func (s *GatewayService) GetAnalytics(
	ctx context.Context,
	clientIP string,
	pat string,
	orgSlug string,
	repoSlug string,
) (domain.RepoAnalytics, error) {
	if err := s.checkRateLimit(ctx, clientIP); err != nil {
		return domain.RepoAnalytics{}, err
	}

	if err := validateSlugs(orgSlug, repoSlug); err != nil {
		return domain.RepoAnalytics{}, err
	}

	rpcCtx, cancel := context.WithTimeout(withPAT(ctx, pat), s.aggregatorTimeout)
	defer cancel()

	resp, err := s.aggregatorClient.GetAnalytics(
		rpcCtx,
		&agg.GetAnalyticsRequest{OrgSlug: orgSlug, RepoSlug: repoSlug},
	)
	if err != nil {
		return domain.RepoAnalytics{}, rpcError("get analytics", err)
	}

	result := domain.RepoAnalytics{Analytics: resp.GetAnalytics()}
	if resp.GetGradedAt() != nil {
		gradedAt := resp.GetGradedAt().AsTime()
		result.GradedAt = &gradedAt
	}

	return result, nil
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

	sourcecraftUUID, err := uuid.Parse(grade.GetSourcecraftUuid())
	if err != nil {
		return domain.RepoGrade{}, fmt.Errorf(
			"%w: invalid sourcecraft uuid: %v",
			domain.ErrRequestFailed,
			err,
		)
	}

	if grade.GetGradedAt() == nil {
		return domain.RepoGrade{}, fmt.Errorf(
			"%w: graded_at is missing",
			domain.ErrRequestFailed,
		)
	}

	if err := grade.GetGradedAt().CheckValid(); err != nil {
		return domain.RepoGrade{}, fmt.Errorf(
			"%w: invalid graded_at: %v",
			domain.ErrRequestFailed,
			err,
		)
	}

	return domain.RepoGrade{
		SourcecraftUUID: sourcecraftUUID,
		Score:           grade.Score,
		CategoryScores:  append([]byte(nil), grade.GetCategoryScores()...),
		Advice:          append([]byte(nil), grade.GetAdvice()...),
		MissingData:     append([]string(nil), grade.GetMissingData()...),
		GradedAt:        grade.GetGradedAt().AsTime(),
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

			HideLowConfidence: request.HideLowConfidence,
			Size:              request.Size,
			HasCi:             request.HasCI,
			HasSecurityPolicy: request.HasSecurityPolicy,
			Topic:             request.Topic,
		},
	)
	if err != nil {
		return domain.GetRankingResponse{}, rpcError("get ranking", err)
	}

	return mapGetRankingResponse(resp)
}

// GetPublicScore returns the stored public score of a public repo for the README badge; nil
// means not graded yet. It never starts a grading and skips the per-IP limit, since READMEs are
// viewed by many clients and a 429 would show as a broken image.
func (s *GatewayService) GetPublicScore(
	ctx context.Context,
	orgSlug string,
	repoSlug string,
) (*int32, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, s.ratingTimeout)
	defer cancel()

	resp, err := s.ratingClient.GetPublicScore(
		rpcCtx,
		&rank.GetPublicScoreRequest{OrgSlug: orgSlug, RepoSlug: repoSlug},
	)
	if err != nil {
		return nil, rpcError("get public score", err)
	}

	return resp.PublicScore, nil
}

// GetPublicSummary returns the stored public grade of a public repo with its category scores.
// It never starts a grading and skips the per-IP limit, like GetPublicScore, so badges and the
// widget embedded in READMEs do not break under load.
func (s *GatewayService) GetPublicSummary(
	ctx context.Context,
	orgSlug string,
	repoSlug string,
) (domain.PublicSummary, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, s.ratingTimeout)
	defer cancel()

	resp, err := s.ratingClient.GetPublicSummary(
		rpcCtx,
		&rank.GetPublicSummaryRequest{OrgSlug: orgSlug, RepoSlug: repoSlug},
	)
	if err != nil {
		return domain.PublicSummary{}, rpcError("get public summary", err)
	}

	return mapPublicSummary(resp), nil
}

// allowPublicAPI applies the public API's own per-IP limit; nil limiter means no limit.
func (s *GatewayService) allowPublicAPI(ctx context.Context, clientIP string) error {
	if s.publicAPILimiter == nil {
		return nil
	}

	allowed, err := s.publicAPILimiter.Allow(ctx, "rate_limit:gateway:public:ip:"+clientIP)
	if err != nil {
		return fmt.Errorf("check public api rate limit: %w", err)
	}

	if !allowed {
		return domain.ErrLimitExceeded
	}

	return nil
}

// GetScore is GetPublicSummary behind the public API's own per-IP limit.
func (s *GatewayService) GetScore(
	ctx context.Context,
	clientIP string,
	orgSlug string,
	repoSlug string,
) (domain.PublicSummary, error) {
	if err := s.allowPublicAPI(ctx, clientIP); err != nil {
		return domain.PublicSummary{}, err
	}

	return s.GetPublicSummary(ctx, orgSlug, repoSlug)
}

// GetScoreHistory returns the stored public score history of a public repo, oldest first, behind
// the public API's per-IP limit. It only reads: it never starts a grading.
func (s *GatewayService) GetScoreHistory(
	ctx context.Context,
	clientIP string,
	orgSlug string,
	repoSlug string,
) ([]domain.HistoryPoint, error) {
	if err := s.allowPublicAPI(ctx, clientIP); err != nil {
		return nil, err
	}

	rpcCtx, cancel := context.WithTimeout(ctx, s.ratingTimeout)
	defer cancel()

	resp, err := s.ratingClient.GetScoreHistory(
		rpcCtx,
		&rank.GetScoreHistoryRequest{OrgSlug: orgSlug, RepoSlug: repoSlug},
	)
	if err != nil {
		return nil, rpcError("get score history", err)
	}

	points := make([]domain.HistoryPoint, 0, len(resp.GetPoints()))

	for _, point := range resp.GetPoints() {
		categories := make([]domain.PublicCategory, 0, len(point.GetCategories()))
		for _, category := range point.GetCategories() {
			categories = append(categories, domain.PublicCategory{ID: category.GetId(), Score: category.Score})
		}

		points = append(points, domain.HistoryPoint{
			GradedAt:      point.GetGradedAt().AsTime(),
			Score:         point.GetScore(),
			Categories:    categories,
			LowConfidence: point.LowConfidence,
		})
	}

	return points, nil
}

// GetRegradeStatus reports the scheduled public regrade (on or off, next run, progress), behind
// the public API's per-IP limit. It only reads.
func (s *GatewayService) GetRegradeStatus(ctx context.Context, clientIP string) (domain.RegradeStatus, error) {
	if err := s.allowPublicAPI(ctx, clientIP); err != nil {
		return domain.RegradeStatus{}, err
	}

	rpcCtx, cancel := context.WithTimeout(ctx, s.aggregatorTimeout)
	defer cancel()

	resp, err := s.aggregatorClient.GetRegradeStatus(rpcCtx, &agg.GetRegradeStatusRequest{})
	if err != nil {
		return domain.RegradeStatus{}, rpcError("get regrade status", err)
	}

	status := domain.RegradeStatus{
		Enabled:         resp.GetEnabled(),
		IntervalSeconds: resp.GetIntervalSeconds(),
		Running:         resp.GetRunning(),
		Total:           resp.GetTotal(),
		Done:            resp.GetDone(),
	}

	if resp.GetNextRunAt() != nil {
		next := resp.GetNextRunAt().AsTime()
		status.NextRunAt = &next
	}

	if resp.GetStartedAt() != nil {
		started := resp.GetStartedAt().AsTime()
		status.StartedAt = &started
	}

	if resp.GetLastFinishedAt() != nil {
		finished := resp.GetLastFinishedAt().AsTime()
		status.LastFinishedAt = &finished
	}

	return status, nil
}

// GetPositionExplanation explains a public repo's place in the default ranking, behind the public
// API's per-IP limit. It only reads.
func (s *GatewayService) GetPositionExplanation(
	ctx context.Context,
	clientIP string,
	orgSlug string,
	repoSlug string,
) (domain.PositionExplanation, error) {
	if err := s.allowPublicAPI(ctx, clientIP); err != nil {
		return domain.PositionExplanation{}, err
	}

	rpcCtx, cancel := context.WithTimeout(ctx, s.ratingTimeout)
	defer cancel()

	resp, err := s.ratingClient.GetPositionExplanation(
		rpcCtx,
		&rank.GetPositionExplanationRequest{OrgSlug: orgSlug, RepoSlug: repoSlug},
	)
	if err != nil {
		return domain.PositionExplanation{}, rpcError("get position explanation", err)
	}

	result := domain.PositionExplanation{Ranked: resp.GetRanked()}
	if !result.Ranked {
		return result, nil
	}

	result.Place, result.Total, result.Score = resp.GetPlace(), resp.GetTotal(), resp.GetScore()
	result.Above, result.Below = mapNeighbour(resp.GetAbove()), mapNeighbour(resp.GetBelow())
	result.WeakestCategory = resp.GetWeakestCategory()

	result.Categories = make([]domain.CategoryComparison, 0, len(resp.GetCategories()))
	for _, category := range resp.GetCategories() {
		result.Categories = append(result.Categories, domain.CategoryComparison{
			ID:          category.GetId(),
			Score:       category.GetScore(),
			Top10Median: category.Top10Median,
			Gap:         category.Gap,
		})
	}

	return result, nil
}

func mapNeighbour(n *rank.Neighbour) *domain.RankNeighbour {
	if n == nil {
		return nil
	}

	return &domain.RankNeighbour{
		Place:   n.GetPlace(),
		OrgSlug: n.GetOrgSlug(),
		Name:    n.GetName(),
		Score:   n.GetScore(),
		Points:  n.GetPoints(),
	}
}

// mapConfidence builds the flag from the wire fields; an unset low means it is unknown.
func mapConfidence(low *bool, reasons []*rank.ConfidenceReason) *domain.Confidence {
	if low == nil {
		return nil
	}

	confidence := &domain.Confidence{Low: *low, Reasons: make([]domain.ConfidenceReason, 0, len(reasons))}
	for _, reason := range reasons {
		confidence.Reasons = append(confidence.Reasons, domain.ConfidenceReason{Code: reason.GetCode(), Text: reason.GetText()})
	}

	return confidence
}

func mapPublicSummary(resp *rank.GetPublicSummaryResponse) domain.PublicSummary {
	summary := domain.PublicSummary{
		Name:        resp.GetName(),
		WebURL:      resp.GetWebUrl(),
		Score:       resp.PublicScore,
		Categories:  make([]domain.PublicCategory, 0, len(resp.GetCategories())),
		MissingData: append([]string(nil), resp.GetMissingData()...),
	}

	for _, category := range resp.GetCategories() {
		summary.Categories = append(summary.Categories, domain.PublicCategory{ID: category.GetId(), Score: category.Score})
	}

	if resp.GetGradedAt() != nil {
		gradedAt := resp.GetGradedAt().AsTime()
		summary.GradedAt = &gradedAt
	}

	summary.Confidence = mapConfidence(resp.LowConfidence, resp.GetConfidenceReasons())

	return summary
}

func mapRankingSortBy(
	sortBy domain.RankingSortBy,
) rank.SortBy {
	switch sortBy {
	case domain.RankingSortByLastActivity:
		return rank.SortBy_SORT_BY_LAST_ACTIVITY

	case domain.RankingSortByLikes:
		return rank.SortBy_SORT_BY_LIKES

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
		len(resp.GetEntries()),
	)

	for _, entry := range resp.GetEntries() {
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
		TotalCount: resp.GetTotalCount(),
		Page:       resp.GetPage(),
		PageSize:   resp.GetPageSize(),

		DynamicsDays: resp.GetDynamicsDays(),
	}, nil
}

func mapRankingEntry(
	entry *rank.RankingEntry,
) (domain.RankingEntry, error) {
	sourcecraftUUID, err := uuid.Parse(
		entry.GetSourcecraftUuid(),
	)
	if err != nil {
		return domain.RankingEntry{}, fmt.Errorf(
			"%w: invalid sourcecraft uuid %q: %v",
			domain.ErrRequestFailed,
			entry.GetSourcecraftUuid(),
			err,
		)
	}

	result := domain.RankingEntry{
		Place:           entry.GetPlace(),
		SourcecraftUUID: sourcecraftUUID,
		Name:            entry.GetName(),
		WebURL:          entry.GetWebUrl(),
		Language: domain.PrimaryLanguage{
			Name:  entry.GetLanguage().GetName(),
			Color: entry.GetLanguage().GetColor(),
		},
	}

	if entry.PublicScore != nil {
		score := entry.GetPublicScore()
		result.PublicScore = &score
	}

	result.Confidence = mapConfidence(entry.LowConfidence, entry.GetConfidenceReasons())
	result.ScoreChange = entry.ScoreChange
	result.PlaceChange = entry.PlaceChange
	result.GradeCount = entry.GetGradeCount()

	if entry.Likes != nil {
		likes := entry.GetLikes()
		result.Likes = &likes
	}

	if entry.GetLastActivityAt() != nil {
		if err := entry.GetLastActivityAt().CheckValid(); err != nil {
			return domain.RankingEntry{}, fmt.Errorf(
				"%w: invalid last_activity_at: %v",
				domain.ErrRequestFailed,
				err,
			)
		}

		lastActivityAt := entry.GetLastActivityAt().AsTime()
		result.LastActivityAt = &lastActivityAt
	}

	return result, nil
}

func (s *GatewayService) LoginWithYandex(
	ctx context.Context,
	clientIP string,
	request domain.YandexLoginDTO,
) (domain.LoginResponse, error) {
	if err := s.checkRateLimit(ctx, clientIP); err != nil {
		return domain.LoginResponse{}, err
	}

	if strings.TrimSpace(request.OAuthToken) == "" {
		return domain.LoginResponse{}, fmt.Errorf(
			"%w: oauth token is required",
			domain.ErrInvalidRequest,
		)
	}

	yandexCtx, cancel := context.WithTimeout(ctx, s.yandexTimeout)
	defer cancel()

	yandexUser, err := s.yandexClient.GetUser(
		yandexCtx,
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

package service

import (
	"aggregator-service/internal/domain"
	"aggregator-service/internal/transport/rpc/gen/client"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// DefaultLLMRequestTTL is how long a published LLM request suppresses publishing another one for
// the same repo view; the LLM service's own model budget is 60 seconds.
const DefaultLLMRequestTTL = 2 * time.Minute

type AggregatorService struct {
	ReposRepo        domain.RepoRepository
	GradesRepo       domain.GradesRepository
	LLMRepo          domain.LLMRepository
	ticker           *time.Ticker
	clientRPC        client.CodeHealthClient
	gradingPublisher domain.GradingPublisher
	llmPublisher     domain.LLMRecommendationsPublisher
	RPCTimeout       time.Duration
	// Token is the service's own SourceCraft PAT: used for wrapper lookups when the caller has
	// no token and for the public regrade. It never grants access to a private grade.
	Token string
	// RegradeLimit caps each public regrade to the top N public repos by SourceCraft rating;
	// 0 means every listed repo.
	RegradeLimit int
	// RegradeListTimeout bounds the full public repo listing, which pages through all of
	// SourceCraft and outlasts RPCTimeout; 0 falls back to RPCTimeout.
	RegradeListTimeout time.Duration
	// LLMRequestTTL overrides DefaultLLMRequestTTL (config LLM_REQUEST_TTL).
	LLMRequestTTL time.Duration
	// GradeLimiter caps new gradings started by users across the service; the scheduled public
	// regrade is not counted. nil means no limit.
	GradeLimiter *GradeLimiter

	llmMu       sync.Mutex
	llmRequests map[string]llmRequest

	failureMu       sync.Mutex
	gradingFailures map[string]time.Time
	now             func() time.Time
}

// llmRequest tracks one in-flight LLM request per repo view, keyed by llmKey.
type llmRequest struct {
	requestedAt time.Time
	failure     string
}

func NewAggregatorService(reposRepo domain.RepoRepository, gradesRepo domain.GradesRepository, llmRepo domain.LLMRepository,
	clientRPC client.CodeHealthClient, gradingPublisher domain.GradingPublisher, llmPublisher domain.LLMRecommendationsPublisher,
	rpcTimeout time.Duration, token string) *AggregatorService {
	return &AggregatorService{
		ReposRepo:        reposRepo,
		GradesRepo:       gradesRepo,
		LLMRepo:          llmRepo,
		clientRPC:        clientRPC,
		gradingPublisher: gradingPublisher,
		llmPublisher:     llmPublisher,
		RPCTimeout:       rpcTimeout,
		Token:            token,
		LLMRequestTTL:    DefaultLLMRequestTTL,
		llmRequests:      map[string]llmRequest{},
		gradingFailures:  map[string]time.Time{},
		now:              time.Now,
	}
}

func (as *AggregatorService) SaveLLMRecommendations(ctx context.Context, event domain.LLMRecommendationsReceivedEvent) error {
	key := llmKey(event.SourcecraftUUID, event.Visibility)

	if event.Error != "" {
		as.llmFailed(key, event.Error)

		return fmt.Errorf(
			"%w: %v",
			domain.ErrLLMUnavailable,
			event.Error,
		)
	}

	switch event.Visibility {
	case domain.VisibilityPublic:
		if err := as.LLMRepo.UpsertPublicResponse(ctx, event); err != nil {
			return fmt.Errorf(
				"%w: save public llm recommendations: %v",
				domain.ErrLLMUnavailable,
				err,
			)
		}

	case domain.VisibilityPrivate, "internal":
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

	as.llmDone(key)

	return nil
}

// RequestLLMRecommendations returns the stored LLM response when it describes the current grade
// (same graded_at). Otherwise it asks for one (or for a grading first, when the grade is
// missing or outdated) and reports GradingPending.
func (as *AggregatorService) RequestLLMRecommendations(
	ctx context.Context,
	orgSlug string,
	repoSlug string,
) (domain.RepoLLMResponse, error) {
	repo, private, err := as.resolveView(ctx, orgSlug, repoSlug)
	if err != nil {
		return domain.RepoLLMResponse{}, err
	}

	grade, err := as.loadGrade(ctx, repo.SourcecraftUUID, private)
	if err != nil && !errors.Is(err, domain.ErrGradeNotFound) {
		return domain.RepoLLMResponse{}, fmt.Errorf(
			"%w: get grade: %v",
			domain.ErrLLMUnavailable,
			err,
		)
	}

	if err != nil || needsGrading(repo, grade) {
		return domain.RepoLLMResponse{}, as.requestGrading(ctx, repo, private, true)
	}

	response, err := as.loadLLMResponse(ctx, repo.SourcecraftUUID, private)
	if err != nil && !errors.Is(err, domain.ErrLLMResponseNotFound) {
		return domain.RepoLLMResponse{}, fmt.Errorf(
			"%w: get llm response: %v",
			domain.ErrLLMUnavailable,
			err,
		)
	}

	if err == nil && llmResponseIsCurrent(response.GradedAt, grade.GradedAt) {
		return response, nil
	}

	visibility := domain.VisibilityPublic
	if private {
		visibility = domain.VisibilityPrivate
	}

	key := llmKey(repo.SourcecraftUUID, visibility)

	if failure := as.takeLLMFailure(key); failure != "" {
		return domain.RepoLLMResponse{}, fmt.Errorf(
			"%w: %s",
			domain.ErrRecommendationsFailed,
			failure,
		)
	}

	if !as.markLLMRequested(key) {
		return domain.RepoLLMResponse{}, domain.GradingPending
	}

	llmGrade, err := llmGradeFromRepoGrade(grade)
	if err != nil {
		as.llmDone(key)

		return domain.RepoLLMResponse{}, fmt.Errorf(
			"%w: %v",
			domain.ErrRecommendationsFailed,
			err,
		)
	}

	event := domain.LLMRecommendationsRequestedEvent{
		SourcecraftUUID: repo.SourcecraftUUID,
		OrgSlug:         repo.OrgSlug,
		RepoSlug:        repo.RepoSlug,
		Grade:           llmGrade,
		RequestedAt:     as.now(),
		Visibility:      visibility,
	}

	if err := as.llmPublisher.Publish(ctx, event); err != nil {
		as.llmDone(key)

		return domain.RepoLLMResponse{}, fmt.Errorf(
			"%w: publish llm recommendations request: %v",
			domain.ErrLLMUnavailable,
			err,
		)
	}

	return domain.RepoLLMResponse{}, domain.GradingPending
}

// llmResponseIsCurrent reports whether a stored LLM response describes the current grade: the
// graded_at it echoed must equal the grade's graded_at exactly. Comparing graded_at values, both
// set by grade-service, avoids trusting the LLM service's clock; a response for an earlier grade
// that arrives after a regrade is never taken as current. A response without graded_at (stored
// before it was recorded) is not current.
func llmResponseIsCurrent(responseGradedAt *time.Time, gradedAt *time.Time) bool {
	if responseGradedAt == nil || gradedAt == nil {
		return false
	}

	return responseGradedAt.Equal(*gradedAt)
}

func (as *AggregatorService) GetGrading(
	ctx context.Context,
	orgSlug string,
	repoSlug string,
) (*domain.RepoGrade, error) {
	repo, private, err := as.resolveView(ctx, orgSlug, repoSlug)
	if err != nil {
		return nil, err
	}

	grade, err := as.loadGrade(ctx, repo.SourcecraftUUID, private)
	if err != nil && !errors.Is(err, domain.ErrGradeNotFound) {
		return nil, fmt.Errorf(
			"%w: get grade: %v",
			domain.ErrGradingFailed,
			err,
		)
	}

	hasGrade := err == nil
	if hasGrade && !needsGrading(repo, grade) {
		return &grade, nil
	}

	// The previous grading failed: say so once instead of silently starting another one.
	if as.takeGradingFailure(llmKey(repo.SourcecraftUUID, viewVisibility(private))) {
		if hasGrade {
			return &grade, nil
		}

		return nil, domain.ErrGradingInterrupted
	}

	requestErr := as.requestGrading(ctx, repo, private, true)
	// Over the grading limit, an outdated grade is still better than none.
	if hasGrade && errors.Is(requestErr, domain.ErrGradingRateLimited) {
		return &grade, nil
	}

	return nil, requestErr
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

// resolveView looks the repo up and decides which grade the caller may see: private when the
// caller's own token can see the repo, public otherwise. A non-public repo without that access
// is refused, since only a private grading could cover it.
func (as *AggregatorService) resolveView(ctx context.Context, orgSlug, repoSlug string) (domain.Repo, bool, error) {
	repo, err := as.resolveRepo(ctx, orgSlug, repoSlug)
	if err != nil {
		return domain.Repo{}, false, err
	}

	private := as.hasPrivateAccess(ctx, repo)
	if !private && repo.Visibility != domain.VisibilityPublic {
		return domain.Repo{}, false, domain.ErrNoAccess
	}

	return repo, private, nil
}

// resolveRepo fetches the repo's current metadata from the wrapper and stores it in repos.
func (as *AggregatorService) resolveRepo(ctx context.Context, orgSlug, repoSlug string) (domain.Repo, error) {
	cont, cancel := context.WithTimeout(as.wrapperContext(ctx), as.RPCTimeout)
	defer cancel()

	response, err := as.clientRPC.GetRepositoryBySlugs(
		cont,
		&client.GetRepositoryBySlugsRequest{
			OrgSlug:  orgSlug,
			RepoSlug: repoSlug,
		},
	)
	if err != nil {
		return domain.Repo{}, wrapperError("get repository", err)
	}

	repo, err := repoFromWrapper(response)
	if err != nil {
		return domain.Repo{}, err
	}

	if err := as.upsertRepo(ctx, repo); err != nil {
		return domain.Repo{}, fmt.Errorf(
			"%w: save repository: %v",
			domain.ErrGradingFailed,
			err,
		)
	}

	return repo, nil
}

func (as *AggregatorService) upsertRepo(ctx context.Context, repo domain.Repo) error {
	_, err := as.ReposRepo.UpdateRepository(ctx, repo)
	if !errors.Is(err, domain.ErrRepositoryNotFound) {
		return err
	}

	_, err = as.ReposRepo.CreateRepository(ctx, repo)
	if errors.Is(err, domain.ErrRepositoryExists) {
		// Created concurrently by another request.
		_, err = as.ReposRepo.UpdateRepository(ctx, repo)
	}

	return err
}

func (as *AggregatorService) loadGrade(ctx context.Context, repoID uuid.UUID, private bool) (domain.RepoGrade, error) {
	if private {
		return as.GradesRepo.GetPrivateGradeByRepoID(ctx, repoID)
	}

	return as.GradesRepo.GetPublicGradeByRepoID(ctx, repoID)
}

func (as *AggregatorService) loadLLMResponse(ctx context.Context, repoID uuid.UUID, private bool) (domain.RepoLLMResponse, error) {
	if private {
		resp, err := as.LLMRepo.GetPrivateResponseByRepoID(ctx, repoID)

		return domain.RepoLLMResponse(resp), err
	}

	resp, err := as.LLMRepo.GetPublicResponseByRepoID(ctx, repoID)

	return domain.RepoLLMResponse(resp), err
}

// requestGrading takes the repo lock and publishes grading.requests. It returns GradingPending
// both when it published and when the lock was already taken by a grading in flight.
// requestGrading starts a grading unless one is already running for repo. Starts by users
// (userInitiated) count toward GradeLimiter; a request that joins a running grading does not.
func (as *AggregatorService) requestGrading(ctx context.Context, repo domain.Repo, private bool, userInitiated bool) error {
	acquired, err := as.ReposRepo.AcquireRepositoryLock(ctx, repo.SourcecraftUUID)
	if err != nil {
		return fmt.Errorf(
			"%w: acquire repository lock: %v",
			domain.ErrGradingFailed,
			err,
		)
	}

	if !acquired {
		return domain.GradingPending
	}

	reservedAt := as.now()
	if userInitiated {
		if ok, retryAfter := as.GradeLimiter.Reserve(reservedAt); !ok {
			_ = as.ReposRepo.ReleaseRepositoryLock(ctx, repo.SourcecraftUUID)

			return domain.RateLimitedError{RetryAfter: retryAfter}
		}
	}

	event := domain.GradingRequestedEvent{
		OrgSlug:     repo.OrgSlug,
		RepoSlug:    repo.RepoSlug,
		Visibility:  repo.Visibility,
		RequestedAt: as.now(),
	}

	if private {
		event.Token = tokenFromContext(ctx)
	}

	if err := as.gradingPublisher.Publish(ctx, event); err != nil {
		_ = as.ReposRepo.ReleaseRepositoryLock(ctx, repo.SourcecraftUUID)
		if userInitiated {
			as.GradeLimiter.Cancel(reservedAt)
		}

		return fmt.Errorf(
			"%w: publish grading request: %v",
			domain.ErrGradingFailed,
			err,
		)
	}

	return domain.GradingPending
}

func (as *AggregatorService) GetMyRepos(
	ctx context.Context,
) (domain.MyRepositoriesResponse, error) {
	token := tokenFromContext(ctx)
	if token == "" {
		return domain.MyRepositoriesResponse{}, domain.ErrUnauthenticated
	}

	cont, cancel := context.WithTimeout(withToken(ctx, token), as.RPCTimeout)
	defer cancel()

	response, err := as.clientRPC.GetMyRepos(
		cont,
		&client.GetMyReposRequest{},
	)
	if err != nil {
		return domain.MyRepositoriesResponse{}, wrapperError("get my repositories", err)
	}

	repositories := make([]domain.Repo, 0, len(response.GetRepositories()))

	for _, pbRepo := range response.GetRepositories() {
		repo, err := repoFromWrapper(pbRepo)
		if err != nil {
			return domain.MyRepositoriesResponse{}, err
		}

		repositories = append(repositories, repo)
	}

	return domain.MyRepositoriesResponse{
		Repositories:  repositories,
		NextPageToken: response.GetNextPageToken(),
	}, nil
}

// StartPublicGrading regrades public repos once at startup and then every interval.
// StartPublicGrading regrades public repos every interval; onStart also runs one regrade
// immediately instead of waiting for the first tick.
func (as *AggregatorService) StartPublicGrading(ctx context.Context, interval time.Duration, onStart bool) {
	as.ticker = time.NewTicker(interval)

	go func() {
		defer as.ticker.Stop()

		if onStart {
			as.runPublicRegrade(ctx)
		}

		for {
			select {
			case <-ctx.Done():
				return

			case <-as.ticker.C:
				as.runPublicRegrade(ctx)
			}
		}
	}()
}

func (as *AggregatorService) runPublicRegrade(ctx context.Context) {
	if err := as.regradePublicRepos(ctx); err != nil {
		log.Println("error during regrading: ", err.Error())
	}
}

// SaveGradingResult stores a grading.results message and always releases the repo lock, so a
// failed save can't leave the repo locked.
func (as *AggregatorService) SaveGradingResult(ctx context.Context,
	grade domain.RepoGrade, gradeMode string, errorCode string) error {
	defer func() {
		if err := as.ReposRepo.ReleaseRepositoryLock(ctx, grade.SourcecraftUUID); err != nil {
			log.Printf("release repository lock %s: %v", grade.SourcecraftUUID, err)
		}
	}()

	if errorCode != "" {
		as.gradingFailed(llmKey(grade.SourcecraftUUID, gradeMode))

		return fmt.Errorf(
			"%w: %v",
			domain.ErrGradingFailed,
			errorCode,
		)
	}

	normalizeGrade(&grade)

	switch gradeMode {
	case domain.VisibilityPublic:
		if err := as.GradesRepo.UpsertPublicGrade(ctx, grade); err != nil {
			return fmt.Errorf(
				"%w: save public grade: %v",
				domain.ErrGradingFailed,
				err,
			)
		}

	case domain.VisibilityPrivate:
		if err := as.GradesRepo.UpsertPrivateGrade(ctx, grade); err != nil {
			return fmt.Errorf(
				"%w: save private grade: %v",
				domain.ErrGradingFailed,
				err,
			)
		}

	default:
		return fmt.Errorf(
			"%w: unknown grade mode %q",
			domain.ErrInvalidGrade,
			gradeMode,
		)
	}

	return nil
}

// normalizeGrade replaces JSON nulls and nil slices with empty values: the grade columns are
// NOT NULL, and pgx writes a nil slice as NULL.
func normalizeGrade(grade *domain.RepoGrade) {
	if grade.MissingData == nil {
		grade.MissingData = []string{}
	}

	if isJSONEmpty(grade.CategoryScores) {
		grade.CategoryScores = json.RawMessage(`{}`)
	}

	if isJSONEmpty(grade.Advice) {
		grade.Advice = json.RawMessage(`[]`)
	}
}

func isJSONEmpty(value json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(value))

	return trimmed == "" || trimmed == "null"
}

func (as *AggregatorService) regradePublicRepos(ctx context.Context) error {
	if as.Token == "" {
		log.Println("SERVICE_TOKEN is not set: skipping public regrade")

		return nil
	}

	listTimeout := as.RPCTimeout
	if as.RegradeListTimeout > 0 {
		listTimeout = as.RegradeListTimeout
	}

	cont, cancel := context.WithTimeout(withToken(ctx, as.Token), listTimeout)
	defer cancel()

	response, err := as.clientRPC.ListAllPublicRepos(
		cont,
		&client.ListAllPublicReposRequest{},
	)
	if err != nil {
		return wrapperError("list public repositories", err)
	}

	repos := topPublicRepos(response.GetRepos(), as.RegradeLimit)
	failed := 0

	for _, pbRepo := range repos {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err := as.regradeRepo(ctx, pbRepo); err != nil {
			failed++
			log.Printf("regrade %s/%s: %v", pbRepo.GetOrganization().GetSlug(), pbRepo.GetSlug(), err)
		}
	}

	if failed > 0 {
		return fmt.Errorf(
			"%w: %d of %d public repositories failed",
			domain.ErrGradingFailed,
			failed,
			len(repos),
		)
	}

	return nil
}

// topPublicRepos keeps the public repos, highest SourceCraft rating first (listing order breaks
// ties), cut to limit when limit > 0.
func topPublicRepos(repos []*client.Repository, limit int) []*client.Repository {
	public := make([]*client.Repository, 0, len(repos))
	for _, r := range repos {
		if r.GetVisibility() == domain.VisibilityPublic {
			public = append(public, r)
		}
	}

	sort.SliceStable(public, func(i, j int) bool {
		return public[i].GetRating().GetValue() > public[j].GetRating().GetValue()
	})

	if limit > 0 && len(public) > limit {
		public = public[:limit]
	}

	return public
}

func (as *AggregatorService) regradeRepo(ctx context.Context, pbRepo *client.Repository) error {
	repo, err := repoFromWrapper(pbRepo)
	if err != nil {
		return err
	}

	if repo.Visibility != domain.VisibilityPublic {
		return nil
	}

	if err := as.upsertRepo(ctx, repo); err != nil {
		return fmt.Errorf("save repository: %w", err)
	}

	grade, err := as.GradesRepo.GetPublicGradeByRepoID(ctx, repo.SourcecraftUUID)
	if err != nil && !errors.Is(err, domain.ErrGradeNotFound) {
		return fmt.Errorf("get grade: %w", err)
	}

	if err == nil && !needsGrading(repo, grade) {
		return nil
	}

	if err := as.requestGrading(ctx, repo, false, false); !errors.Is(err, domain.GradingPending) {
		return err
	}

	return nil
}

// wrapperRepo is what the wrapper's repository messages have in common.
type wrapperRepo interface {
	GetId() string
	GetOrganization() *client.Organization
	GetSlug() string
	GetVisibility() string
	GetLastActivityAt() string
	GetLanguage() *client.Language
	GetRating() *client.Rating
	GetWebUrl() string
}

func repoFromWrapper(r wrapperRepo) (domain.Repo, error) {
	repoID, err := uuid.Parse(r.GetId())
	if err != nil {
		return domain.Repo{}, fmt.Errorf(
			"%w: invalid repository UUID %q: %v",
			domain.ErrInvalidRepository,
			r.GetId(),
			err,
		)
	}

	var lastActivityAt *time.Time

	if value := r.GetLastActivityAt(); value != "" {
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return domain.Repo{}, fmt.Errorf(
				"%w: invalid last_activity_at %q: %v",
				domain.ErrInvalidRepository,
				value,
				err,
			)
		}

		lastActivityAt = &parsed
	}

	return domain.Repo{
		SourcecraftUUID: repoID,
		OrgSlug:         r.GetOrganization().GetSlug(),
		RepoSlug:        r.GetSlug(),
		Visibility:      r.GetVisibility(),
		LastActivityAt:  lastActivityAt,
		Language: domain.PrimaryLanguage{
			Name:  r.GetLanguage().GetName(),
			Color: r.GetLanguage().GetColor(),
		},
		RatingValue: r.GetRating().GetValue(),
		WebURL:      r.GetWebUrl(),
	}, nil
}

// llmGradeFromRepoGrade converts a stored grade (grade-service's category objects and
// recommendation objects) into the LLM request shape.
func llmGradeFromRepoGrade(grade domain.RepoGrade) (domain.LLMGrade, error) {
	var categories map[string]struct {
		NoData bool    `json:"no_data"`
		Score  float64 `json:"score"`
	}

	if !isJSONEmpty(grade.CategoryScores) {
		if err := json.Unmarshal(grade.CategoryScores, &categories); err != nil {
			return domain.LLMGrade{}, fmt.Errorf("decode category scores: %w", err)
		}
	}

	scores := make(map[string]*float64, len(categories))
	for id, category := range categories {
		if category.NoData {
			scores[id] = nil
			continue
		}

		score := category.Score
		scores[id] = &score
	}

	var recommendations []struct {
		CategoryID string `json:"category_id"`
		Priority   string `json:"priority"`
		Text       string `json:"text"`
	}

	if !isJSONEmpty(grade.Advice) {
		if err := json.Unmarshal(grade.Advice, &recommendations); err != nil {
			return domain.LLMGrade{}, fmt.Errorf("decode advice: %w", err)
		}
	}

	advice := make([]domain.LLMAdvice, 0, len(recommendations))
	for _, recommendation := range recommendations {
		priority, text := splitPriority(recommendation.Priority, recommendation.Text)
		if text != "" {
			advice = append(advice, domain.LLMAdvice{CategoryID: recommendation.CategoryID, Priority: priority, Text: text})
		}
	}

	missingData := grade.MissingData
	if missingData == nil {
		missingData = []string{}
	}

	return domain.LLMGrade{
		Score:          grade.Score,
		CategoryScores: scores,
		Advice:         advice,
		MissingData:    missingData,
		GradedAt:       grade.GradedAt,
	}, nil
}

// recommendationPriorities are the valid recommendation priorities.
var recommendationPriorities = []string{"critical", "high", "medium", "low"}

// defaultRecommendationPriority is used when a stored recommendation has no recognizable priority.
const defaultRecommendationPriority = "medium"

// splitPriority returns a recommendation's priority and its text without the transitional
// "[priority] " tag. The priority field wins; a grade stored before it existed falls back to the
// tag, and to medium when neither is valid.
func splitPriority(priority, text string) (string, string) {
	text = strings.TrimSpace(text)
	tagged := ""
	for _, p := range recommendationPriorities {
		if rest, ok := strings.CutPrefix(text, "["+p+"]"); ok {
			tagged, text = p, strings.TrimSpace(rest)
			break
		}
	}

	switch {
	case slices.Contains(recommendationPriorities, priority):
		return priority, text
	case tagged != "":
		return tagged, text
	default:
		return defaultRecommendationPriority, text
	}
}

func llmKey(repoID uuid.UUID, visibility string) string {
	return repoID.String() + "/" + visibility
}

// markLLMRequested records a request for key and reports whether the caller should publish it;
// false means one is already in flight.
func (as *AggregatorService) markLLMRequested(key string) bool {
	as.llmMu.Lock()
	defer as.llmMu.Unlock()

	now := as.now()
	if request, ok := as.llmRequests[key]; ok && request.failure == "" && now.Sub(request.requestedAt) < as.LLMRequestTTL {
		return false
	}

	as.llmRequests[key] = llmRequest{requestedAt: now}

	return true
}

func (as *AggregatorService) llmDone(key string) {
	as.llmMu.Lock()
	defer as.llmMu.Unlock()

	delete(as.llmRequests, key)
}

func (as *AggregatorService) llmFailed(key, code string) {
	as.llmMu.Lock()
	defer as.llmMu.Unlock()

	if _, ok := as.llmRequests[key]; ok {
		as.llmRequests[key] = llmRequest{requestedAt: as.now(), failure: code}
	}
}

// takeLLMFailure returns and clears a failure reported for key, so the caller sees it once and
// the next call asks again.
func (as *AggregatorService) takeLLMFailure(key string) string {
	as.llmMu.Lock()
	defer as.llmMu.Unlock()

	request, ok := as.llmRequests[key]
	if !ok || request.failure == "" {
		return ""
	}

	delete(as.llmRequests, key)

	return request.failure
}

// gradingFailureTTL is how long a failed grading waits to be reported before it is forgotten.
const gradingFailureTTL = 30 * time.Minute

func viewVisibility(private bool) string {
	if private {
		return domain.VisibilityPrivate
	}

	return domain.VisibilityPublic
}

func (as *AggregatorService) gradingFailed(key string) {
	as.failureMu.Lock()
	defer as.failureMu.Unlock()

	as.gradingFailures[key] = as.now()
}

// takeGradingFailure reports and clears a recent failed grading for key.
func (as *AggregatorService) takeGradingFailure(key string) bool {
	as.failureMu.Lock()
	defer as.failureMu.Unlock()

	failedAt, ok := as.gradingFailures[key]
	if !ok {
		return false
	}

	delete(as.gradingFailures, key)

	return as.now().Sub(failedAt) < gradingFailureTTL
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
	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimPrefix(token, "bearer ")

	return strings.TrimSpace(token)
}

func withToken(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}

// wrapperContext authorizes a wrapper lookup with the caller's token, or the service token when
// the caller has none.
func (as *AggregatorService) wrapperContext(ctx context.Context) context.Context {
	token := tokenFromContext(ctx)
	if token == "" {
		token = as.Token
	}

	if token == "" {
		return ctx
	}

	return withToken(ctx, token)
}

// hasPrivateAccess reports whether the caller's own token can see the repo. The service token
// is never used here.
func (as *AggregatorService) hasPrivateAccess(ctx context.Context, repo domain.Repo) bool {
	token := tokenFromContext(ctx)
	if token == "" {
		return false
	}

	cont, cancel := context.WithTimeout(withToken(ctx, token), as.RPCTimeout)
	defer cancel()

	response, err := as.clientRPC.GetMyRepos(
		cont,
		&client.GetMyReposRequest{},
	)
	if err != nil {
		return false
	}

	for _, r := range response.GetRepositories() {
		if r.GetSlug() == repo.RepoSlug && r.GetOrganization().GetSlug() == repo.OrgSlug {
			return true
		}
	}

	return false
}

func wrapperError(op string, err error) error {
	switch status.Code(err) {
	case codes.DeadlineExceeded:
		return fmt.Errorf("%w: %s: %v", domain.ErrTimeout, op, err)

	case codes.PermissionDenied:
		return domain.ErrNoAccess

	case codes.NotFound:
		return domain.ErrRepositoryNotFound

	case codes.Unauthenticated:
		return domain.ErrUnauthenticated

	case codes.Unavailable:
		return fmt.Errorf("%w: %s: %v", domain.ErrSourceCraftUnavailable, op, err)

	default:
		return fmt.Errorf("%w: %s: %v", domain.ErrExternalAPI, op, err)
	}
}

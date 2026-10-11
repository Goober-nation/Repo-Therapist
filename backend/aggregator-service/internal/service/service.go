package service

import (
	"aggregator-service/internal/domain"
	"aggregator-service/internal/transport/rpc/gen/client"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"slices"
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

// DefaultGradingTokenTTL matches the default GRADING_LOCK_TIMEOUT.
const DefaultGradingTokenTTL = 20 * time.Minute

// Recorder counts what the service does, for monitoring (docs/metrics.md). Visibility is public or
// private; labels are fixed words, never slugs or users. A nil Recorder counts nothing.
type Recorder interface {
	GradingRequested(visibility, trigger string)
	// GradingCompleted: duration is zero when the request was made before a restart.
	GradingCompleted(visibility, outcome string, duration time.Duration)
	GradeLimiterRejected()
	LLMRequest(outcome string)
	BatchItem(status string)
	BatchEvent(event string)
}

type AggregatorService struct {
	ReposRepo  domain.RepoRepository
	GradesRepo domain.GradesRepository
	LLMRepo    domain.LLMRepository
	// Metrics counts gradings, LLM requests and batches; nil counts nothing.
	Metrics Recorder
	// AuditRepo stores the audit log; nil leaves auditing off.
	AuditRepo domain.AuditRepository
	// BatchRepo stores admin grading batches; nil leaves them off.
	BatchRepo domain.BatchRepository
	// BatchConcurrency, BatchItemTimeout and BatchPoll tune the batch grading; zero
	// values mean the defaults in grading_batch.go.
	BatchConcurrency int
	BatchItemTimeout time.Duration
	BatchPoll        time.Duration
	// BatchMaxLimit caps the limit of one batch (config BATCH_GRADING_MAX_LIMIT); 0 means no cap.
	BatchMaxLimit int
	// Settings is the admin settings page (config/app.yaml overrides); nil leaves it off.
	Settings *Settings
	// CatalogRepo stores the public repo catalog; nil leaves it off and the live listings in use.
	CatalogRepo domain.CatalogRepository
	// CatalogMaxPages bounds one catalog refresh; 0 means DefaultCatalogMaxPages.
	CatalogMaxPages  int
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
	// LLMRequestTTL overrides DefaultLLMRequestTTL (config LLM_REQUEST_TTL).
	LLMRequestTTL time.Duration
	// GradeLimiter caps new gradings started by users across the service; the scheduled public
	// regrade is not counted. nil means no limit.
	GradeLimiter *GradeLimiter
	// RegradeMissingAnalytics makes a grade without a stored analytics row count as outdated, so
	// a repo graded before analytics existed is regraded even when it has no new activity. Turn
	// it off when grade-service has analytics disabled, or every request would start a regrade.
	RegradeMissingAnalytics bool
	// TokenStore receives the caller's PAT for a private grading, keyed by job id.
	TokenStore domain.GradingTokenStore
	// TokenTTL bounds how long a stored PAT lives; keep it equal to the repo lock timeout.
	// 0 falls back to DefaultGradingTokenTTL.
	TokenTTL time.Duration
	// AuditRetentionDays deletes audit entries older than this many days; 0 keeps them forever.
	AuditRetentionDays int

	llmMu       sync.Mutex
	llmRequests map[string]llmRequest

	failureMu       sync.Mutex
	gradingFailures map[string]time.Time
	now             func() time.Time

	// liveMu guards the fields config reloads and admin overrides change while the service runs
	// (UpdateLive); read them through the accessors in live.go.
	liveMu sync.RWMutex

	regradeMu    sync.Mutex
	regradeState regradeState

	batches  batchRunner
	batchCtx context.Context

	catalog    catalogRunner
	catalogCtx context.Context

	// gradingStarts remembers when each repo's grading was requested, to time it at the result.
	gradingStarts sync.Map

	// inflight tracks published gradings so background ones yield to user ones (priority.go).
	inflight flights
}

// regradeState is what the public regrade reports about itself; guarded by regradeMu.
type regradeState struct {
	enabled        bool
	interval       time.Duration
	nextRunAt      time.Time
	running        bool
	startedAt      time.Time
	lastFinishedAt time.Time
	total          int
	done           int
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
		as.countLLM("received_error")

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
	as.countLLM("received_ok")

	return nil
}

func (as *AggregatorService) countLLM(outcome string) {
	if as.Metrics != nil {
		as.Metrics.LLMRequest(outcome)
	}
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

	if err != nil || as.needsRegrade(ctx, repo, private, grade) {
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
		as.countLLM("publish_failed")

		return domain.RepoLLMResponse{}, fmt.Errorf(
			"%w: publish llm recommendations request: %v",
			domain.ErrLLMUnavailable,
			err,
		)
	}

	as.countLLM("requested")

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
	return as.getGrading(ctx, orgSlug, repoSlug, false)
}

// ForceGrading is GetGrading that starts a new grading even when the stored grade is current, and
// after a failed one. It still respects the grading limit and a grading already in progress.
func (as *AggregatorService) ForceGrading(
	ctx context.Context,
	orgSlug string,
	repoSlug string,
) (*domain.RepoGrade, error) {
	return as.getGrading(ctx, orgSlug, repoSlug, true)
}

func (as *AggregatorService) getGrading(
	ctx context.Context,
	orgSlug string,
	repoSlug string,
	force bool,
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
	if hasGrade && !force && !as.needsRegrade(ctx, repo, private, grade) {
		return &grade, nil
	}

	// The previous grading failed: say so once instead of silently starting another one. A forced
	// grading clears the failure and tries again.
	if as.takeGradingFailure(llmKey(repo.SourcecraftUUID, viewVisibility(private))) && !force {
		if hasGrade {
			return &grade, nil
		}

		return nil, domain.ErrGradingInterrupted
	}

	requestErr := as.requestGrading(ctx, repo, private, true)
	// Over the grading limit, an outdated grade is still better than none; a forced grading reports
	// the limit, since the caller asked for a new grade.
	if hasGrade && !force && errors.Is(requestErr, domain.ErrGradingRateLimited) {
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

// MinAnalyticsVersion is the oldest analytics schema version that counts as current: version 2
// added the pull request review block, version 3 the finer owner map and the review
// bottlenecks and version 4 the issue first-reply block, so an older stored object is regraded.
const MinAnalyticsVersion = 4

// needsRegrade is needsGrading plus the data check: a grade whose analytics row is missing, or
// stored with an older schema version, or (public view) whose facts row is missing, is outdated too. Only a definite answer counts; a failed
// lookup never starts a grading.
func (as *AggregatorService) needsRegrade(ctx context.Context, repo domain.Repo, private bool, grade domain.RepoGrade) bool {
	if needsGrading(repo, grade) {
		return true
	}

	if !as.regradeMissingAnalytics() {
		return false
	}

	var (
		analytics domain.RepoAnalytics
		err       error
	)
	if private {
		analytics, err = as.GradesRepo.GetPrivateAnalytics(ctx, repo.SourcecraftUUID)
	} else {
		analytics, err = as.GradesRepo.GetPublicAnalytics(ctx, repo.SourcecraftUUID)
	}

	if errors.Is(err, domain.ErrGradeNotFound) {
		return true
	}

	if err == nil && analyticsVersion(analytics.Analytics) < MinAnalyticsVersion {
		return true
	}

	// The ranking filters read the facts of a public grade; a grade stored before they existed has none.
	if !private {
		hasFacts, factsErr := as.GradesRepo.HasPublicFacts(ctx, repo.SourcecraftUUID)
		return factsErr == nil && !hasFacts
	}

	return false
}

// analyticsVersion reads the schema version of a stored analytics object; 0 when it has none.
func analyticsVersion(raw json.RawMessage) int {
	var head struct {
		Version int `json:"version"`
	}
	if json.Unmarshal(raw, &head) != nil {
		return 0
	}

	return head.Version
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
	cont, cancel := context.WithTimeout(as.wrapperContext(ctx), as.rpcTimeout())
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

type triggerKey struct{}

// withTrigger marks the gradings requested under ctx as started by trigger (batch), for the metrics.
func withTrigger(ctx context.Context, trigger string) context.Context {
	return context.WithValue(ctx, triggerKey{}, trigger)
}

func gradingTrigger(ctx context.Context, userInitiated bool) string {
	if trigger, ok := ctx.Value(triggerKey{}).(string); ok {
		return trigger
	}

	if userInitiated {
		return "user"
	}

	return "schedule"
}

func visibilityLabel(private bool) string {
	if private {
		return domain.VisibilityPrivate
	}

	return domain.VisibilityPublic
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
			if as.Metrics != nil {
				as.Metrics.GradeLimiterRejected()
			}

			return domain.RateLimitedError{RetryAfter: retryAfter}
		}
	}

	event := domain.GradingRequestedEvent{
		JobID:       uuid.NewString(),
		OrgSlug:     repo.OrgSlug,
		RepoSlug:    repo.RepoSlug,
		Visibility:  repo.Visibility,
		RequestedAt: as.now(),
	}

	abort := func() {
		_ = as.ReposRepo.ReleaseRepositoryLock(ctx, repo.SourcecraftUUID)
		if userInitiated {
			as.GradeLimiter.Cancel(reservedAt)
		}
	}

	storedToken := false
	if private {
		if err := as.storeGradingToken(ctx, event.JobID, tokenFromContext(ctx)); err != nil {
			abort()

			return fmt.Errorf(
				"%w: store grading token: %v",
				domain.ErrGradingFailed,
				err,
			)
		}
		storedToken = true
	}

	if err := as.gradingPublisher.Publish(ctx, event); err != nil {
		if storedToken {
			if err := as.TokenStore.DeleteToken(ctx, event.JobID); err != nil {
				log.Printf("deleting grading token for job %s: %v", event.JobID, err)
			}
		}
		abort()

		return fmt.Errorf(
			"%w: publish grading request: %v",
			domain.ErrGradingFailed,
			err,
		)
	}

	as.gradingStarts.Store(repo.SourcecraftUUID, as.now())

	trigger := gradingTrigger(ctx, userInitiated)
	as.trackFlight(repo.SourcecraftUUID, isBackgroundTrigger(trigger))

	if as.Metrics != nil {
		as.Metrics.GradingRequested(visibilityLabel(private), trigger)
	}

	return domain.GradingPending
}

// storeGradingToken puts a private grading's PAT in the token store under jobID.
func (as *AggregatorService) storeGradingToken(ctx context.Context, jobID, token string) error {
	if token == "" {
		return errors.New("no user token for private grading")
	}
	if as.TokenStore == nil {
		return errors.New("no grading token store configured")
	}
	ttl := as.tokenTTL()
	if ttl <= 0 {
		ttl = DefaultGradingTokenTTL
	}

	return as.TokenStore.PutToken(ctx, jobID, token, ttl)
}

func (as *AggregatorService) GetMyRepos(
	ctx context.Context,
) (domain.MyRepositoriesResponse, error) {
	token := tokenFromContext(ctx)
	if token == "" {
		return domain.MyRepositoriesResponse{}, domain.ErrUnauthenticated
	}

	cont, cancel := context.WithTimeout(withToken(ctx, token), as.rpcTimeout())
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

// StartPublicGrading regrades public repos every interval; onStart also runs one regrade
// immediately instead of waiting for the first tick. A non-positive interval, or a missing
// service token, leaves the scheduled regrade off (GetRegradeStatus then reports it disabled).
func (as *AggregatorService) StartPublicGrading(ctx context.Context, interval time.Duration, onStart bool) {
	if interval <= 0 || as.Token == "" {
		log.Println("scheduled public regrade is off (no interval or no SERVICE_TOKEN)")

		return
	}

	as.regradeMu.Lock()
	as.regradeState.enabled = true
	as.regradeState.interval = interval
	as.regradeState.nextRunAt = as.currentTime().Add(interval)
	// Under regradeMu: SetRegradeInterval resets this ticker from config changes.
	as.ticker = time.NewTicker(interval)
	as.regradeMu.Unlock()

	go func() {
		defer as.ticker.Stop()

		if onStart {
			as.runPublicRegrade(ctx)
		}

		for {
			select {
			case <-ctx.Done():
				return

			case tick := <-as.ticker.C:
				as.regradeMu.Lock()
				as.regradeState.nextRunAt = tick.Add(as.regradeState.interval)
				as.regradeMu.Unlock()

				as.runPublicRegrade(ctx)
			}
		}
	}()
}

func (as *AggregatorService) currentTime() time.Time {
	if as.now != nil {
		return as.now()
	}

	return time.Now()
}

func (as *AggregatorService) runPublicRegrade(ctx context.Context) {
	as.regradeMu.Lock()
	as.regradeState.running = true
	as.regradeState.startedAt = as.currentTime()
	as.regradeState.total, as.regradeState.done = 0, 0
	as.regradeMu.Unlock()

	defer func() {
		as.regradeMu.Lock()
		as.regradeState.running = false
		as.regradeState.lastFinishedAt = as.currentTime()
		as.regradeMu.Unlock()
	}()

	if err := as.regradePublicRepos(ctx); err != nil {
		log.Println("error during regrading: ", err.Error())
	}
}

// GetRegradeStatus reports the scheduled public regrade: whether it is on, when it runs next
// and, while it runs, how many repos it has gone through. It only reads in-memory state.
func (as *AggregatorService) GetRegradeStatus(_ context.Context) domain.RegradeStatus {
	as.regradeMu.Lock()
	defer as.regradeMu.Unlock()

	state := as.regradeState
	status := domain.RegradeStatus{
		Enabled:  state.enabled,
		Interval: state.interval,
		Running:  state.running,
		Total:    state.total,
		Done:     state.done,
	}

	if state.enabled {
		status.NextRunAt = &state.nextRunAt
	}

	if !state.startedAt.IsZero() {
		status.StartedAt = &state.startedAt
	}

	if !state.lastFinishedAt.IsZero() {
		status.LastFinishedAt = &state.lastFinishedAt
	}

	return status
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

	started, timed := as.gradingStarts.LoadAndDelete(grade.SourcecraftUUID)
	as.landFlight(grade.SourcecraftUUID)

	var elapsed time.Duration

	if at, ok := started.(time.Time); timed && ok {
		elapsed = as.now().Sub(at)
	}

	if as.Metrics != nil && (gradeMode == domain.VisibilityPublic || gradeMode == domain.VisibilityPrivate) {
		outcome := "ok"
		if errorCode != "" {
			outcome = "error"
		}

		as.Metrics.GradingCompleted(gradeMode, outcome, elapsed)
	}

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

		if err := as.savePublicConfidence(ctx, grade); err != nil {
			return err
		}

		as.saveAnalytics(ctx, grade, true)
		as.saveHistory(ctx, grade)
		as.saveFacts(ctx, grade)

	case domain.VisibilityPrivate:
		if err := as.GradesRepo.UpsertPrivateGrade(ctx, grade); err != nil {
			return fmt.Errorf(
				"%w: save private grade: %v",
				domain.ErrGradingFailed,
				err,
			)
		}

		as.saveAnalytics(ctx, grade, false)

	default:
		return fmt.Errorf(
			"%w: unknown grade mode %q",
			domain.ErrInvalidGrade,
			gradeMode,
		)
	}

	return nil
}

// savePublicConfidence stores the low-confidence flag next to a public grade, or removes a stale
// one when grade-service sent none (flag turned off). The flag is advisory: the grade itself is
// already saved, so a failure here is returned but never rolls the grade back.
func (as *AggregatorService) savePublicConfidence(ctx context.Context, grade domain.RepoGrade) error {
	if grade.Confidence == nil {
		if err := as.GradesRepo.DeletePublicConfidence(ctx, grade.SourcecraftUUID); err != nil {
			return fmt.Errorf("%w: delete public confidence: %v", domain.ErrGradingFailed, err)
		}

		return nil
	}

	if err := as.GradesRepo.UpsertPublicConfidence(ctx, grade.SourcecraftUUID, *grade.Confidence, grade.GradedAt); err != nil {
		return fmt.Errorf("%w: save public confidence: %v", domain.ErrGradingFailed, err)
	}

	return nil
}

// saveAnalytics stores the informational analytics next to a grade, or removes a stale row when
// grade-service sent none. The analytics are advisory and the grade is already saved, so a failure
// here is only logged and never fails the grading.
func (as *AggregatorService) saveAnalytics(ctx context.Context, grade domain.RepoGrade, public bool) {
	var err error

	switch {
	case isJSONEmpty(grade.Analytics) && public:
		err = as.GradesRepo.DeletePublicAnalytics(ctx, grade.SourcecraftUUID)
	case isJSONEmpty(grade.Analytics):
		err = as.GradesRepo.DeletePrivateAnalytics(ctx, grade.SourcecraftUUID)
	case public:
		err = as.GradesRepo.UpsertPublicAnalytics(ctx, grade.SourcecraftUUID, grade.Analytics, grade.GradedAt)
	default:
		err = as.GradesRepo.UpsertPrivateAnalytics(ctx, grade.SourcecraftUUID, grade.Analytics, grade.GradedAt)
	}

	if err != nil {
		log.Printf("save analytics of %s (public=%v): %v", grade.SourcecraftUUID, public, err)
	}
}

// saveHistory appends the public grade to the score history. The history is advisory and the
// grade is already saved, so a failure here is only logged and never fails the grading.
func (as *AggregatorService) saveHistory(ctx context.Context, grade domain.RepoGrade) {
	if err := as.GradesRepo.AppendPublicHistory(ctx, grade); err != nil {
		log.Printf("append score history of %s: %v", grade.SourcecraftUUID, err)
	}
}

// saveFacts stores the filterable facts next to a public grade, or removes a stale row when
// grade-service sent none or an unreadable object. The facts are advisory filters and the grade is
// already saved, so a failure here is only logged and never fails the grading.
func (as *AggregatorService) saveFacts(ctx context.Context, grade domain.RepoGrade) {
	var facts domain.RepoFacts

	if isJSONEmpty(grade.Facts) || json.Unmarshal(grade.Facts, &facts) != nil {
		if err := as.GradesRepo.DeletePublicFacts(ctx, grade.SourcecraftUUID); err != nil {
			log.Printf("delete facts of %s: %v", grade.SourcecraftUUID, err)
		}

		return
	}

	if err := as.GradesRepo.UpsertPublicFacts(ctx, grade.SourcecraftUUID, facts, grade.GradedAt); err != nil {
		log.Printf("save facts of %s: %v", grade.SourcecraftUUID, err)
	}
}

// GetAnalytics returns the stored analytics of the repo for the view the caller may see (private
// with the owner's token, public otherwise). It never starts a grading; Analytics is nil when the
// repo has none yet.
func (as *AggregatorService) GetAnalytics(ctx context.Context, orgSlug, repoSlug string) (domain.RepoAnalytics, error) {
	repo, private, err := as.resolveView(ctx, orgSlug, repoSlug)
	if err != nil {
		return domain.RepoAnalytics{}, err
	}

	var analytics domain.RepoAnalytics
	if private {
		analytics, err = as.GradesRepo.GetPrivateAnalytics(ctx, repo.SourcecraftUUID)
	} else {
		analytics, err = as.GradesRepo.GetPublicAnalytics(ctx, repo.SourcecraftUUID)
	}

	if errors.Is(err, domain.ErrGradeNotFound) {
		return domain.RepoAnalytics{SourcecraftUUID: repo.SourcecraftUUID}, nil
	}
	if err != nil {
		return domain.RepoAnalytics{}, fmt.Errorf("%w: get analytics: %v", domain.ErrGradingFailed, err)
	}

	return analytics, nil
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

	repos, failed, err := as.publicReposToRegrade(ctx)
	if err != nil {
		return err
	}

	listed := len(repos) + failed

	as.regradeMu.Lock()
	as.regradeState.total = len(repos)
	as.regradeMu.Unlock()

	for _, repo := range repos {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err := as.regradeRepo(ctx, repo); err != nil {
			failed++
			log.Printf("regrade %s/%s: %v", repo.OrgSlug, repo.RepoSlug, err)
		}

		as.regradeMu.Lock()
		as.regradeState.done++
		as.regradeMu.Unlock()
	}

	if failed > 0 {
		return fmt.Errorf(
			"%w: %d of %d public repositories failed",
			domain.ErrGradingFailed,
			failed,
			listed,
		)
	}

	return nil
}

// publicReposToRegrade is the top RegradeLimit already graded public repos by SourceCraft rating (all
// with 0). The scheduled run only refreshes existing grades: a repo never graded is not in the list.
// invalid is kept for the caller's count and is always 0.
func (as *AggregatorService) publicReposToRegrade(ctx context.Context) (repos []domain.Repo, invalid int, err error) {
	limit := as.regradeLimit()
	if limit <= 0 {
		limit = math.MaxInt32
	}

	repos, err = as.GradesRepo.TopGradedPublicRepos(ctx, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: list graded public repos: %v", domain.ErrGradingFailed, err)
	}

	return repos, 0, nil
}

// regradeRepo requests a public grading for a repo of the scheduled regrade. Only an already graded
// repo is regraded, and only when SourceCraft reports activity since its grade (asked now); an
// unanswered check skips it until the next run.
func (as *AggregatorService) regradeRepo(ctx context.Context, repo domain.Repo) error {
	if repo.Visibility != domain.VisibilityPublic {
		return nil
	}

	// Only existing grades are refreshed; a repo without one is left for a user or a batch to grade.
	grade, err := as.GradesRepo.GetPublicGradeByRepoID(ctx, repo.SourcecraftUUID)
	if errors.Is(err, domain.ErrGradeNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get grade: %w", err)
	}

	// Only new SourceCraft activity since the grade counts; outdated analytics or facts do not start a
	// scheduled regrade, and an unanswered check skips the repo until the next run.
	fresh, active, err := as.freshActivity(ctx, repo, grade.GradedAt)
	if err != nil {
		log.Printf("regrade %s/%s: activity check: %v (skipped)", repo.OrgSlug, repo.RepoSlug, err)

		return nil
	}

	if fresh.Visibility != domain.VisibilityPublic || !active {
		return nil
	}

	// Waits while users' gradings are in flight or the background slots are full.
	release, err := as.backgroundTurn(ctx)
	if err != nil {
		return err
	}
	defer release()

	if err := as.requestGrading(ctx, fresh, false, false); !errors.Is(err, domain.GradingPending) {
		return err
	}

	return nil
}

// freshActivity looks the repo up in SourceCraft now (saving what it finds) and reports whether it
// was active after gradedAt. On an error it returns the repo as given, counted as active.
func (as *AggregatorService) freshActivity(ctx context.Context, repo domain.Repo, gradedAt *time.Time) (domain.Repo, bool, error) {
	fresh, err := as.resolveRepo(ctx, repo.OrgSlug, repo.RepoSlug)
	if err != nil {
		return repo, true, err
	}

	return fresh, domain.HasNewActivity(fresh.LastActivityAt, gradedAt), nil
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
	GetDescription() string
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
		Description: r.GetDescription(),
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
	if request, ok := as.llmRequests[key]; ok && request.failure == "" && now.Sub(request.requestedAt) < as.llmRequestTTL() {
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

	cont, cancel := context.WithTimeout(withToken(ctx, token), as.rpcTimeout())
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

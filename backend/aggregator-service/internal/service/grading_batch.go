package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"aggregator-service/internal/domain"

	"github.com/google/uuid"
)

// Defaults of the admin batch grading (config BATCH_GRADING_*).
const (
	DefaultBatchConcurrency = 3
	DefaultBatchItemTimeout = 15 * time.Minute
	DefaultBatchPoll        = 2 * time.Second
	// DefaultBatchEstimateSeconds is the guess for one grading until batches have measured real ones.
	DefaultBatchEstimateSeconds = 90

	// gradeClockSkew tolerates the grader's clock being slightly behind ours when deciding whether a
	// grade is newer than the start of the item.
	gradeClockSkew = 5 * time.Second
)

// batchRunner is the process-wide state of the batch worker: at most one batch runs at a time, and
// the batch itself lives in the database, so a restart picks it up again (ResumeBatch).
type batchRunner struct {
	mu      sync.Mutex
	running bool
}

func (as *AggregatorService) batchConcurrency() int {
	if as.BatchConcurrency > 0 {
		return as.BatchConcurrency
	}

	return DefaultBatchConcurrency
}

func (as *AggregatorService) batchItemTimeout() time.Duration {
	if as.BatchItemTimeout > 0 {
		return as.BatchItemTimeout
	}

	return DefaultBatchItemTimeout
}

func (as *AggregatorService) batchPoll() time.Duration {
	if as.BatchPoll > 0 {
		return as.BatchPoll
	}

	return DefaultBatchPoll
}

func (as *AggregatorService) requireBatches() error {
	if as.BatchRepo == nil {
		return fmt.Errorf("%w: batch grading is not configured", domain.ErrInvalidBatch)
	}

	return nil
}

// batchSelection is what a filter selected: up to the filter's limit, highest SourceCraft rating first,
// out of Matched repos. Fetched holds the metadata of repos taken from the catalog, saved to repos when
// the batch starts (they may be new to the service).
type batchSelection struct {
	Repos   []domain.BatchCandidate
	Fetched []domain.Repo
	Matched int
}

// selectForBatch validates the filter and returns the repos it selects, public ones only: from the
// stored catalog, or, before its first successful refresh, from the repos the service already knows.
func (as *AggregatorService) selectForBatch(ctx context.Context, filter domain.BatchFilter) (domain.BatchFilter, batchSelection, error) {
	if err := as.requireBatches(); err != nil {
		return filter, batchSelection{}, err
	}

	filter = filter.Normalize()
	if err := filter.Validate(); err != nil {
		return filter, batchSelection{}, err
	}

	if as.BatchMaxLimit > 0 && filter.Limit > as.BatchMaxLimit {
		return filter, batchSelection{}, fmt.Errorf("%w (%d)", domain.ErrBatchLimitTooHigh, as.BatchMaxLimit)
	}

	if as.catalogInUse(ctx) {
		sel, err := as.selectFromCatalog(ctx, filter)

		return filter, sel, err
	}

	repos, matched, err := as.BatchRepo.SelectBatchRepos(ctx, filter)
	if err != nil {
		return filter, batchSelection{}, err
	}

	return filter, batchSelection{Repos: repos, Matched: matched}, nil
}

// selectFromCatalog selects from the stored public repo catalog. Catalog repos may be new to the
// service, so they are all returned as Fetched.
func (as *AggregatorService) selectFromCatalog(ctx context.Context, filter domain.BatchFilter) (batchSelection, error) {
	entries, matched, err := as.CatalogRepo.Select(ctx, domain.CatalogFilter{
		Languages: filter.Languages, MinScore: filter.MinScore, MaxScore: filter.MaxScore,
		OlderThanDays: filter.OlderThanDays, Repos: filter.Repos, OnlyRegrade: filter.OnlyRegrade,
	}, filter.Limit)
	if err != nil {
		return batchSelection{}, err
	}

	sel := batchSelection{Matched: matched}

	for _, entry := range entries {
		repo := entry.Repo
		sel.Repos = append(sel.Repos, domain.BatchCandidate{
			RepoID: repo.SourcecraftUUID, OrgSlug: repo.OrgSlug, RepoSlug: repo.RepoSlug,
			LastActivityAt: repo.LastActivityAt, GradedAt: entry.GradedAt,
		})
		sel.Fetched = append(sel.Fetched, repo)
	}

	return sel, nil
}

// PreviewBatch is the dry run: how many public repos the filter selects, a sample of them, the
// hand-picked ones that matched nothing and a rough duration. It starts nothing and writes nothing.
func (as *AggregatorService) PreviewBatch(ctx context.Context, filter domain.BatchFilter) (domain.BatchPreview, error) {
	filter, sel, err := as.selectForBatch(ctx, filter)
	if err != nil {
		return domain.BatchPreview{}, err
	}

	repos := sel.Repos
	preview := domain.BatchPreview{Matched: sel.Matched, Count: len(repos)}

	if !filter.Force {
		for _, repo := range repos {
			if !domain.HasNewActivity(repo.LastActivityAt, repo.GradedAt) {
				preview.Inactive++
			}
		}
	}

	for i, repo := range repos {
		if i >= domain.PreviewSampleSize {
			break
		}

		preview.Sample = append(preview.Sample, repo.Slug())
	}

	// With the limit cutting the matches, a missing pick may just be below the cut: not reported then.
	if sel.Matched == len(repos) {
		preview.Unmatched = unmatchedPicks(filter.Repos, repos)
	}

	per := float64(DefaultBatchEstimateSeconds)
	if avg, err := as.BatchRepo.AvgGradeSeconds(ctx); err == nil && avg > 0 {
		per = avg
	}

	preview.EstimateSeconds = int(math.Ceil(float64(len(repos))/float64(as.batchConcurrency())) * per)

	return preview, nil
}

func unmatchedPicks(picked []string, matched []domain.BatchCandidate) []string {
	found := make(map[string]struct{}, len(matched))
	for _, repo := range matched {
		found[lowerASCII(repo.Slug())] = struct{}{}
	}

	var missing []string

	for _, slug := range picked {
		if _, ok := found[lowerASCII(slug)]; !ok {
			missing = append(missing, slug)
		}
	}

	return missing
}

func lowerASCII(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + 'a' - 'A'
		}
	}

	return string(out)
}

// StartBatch creates a batch for the filter and starts working on it. Only one batch can be
// unfinished at a time (ErrBatchActive); a filter that selects nothing is refused.
func (as *AggregatorService) StartBatch(ctx context.Context, actorHash string, filter domain.BatchFilter) (domain.Batch, error) {
	filter, sel, err := as.selectForBatch(ctx, filter)
	if err != nil {
		return domain.Batch{}, err
	}

	repos := sel.Repos
	if len(repos) == 0 {
		return domain.Batch{}, domain.ErrBatchNothing
	}

	// A cancelled batch's worker is alive until its in-flight repos finish; starting another before then
	// would put more than the configured number of gradings in flight.
	if as.batchWorkerBusy() {
		return domain.Batch{}, domain.ErrBatchActive
	}

	// A catalog selection may hold repos the service has never seen; the items need their rows.
	for _, repo := range sel.Fetched {
		if err := as.upsertRepo(ctx, repo); err != nil {
			return domain.Batch{}, fmt.Errorf("save repository %s/%s: %w", repo.OrgSlug, repo.RepoSlug, err)
		}
	}

	id, err := as.BatchRepo.CreateBatch(ctx, actorHash, filter, repos)
	if err != nil {
		return domain.Batch{}, err
	}

	as.auditBatch(ctx, actorHash, domain.AuditActorAdmin, "batch_grading.start", id, map[string]any{
		"total": len(repos), "filter": filterSummary(filter),
	})

	as.startBatchRunner(id)

	return as.BatchRepo.GetBatch(ctx, id)
}

// ControlBatch pauses, resumes or cancels a batch. Pausing lets the repos already in flight finish and
// takes no new ones; cancelling does the same and drops the rest.
func (as *AggregatorService) ControlBatch(ctx context.Context, actorHash string, id int64, action string) (domain.Batch, error) {
	if err := as.requireBatches(); err != nil {
		return domain.Batch{}, err
	}

	switch action {
	case domain.BatchActionPause:
		ok, err := as.BatchRepo.SetBatchStatus(ctx, id, []string{domain.BatchRunning}, domain.BatchPaused)
		if err != nil {
			return domain.Batch{}, err
		}

		if !ok {
			return domain.Batch{}, as.stateError(ctx, id, "only a running batch can be paused")
		}

	case domain.BatchActionResume:
		ok, err := as.BatchRepo.SetBatchStatus(ctx, id, []string{domain.BatchPaused}, domain.BatchRunning)
		if err != nil {
			return domain.Batch{}, err
		}

		if !ok {
			return domain.Batch{}, as.stateError(ctx, id, "only a paused batch can be resumed")
		}

		as.startBatchRunner(id)

	case domain.BatchActionCancel:
		batch, err := as.BatchRepo.GetBatch(ctx, id)
		if err != nil {
			return domain.Batch{}, err
		}

		if !batch.Active() {
			return domain.Batch{}, fmt.Errorf("%w: the batch has already finished", domain.ErrBatchState)
		}

		if err := as.BatchRepo.FinishBatch(ctx, id, domain.BatchCancelled); err != nil {
			return domain.Batch{}, err
		}

		if _, err := as.BatchRepo.SkipPendingItems(ctx, id); err != nil {
			return domain.Batch{}, err
		}

	default:
		return domain.Batch{}, fmt.Errorf("%w: unknown action %q", domain.ErrInvalidBatch, action)
	}

	as.auditBatch(ctx, actorHash, domain.AuditActorAdmin, "batch_grading."+action, id, nil)

	return as.BatchRepo.GetBatch(ctx, id)
}

// stateError explains a refused control action: the batch is missing, or in the wrong state.
func (as *AggregatorService) stateError(ctx context.Context, id int64, reason string) error {
	if _, err := as.BatchRepo.GetBatch(ctx, id); err != nil {
		return err
	}

	return fmt.Errorf("%w: %s", domain.ErrBatchState, reason)
}

// BatchStatus is a batch with the repos that failed in it. id 0 means the active batch, or the
// latest one when none is active.
func (as *AggregatorService) BatchStatus(ctx context.Context, id int64) (domain.Batch, []domain.BatchItem, error) {
	if err := as.requireBatches(); err != nil {
		return domain.Batch{}, nil, err
	}

	var (
		batch domain.Batch
		err   error
	)

	if id == 0 {
		batch, err = as.BatchRepo.GetActiveBatch(ctx)
		if errors.Is(err, domain.ErrBatchNotFound) {
			var latest []domain.Batch

			if latest, err = as.BatchRepo.ListBatches(ctx, 1); err == nil {
				if len(latest) == 0 {
					return domain.Batch{}, nil, domain.ErrBatchNotFound
				}

				batch = latest[0]
			}
		}
	} else {
		batch, err = as.BatchRepo.GetBatch(ctx, id)
	}

	if err != nil {
		return domain.Batch{}, nil, err
	}

	failed, err := as.BatchRepo.ListBatchItems(ctx, batch.ID, domain.BatchItemFailed, domain.MaxBatchFailuresShown)
	if err != nil {
		return domain.Batch{}, nil, err
	}

	return batch, failed, nil
}

// MaxBatchLimit is the stand's cap on one batch's limit; 0 means none.
func (as *AggregatorService) MaxBatchLimit() int { return as.BatchMaxLimit }

// ListBatches returns the most recent batches, newest first.
func (as *AggregatorService) ListBatches(ctx context.Context, limit int) ([]domain.Batch, error) {
	if err := as.requireBatches(); err != nil {
		return nil, err
	}

	if limit <= 0 || limit > 50 {
		limit = 10
	}

	return as.BatchRepo.ListBatches(ctx, limit)
}

// ResumeBatch continues the unfinished batch after a restart: items that were mid-flight go back to
// pending, and a running batch gets its worker again. A paused batch stays paused.
func (as *AggregatorService) ResumeBatch(ctx context.Context) {
	if as.BatchRepo == nil {
		return
	}

	as.batchCtx = ctx

	if err := as.BatchRepo.SkipOrphanedItems(ctx); err != nil {
		log.Printf("resume grading batch: %v", err)
	}

	batch, err := as.BatchRepo.GetActiveBatch(ctx)
	if errors.Is(err, domain.ErrBatchNotFound) {
		return
	}

	if err != nil {
		log.Printf("resume grading batch: %v", err)

		return
	}

	if err := as.BatchRepo.RequeueRunningItems(ctx, batch.ID); err != nil {
		log.Printf("resume grading batch %d: %v", batch.ID, err)

		return
	}

	log.Printf("grading batch %d is %s, %d repositories left", batch.ID, batch.Status, batch.Pending+batch.Running)

	if batch.Status == domain.BatchRunning {
		as.startBatchRunner(batch.ID)
	}
}

func (as *AggregatorService) batchWorkerBusy() bool {
	as.batches.mu.Lock()
	defer as.batches.mu.Unlock()

	return as.batches.running
}

// startBatchRunner starts the worker unless one is already running in this process.
func (as *AggregatorService) startBatchRunner(id int64) {
	ctx := as.batchCtx
	if ctx == nil {
		ctx = context.Background()
	}

	as.batches.mu.Lock()
	defer as.batches.mu.Unlock()

	if as.batches.running {
		return
	}

	as.batches.running = true

	go func() {
		defer func() {
			as.batches.mu.Lock()
			as.batches.running = false
			as.batches.mu.Unlock()
		}()

		as.runBatch(ctx, id)
	}()
}

// runBatch works through the batch with up to batchConcurrency repos in flight, until nothing is
// left, it is cancelled or the process stops. Pause and cancel take effect in the database: a claim
// only succeeds while the batch is running.
func (as *AggregatorService) runBatch(ctx context.Context, id int64) {
	slots := make(chan struct{}, as.batchConcurrency())

	// Without the batch's filter the force flag is unknown; the activity check then runs, the
	// cautious default (a skipped repo can be regraded by hand, a needless grading cannot be undone).
	force := false
	if batch, err := as.BatchRepo.GetBatch(ctx, id); err == nil {
		force = batch.Filter.Force
	}

	var wg sync.WaitGroup

loop:
	for {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			break loop
		}

		item, ok, err := as.BatchRepo.ClaimNextBatchItem(ctx, id)
		if err != nil || !ok {
			<-slots

			if err != nil && ctx.Err() == nil {
				log.Printf("grading batch %d: claim: %v", id, err)
			}

			if !as.batchShouldWait(ctx, id) {
				break loop
			}

			select {
			case <-time.After(as.batchPoll()):
			case <-ctx.Done():
				break loop
			}

			continue
		}

		wg.Add(1)

		go func() {
			defer wg.Done()
			defer func() { <-slots }()

			as.processBatchItem(ctx, id, item, force)
		}()
	}

	wg.Wait()

	if ctx.Err() != nil {
		return
	}

	as.completeBatch(ctx, id)
}

// batchShouldWait is asked when no item could be claimed: true while the batch is paused or an
// error hides its state, false when the batch is over or has nothing left to start.
func (as *AggregatorService) batchShouldWait(ctx context.Context, id int64) bool {
	batch, err := as.BatchRepo.GetBatch(ctx, id)
	if err != nil {
		return ctx.Err() == nil

	}

	switch {
	case !batch.Active():
		return false
	case batch.Status == domain.BatchPaused:
		return true
	default:
		return batch.Pending > 0
	}
}

func (as *AggregatorService) completeBatch(ctx context.Context, id int64) {
	batch, err := as.BatchRepo.GetBatch(ctx, id)
	if err != nil || !batch.Active() || batch.Pending > 0 {
		return
	}

	if err := as.BatchRepo.FinishBatch(ctx, id, domain.BatchCompleted); err != nil {
		log.Printf("finish grading batch %d: %v", id, err)

		return
	}

	as.auditBatch(ctx, "", domain.AuditActorSystem, "batch_grading.complete", id, map[string]any{
		"total": batch.Total, "ok": batch.OK, "failed": batch.Failed,
	})
}

// processBatchItem regrades one repo and records the outcome. A stop of the process leaves the item
// running; ResumeBatch puts it back in the queue.
func (as *AggregatorService) processBatchItem(ctx context.Context, batchID int64, item domain.BatchItem, force bool) {
	status, code := as.gradeBatchItem(ctx, item.RepoID, force)

	if ctx.Err() != nil {
		return
	}

	if err := as.BatchRepo.FinishBatchItem(ctx, batchID, item.RepoID, status, code); err != nil {
		log.Printf("grading batch %d: record %s/%s: %v", batchID, item.OrgSlug, item.RepoSlug, err)
	}

	if as.Metrics != nil {
		as.Metrics.BatchItem(status)
	}
}

func (as *AggregatorService) gradeBatchItem(ctx context.Context, repoID uuid.UUID, force bool) (string, string) {
	repo, err := as.BatchRepo.GetBatchRepo(ctx, repoID)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("grading batch: load repository %s: %v", repoID, err)
		}

		return domain.BatchItemFailed, domain.BatchErrorRequest
	}

	// A batch only ever grades public repos; this guards against a repo that changed visibility since.
	if repo.Visibility != domain.VisibilityPublic {
		return domain.BatchItemSkipped, ""
	}

	if !force {
		active, err := as.batchRepoActive(ctx, repo)
		if err != nil {
			// An unanswered check never skips: the repo is graded as if forced.
			log.Printf("grading batch: activity check %s/%s: %v (grading anyway)", repo.OrgSlug, repo.RepoSlug, err)
		} else if !active {
			return domain.BatchItemSkipped, domain.BatchSkipNoActivity
		}
	}

	key := llmKey(repo.SourcecraftUUID, domain.VisibilityPublic)
	as.takeGradingFailure(key) // a failure from before this attempt must not count against it

	startedAt := as.now()

	if err := as.requestGrading(withTrigger(ctx, "batch"), repo, false, false); !errors.Is(err, domain.GradingPending) {
		log.Printf("grading batch: request %s/%s: %v", repo.OrgSlug, repo.RepoSlug, err)

		return domain.BatchItemFailed, domain.BatchErrorRequest
	}

	deadline := time.NewTimer(as.batchItemTimeout())
	defer deadline.Stop()

	for {
		select {
		case <-ctx.Done():
			return domain.BatchItemFailed, domain.BatchErrorTimeout
		case <-deadline.C:
			return domain.BatchItemFailed, domain.BatchErrorTimeout
		case <-time.After(as.batchPoll()):
		}

		lock, err := as.ReposRepo.GetRepositoryLock(ctx, repo.SourcecraftUUID)
		if err != nil && !errors.Is(err, domain.ErrRepositoryNotFound) {
			continue
		}

		if err == nil && lock.Status != "free" {
			continue
		}

		break
	}

	if as.takeGradingFailure(key) {
		return domain.BatchItemFailed, domain.BatchErrorGrading
	}

	grade, err := as.GradesRepo.GetPublicGradeByRepoID(ctx, repo.SourcecraftUUID)
	if err != nil || grade.GradedAt == nil || grade.GradedAt.Before(startedAt.Add(-gradeClockSkew)) {
		return domain.BatchItemFailed, domain.BatchErrorNoGrade
	}

	return domain.BatchItemOK, ""
}

// batchRepoActive asks SourceCraft for the repo's current last_updated (refreshing the stored
// metadata) and reports whether it is newer than the last public grade.
func (as *AggregatorService) batchRepoActive(ctx context.Context, repo domain.Repo) (bool, error) {
	grade, err := as.GradesRepo.GetPublicGradeByRepoID(ctx, repo.SourcecraftUUID)
	if errors.Is(err, domain.ErrGradeNotFound) {
		return true, nil
	}

	if err != nil {
		return true, fmt.Errorf("load grade: %w", err)
	}

	_, active, err := as.freshActivity(ctx, repo, grade.GradedAt)

	return active, err
}

// auditBatch records a batch action. It is best effort: the batch is already changed, and a failed
// audit write is logged rather than undoing it.
func (as *AggregatorService) auditBatch(ctx context.Context, actorHash, actorKind, action string, id int64, details map[string]any) {
	if as.Metrics != nil {
		as.Metrics.BatchEvent(strings.TrimPrefix(action, "batch_grading."))
	}

	if as.AuditRepo == nil {
		return
	}

	var raw json.RawMessage

	if details != nil {
		raw, _ = json.Marshal(details)
	}

	err := as.RecordAudit(ctx, domain.AuditEvent{
		ActorKind:  actorKind,
		ActorHash:  actorHash,
		Action:     action,
		TargetKind: domain.AuditTargetBatch,
		Target:     fmt.Sprintf("%d", id),
		Outcome:    domain.AuditOutcomeOK,
		Details:    raw,
	})
	if err != nil {
		log.Printf("audit %s for batch %d: %v", action, id, err)
	}
}

// filterSummary is the filter without the hand-picked slugs (only their count), so one audit entry
// stays small.
func filterSummary(filter domain.BatchFilter) map[string]any {
	summary := map[string]any{}

	if len(filter.Languages) > 0 {
		summary["languages"] = filter.Languages
	}

	if filter.MinScore != nil {
		summary["min_score"] = *filter.MinScore
	}

	if filter.MaxScore != nil {
		summary["max_score"] = *filter.MaxScore
	}

	if filter.OlderThanDays != nil {
		summary["older_than_days"] = *filter.OlderThanDays
	}

	if len(filter.Repos) > 0 {
		summary["picked"] = len(filter.Repos)
	}

	summary["limit"] = filter.Limit

	if filter.OnlyRegrade {
		summary["only_regrade"] = true
	}

	if filter.Force {
		summary["force"] = true
	}

	return summary
}

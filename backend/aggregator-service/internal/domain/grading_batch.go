package domain

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrInvalidBatch is returned for a batch filter or request that breaks the rules below.
	ErrInvalidBatch = errors.New("invalid grading batch")
	// ErrBatchLimitTooHigh: the limit is above the stand's cap (BATCH_GRADING_MAX_LIMIT).
	ErrBatchLimitTooHigh = fmt.Errorf("%w: the limit is above this stand's maximum", ErrInvalidBatch)
	// ErrBatchNothing: a valid filter that matches no public repository cannot start a batch.
	ErrBatchNothing = fmt.Errorf("%w: no public repositories match", ErrInvalidBatch)
	// ErrBatchActive: another batch is still running or paused; only one runs at a time.
	ErrBatchActive = errors.New("a grading batch is already active")
	// ErrBatchNotFound: no batch with that id.
	ErrBatchNotFound = errors.New("grading batch not found")
	// ErrBatchState: the action does not fit the batch's current status (resume of a running batch).
	ErrBatchState = errors.New("grading batch is in the wrong state for this action")
)

// Batch statuses. A batch is "active" while it is running or paused.
const (
	BatchRunning   = "running"
	BatchPaused    = "paused"
	BatchCompleted = "completed"
	BatchCancelled = "cancelled"
)

// Batch item statuses.
const (
	BatchItemPending = "pending"
	BatchItemRunning = "running"
	BatchItemOK      = "ok"
	BatchItemFailed  = "failed"
	BatchItemSkipped = "skipped"
)

// Item error codes: fixed words, never text from the graders.
const (
	BatchErrorRequest = "request_failed"
	BatchErrorGrading = "grading_failed"
	BatchErrorTimeout = "timeout"
	BatchErrorNoGrade = "no_new_grade"
	// BatchSkipNoActivity marks a skipped item: SourceCraft reports no activity since its last grade.
	BatchSkipNoActivity = "no_activity"
)

// Batch control actions.
const (
	BatchActionPause  = "pause"
	BatchActionResume = "resume"
	BatchActionCancel = "cancel"
)

const (
	MaxBatchLanguages = 20
	MaxBatchRepos     = 200
	MaxBatchDays      = 3650
	// PreviewSampleSize is how many repos a preview names.
	PreviewSampleSize = 20
	// MaxBatchFailuresShown bounds the per-repo failures one status call returns.
	MaxBatchFailuresShown = 100
)

var batchRepoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}/[A-Za-z0-9._-]{1,100}$`)

// BatchFilter says which public repositories a batch grades. Every set criterion must hold; none set
// means every public repository. Limit is required: of the matches, the Limit with the highest
// SourceCraft rating are taken.
type BatchFilter struct {
	// Languages matches the main language, case-insensitively.
	Languages []string `json:"languages,omitempty"`
	// MinScore and MaxScore bound the stored public score; an ungraded repo never matches them.
	MinScore *int `json:"min_score,omitempty"`
	MaxScore *int `json:"max_score,omitempty"`
	// OlderThanDays matches repos whose last grade is older than that, and repos never graded.
	OlderThanDays *int `json:"older_than_days,omitempty"`
	// Repos is a hand-picked list of "org/repo" slugs.
	Repos []string `json:"repos,omitempty"`
	// Limit is how many of the matching repos are graded, highest SourceCraft rating first; at least 1.
	Limit int `json:"limit"`
	// OnlyRegrade keeps only repos that already have a public grade; without it never-graded repos
	// are graded for the first time.
	OnlyRegrade bool `json:"only_regrade,omitempty"`
	// Force regrades every selected repo; without it a repo with no SourceCraft activity since its
	// last grade is skipped. Not a criterion: it never selects anything on its own.
	Force bool `json:"force,omitempty"`
}

// Normalize trims and de-duplicates the list fields.
func (f BatchFilter) Normalize() BatchFilter {
	f.Languages = uniqueTrimmed(f.Languages, true)
	f.Repos = uniqueTrimmed(f.Repos, false)

	return f
}

func uniqueTrimmed(values []string, lower bool) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)
		if lower {
			value = strings.ToLower(value)
		}

		if value == "" {
			continue
		}

		if _, dup := seen[strings.ToLower(value)]; dup {
			continue
		}

		seen[strings.ToLower(value)] = struct{}{}
		out = append(out, value)
	}

	return out
}

// Validate checks a normalized filter.
func (f BatchFilter) Validate() error {
	switch {
	case f.Limit < 1:
		return fmt.Errorf("%w: the repository limit is required and at least 1", ErrInvalidBatch)
	case len(f.Languages) > MaxBatchLanguages:
		return fmt.Errorf("%w: at most %d languages", ErrInvalidBatch, MaxBatchLanguages)
	case len(f.Repos) > MaxBatchRepos:
		return fmt.Errorf("%w: at most %d hand-picked repositories", ErrInvalidBatch, MaxBatchRepos)
	}

	for _, language := range f.Languages {
		if len(language) > 64 {
			return fmt.Errorf("%w: a language name is too long", ErrInvalidBatch)
		}
	}

	for _, repo := range f.Repos {
		if !batchRepoPattern.MatchString(repo) {
			return fmt.Errorf("%w: repositories are written org/repo", ErrInvalidBatch)
		}
	}

	for _, score := range []*int{f.MinScore, f.MaxScore} {
		if score != nil && (*score < 0 || *score > 100) {
			return fmt.Errorf("%w: scores are between 0 and 100", ErrInvalidBatch)
		}
	}

	if f.MinScore != nil && f.MaxScore != nil && *f.MinScore > *f.MaxScore {
		return fmt.Errorf("%w: the minimum score is above the maximum", ErrInvalidBatch)
	}

	if f.OlderThanDays != nil && (*f.OlderThanDays < 1 || *f.OlderThanDays > MaxBatchDays) {
		return fmt.Errorf("%w: age is between 1 and %d days", ErrInvalidBatch, MaxBatchDays)
	}

	return nil
}

// BatchCandidate is a public repository a filter selected.
type BatchCandidate struct {
	RepoID   uuid.UUID
	OrgSlug  string
	RepoSlug string
	// LastActivityAt (SourceCraft's last_updated as last seen) and GradedAt (last public grade)
	// estimate in a preview whether the activity check will skip the repo; nil when unknown.
	LastActivityAt *time.Time
	GradedAt       *time.Time
}

// HasNewActivity reports whether SourceCraft saw activity after the last grade. A repo never
// graded, or with an unknown activity time, counts as active: skipping needs a definite answer.
func HasNewActivity(lastActivityAt, gradedAt *time.Time) bool {
	if gradedAt == nil || lastActivityAt == nil {
		return true
	}

	return lastActivityAt.After(*gradedAt)
}

func (c BatchCandidate) Slug() string { return c.OrgSlug + "/" + c.RepoSlug }

// BatchPreview is the dry run: what a filter would regrade, without starting anything.
type BatchPreview struct {
	// Matched is how many public repos the criteria match; Count how many of them the limit takes.
	Matched int
	Count   int
	// Sample names the first repos.
	Sample []string
	// Unmatched are hand-picked slugs that are not public repositories known to the service.
	Unmatched []string
	// EstimateSeconds is a rough duration at the configured concurrency.
	EstimateSeconds int
	// Inactive estimates how many selected repos the activity check will skip (0 with Force).
	// The check itself runs again with fresh data right before each repo is graded.
	Inactive int
}

// Batch is one grading batch with its item counts.
type Batch struct {
	ID         int64
	CreatedAt  time.Time
	FinishedAt *time.Time
	Status     string
	ActorHash  string
	Filter     BatchFilter

	Total, Pending, Running, OK, Failed, Skipped int
}

// Active reports whether the batch can still do work.
func (b Batch) Active() bool { return b.Status == BatchRunning || b.Status == BatchPaused }

// BatchItem is one repository of a batch.
type BatchItem struct {
	RepoID     uuid.UUID
	OrgSlug    string
	RepoSlug   string
	Status     string
	Error      string
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// BatchRepository stores batches. Only the Aggregator writes them.
type BatchRepository interface {
	// SelectBatchRepos returns up to filter.Limit known public repos matching the filter, highest
	// SourceCraft rating first, and how many matched in all.
	SelectBatchRepos(ctx context.Context, filter BatchFilter) (repos []BatchCandidate, matched int, err error)
	// GetBatchRepo loads the few columns a grading request needs (ids, slugs, visibility), whatever the
	// other columns hold; ErrRepositoryNotFound when the repo is gone.
	GetBatchRepo(ctx context.Context, id uuid.UUID) (Repo, error)
	// AvgGradeSeconds is the mean time recent successful batch items took; 0 when there is no data.
	AvgGradeSeconds(ctx context.Context) (float64, error)

	// CreateBatch stores a running batch with all items pending. ErrBatchActive when another is unfinished.
	CreateBatch(ctx context.Context, actorHash string, filter BatchFilter, repos []BatchCandidate) (int64, error)
	GetBatch(ctx context.Context, id int64) (Batch, error)
	// GetActiveBatch returns the running or paused batch; ErrBatchNotFound when there is none.
	GetActiveBatch(ctx context.Context) (Batch, error)
	ListBatches(ctx context.Context, limit int) ([]Batch, error)
	ListBatchItems(ctx context.Context, id int64, status string, limit int) ([]BatchItem, error)

	// SetBatchStatus moves a batch between the active statuses (from -> to); false when it was not in one of from.
	SetBatchStatus(ctx context.Context, id int64, from []string, to string) (bool, error)
	// FinishBatch ends an active batch with a final status and sets finished_at.
	FinishBatch(ctx context.Context, id int64, status string) error
	// ClaimNextBatchItem takes the next pending item (-> running) of a running batch; false when none is
	// pending or the batch is paused or finished, so a pause or cancel stops new work atomically.
	ClaimNextBatchItem(ctx context.Context, id int64) (BatchItem, bool, error)
	FinishBatchItem(ctx context.Context, id int64, repoID uuid.UUID, status, errCode string) error
	// SkipPendingItems marks every pending item skipped and returns how many.
	SkipPendingItems(ctx context.Context, id int64) (int, error)
	// SkipOrphanedItems marks items still "running" in a finished batch (cancelled while they were in
	// flight, then the process stopped) as skipped, so their counters do not stay stuck.
	SkipOrphanedItems(ctx context.Context) error
	// RequeueRunningItems puts items left running by a restart back to pending.
	RequeueRunningItems(ctx context.Context, id int64) error
}

package domain

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrCatalogRefreshActive: a catalog refresh is already running; only one runs at a time.
	ErrCatalogRefreshActive = errors.New("a catalog refresh is already running")
	// ErrCatalogRefreshNotRunning: there is no running refresh to cancel.
	ErrCatalogRefreshNotRunning = errors.New("no catalog refresh is running")
)

// Catalog refresh statuses.
const (
	CatalogRefreshRunning   = "running"
	CatalogRefreshCompleted = "completed"
	CatalogRefreshFailed    = "failed"
	CatalogRefreshCancelled = "cancelled"
)

// Catalog refresh triggers.
const (
	CatalogTriggerAdmin   = "admin"
	CatalogTriggerStartup = "startup"
)

// Catalog refresh error codes: short fixed words, never free text from SourceCraft.
const (
	CatalogErrorWrapper      = "wrapper_error"
	CatalogErrorTooManyPages = "too_many_pages"
	CatalogErrorPageLoop     = "page_loop"
	CatalogErrorStore        = "store_error"
	CatalogErrorInterrupted  = "interrupted"
	CatalogErrorEmpty        = "empty_listing"
)

// CatalogRefresh is one fetch of the full public repo listing.
type CatalogRefresh struct {
	ID         int64
	StartedAt  time.Time
	FinishedAt *time.Time
	Status     string
	Trigger    string
	ActorHash  string
	Pages      int
	Repos      int
	Error      string
}

// CatalogStatus is what the admin panel shows: the catalog in use and the refreshes behind it.
type CatalogStatus struct {
	// Size is the number of repos in the catalog in use; 0 before the first successful refresh.
	Size int
	// UpdatedAt is when the catalog in use was stored; nil before the first successful refresh.
	UpdatedAt *time.Time
	// Current is the running refresh, or the latest one when none runs; nil when there has never been one.
	Current *CatalogRefresh
	// LastCompleted is the refresh that produced the catalog in use.
	LastCompleted *CatalogRefresh
}

// CatalogFilter narrows a catalog selection, highest SourceCraft rating first; it mirrors BatchFilter's criteria. A score bound never
// matches a repo without a grade; the age bound always does.
type CatalogFilter struct {
	Languages     []string
	MinScore      *int
	MaxScore      *int
	OlderThanDays *int
	// Repos are lower-cased "org/repo" slugs.
	Repos []string
	// OnlyRegrade keeps only repos with a public grade.
	OnlyRegrade bool
}

// CatalogEntry is a catalog repo with its last public grade, when it has one.
type CatalogEntry struct {
	Repo     Repo
	GradedAt *time.Time
}

// CatalogRepository stores the public repo catalog and its refreshes. Only the Aggregator writes it.
type CatalogRepository interface {
	// StartRefresh records a running refresh and empties the staging table.
	// ErrCatalogRefreshActive when another refresh is running.
	StartRefresh(ctx context.Context, trigger, actorHash string) (int64, error)
	// StagePage appends one page of repos to the staging table and updates the refresh's progress.
	StagePage(ctx context.Context, id int64, repos []Repo, pages, total int) error
	// CommitRefresh replaces the catalog with the staged repos in one transaction and completes the
	// refresh; it returns the new catalog size.
	CommitRefresh(ctx context.Context, id int64) (int, error)
	// FinishRefresh ends a refresh as failed or cancelled with a fixed error code; the catalog is untouched.
	FinishRefresh(ctx context.Context, id int64, status, code string) error
	// FailOrphanedRefreshes ends refreshes left running by a previous process as failed (interrupted).
	FailOrphanedRefreshes(ctx context.Context) error
	GetRefresh(ctx context.Context, id int64) (CatalogRefresh, error)
	// Status returns the catalog size, its age and the current and last completed refreshes.
	Status(ctx context.Context) (CatalogStatus, error)

	// Size is the number of repos in the catalog.
	Size(ctx context.Context) (int, error)
	// TopRated returns up to limit catalog repos, highest SourceCraft rating first.
	TopRated(ctx context.Context, limit int) ([]Repo, error)
	// Select returns up to limit catalog repos matching the filter, highest rating first, with their
	// last public grade, and how many matched in all.
	Select(ctx context.Context, filter CatalogFilter, limit int) (entries []CatalogEntry, matched int, err error)
}

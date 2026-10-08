package domain

import "time"

// ReasonError carries a fixed reason word from the Aggregator (docs/admin.md) next to the usual error
// class, so the admin UI can explain a refused batch request. It unwraps to Err.
type ReasonError struct {
	Reason string
	Err    error
}

func (e ReasonError) Error() string { return e.Err.Error() }
func (e ReasonError) Unwrap() error { return e.Err }

// BatchFilter says which public repositories a batch regrade covers (mirrors the Aggregator's rules).
type BatchFilter struct {
	Languages     []string
	MinScore      *int32
	MaxScore      *int32
	OlderThanDays *int32
	Repos         []string
	TopRated      *int32
	All           bool
	Force         bool
}

// BatchPreview is the dry run of a batch.
type BatchPreview struct {
	Count           int32
	Capped          bool
	Sample          []string
	Unmatched       []string
	EstimateSeconds int32
	Inactive        int32
}

// Batch statuses and control actions.
const (
	BatchActionPause  = "pause"
	BatchActionResume = "resume"
	BatchActionCancel = "cancel"
)

// RegradeBatch is one batch with its item counts.
type RegradeBatch struct {
	ID         int64
	CreatedAt  time.Time
	FinishedAt *time.Time
	Status     string
	ActorHash  string
	Filter     BatchFilter
	Total      int32
	Pending    int32
	Running    int32
	OK         int32
	Failed     int32
	Skipped    int32
}

// RegradeBatchFailure is a repo that failed in a batch, with a fixed error code.
type RegradeBatchFailure struct {
	OrgSlug  string
	RepoSlug string
	Error    string
}

// RegradeBatchDetail is a batch with the repos that failed in it.
type RegradeBatchDetail struct {
	Batch    RegradeBatch
	Failures []RegradeBatchFailure
}

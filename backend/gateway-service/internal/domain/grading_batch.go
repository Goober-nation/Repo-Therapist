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

// BatchFilter says which public repositories a batch grading covers (mirrors the Aggregator's rules).
type BatchFilter struct {
	Languages     []string
	MinScore      *int32
	MaxScore      *int32
	OlderThanDays *int32
	Repos         []string
	Force         bool
	// Limit is how many matching repos are graded, highest SourceCraft rating first; required.
	Limit int32
	// OnlyRegrade keeps only repos that already have a public grade.
	OnlyRegrade bool
}

// BatchPreview is the dry run of a batch.
type BatchPreview struct {
	// Matched is how many repos the criteria match; Count how many of them the limit takes.
	Matched         int32
	Count           int32
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

// GradingBatch is one batch with its item counts.
type GradingBatch struct {
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

// GradingBatchFailure is a repo that failed in a batch, with a fixed error code.
type GradingBatchFailure struct {
	OrgSlug  string
	RepoSlug string
	Error    string
}

// GradingBatchList is the recent batches and the stand's cap on one batch's limit (0 means none).
type GradingBatchList struct {
	Batches  []GradingBatch
	MaxLimit int32
}

// GradingBatchDetail is a batch with the repos that failed in it.
type GradingBatchDetail struct {
	Batch    GradingBatch
	Failures []GradingBatchFailure
}

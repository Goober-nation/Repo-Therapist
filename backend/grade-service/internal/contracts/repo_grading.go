// Package contracts holds the Kafka message shapes shared between services — the machine-readable
// form of docs/kafka-contracts.md. These two topics are entirely within Repository/Grade service's
// own scope (no cross-team dependency), so they're defined here directly rather than waiting on
// wider team sign-off, per backlog/phase-3-kafka-integration.md.
package contracts

import (
	"encoding/json"
	"time"
)

const (
	TopicGradeRequested = "repo.grade.requested"
	TopicRepoReady      = "repo.ready"
)

// AllTopics is every topic this exchange uses — pass to kafka.EnsureTopics at service startup so
// neither service can join a consumer group before its topic exists. Includes the Aggregator's
// two topics too (grade-service is the only service that touches those, but provisions them here
// alongside its own internal ones for the same reason).
var AllTopics = []string{TopicGradeRequested, TopicRepoReady, TopicGradingRequests, TopicGradingResults}

// GradeRequested is published to ask Repository service to clone+upload a repo. Consumed by
// Repository service, produced by a manual trigger for now (grade-service --request-org/--repo) —
// eventually the Aggregator, not built yet.
type GradeRequested struct {
	OrgSlug     string    `json:"org_slug"`
	RepoSlug    string    `json:"repo_slug"`
	RequestedAt time.Time `json:"requested_at"`
}

// ReadyStatus values for RepoReady.Status.
const (
	StatusReady  = "ready"
	StatusFailed = "failed"
)

// RepoReady is published once Repository service has finished (successfully or not) — consumed
// by Grade service, which pulls from S3 and scores on StatusReady, logs and skips on StatusFailed.
type RepoReady struct {
	OrgSlug         string    `json:"org_slug"`
	RepoSlug        string    `json:"repo_slug"`
	SourcecraftUUID string    `json:"sourcecraft_uuid,omitempty"`
	S3Prefix        string    `json:"s3_prefix,omitempty"`
	Status          string    `json:"status"` // StatusReady | StatusFailed
	Error           string    `json:"error,omitempty"`
	CommitCount     int       `json:"commit_count,omitempty"`
	IssueCount      int       `json:"issue_count,omitempty"`
	MilestoneCount  int       `json:"milestone_count,omitempty"`
	ReadyAt         time.Time `json:"ready_at"`
}

// The Aggregator's real contract (see agents_memory/topics/aggregator-integration.md) — separate
// from the two topics above, which are Repository<->Grade service's own internal handoff.
// Grade service is the only service that talks to these two; Repository service is unaffected.
const (
	TopicGradingRequests = "grading.requests"
	TopicGradingResults  = "grading.results"
)

// GradingRequestedEvent is published by the Aggregator to ask for a repo to be graded — the exact
// trigger for GradeRequested above, just from the Aggregator instead of a manual/local trigger.
// Byte-identical shape to GradeRequested by design: consuming this and republishing GradeRequested
// is a straight passthrough, no field mapping needed.
type GradingRequestedEvent struct {
	OrgSlug     string    `json:"org_slug"`
	RepoSlug    string    `json:"repo_slug"`
	RequestedAt time.Time `json:"requested_at"`
}

// Recommendation is one deduction-reason -> pre-written-text pairing, generated while scoring
// (see config/recommendations.yaml) — not the LLM's recommendation for Documentation, which
// arrives later via the Aggregator's separate LLM event flow and is merged in on that side.
type Recommendation struct {
	CategoryID string `json:"category_id"`
	MetricID   string `json:"metric_id"`
	Text       string `json:"text"`
}

// GradingResultEvent is published once grading finishes — consumed by the Aggregator, which
// writes it via UpsertGrade and releases the repo_lock. PublicScore/PrivateScore are nil only when
// every category feeding that view is NoData (nothing at all to score); MissingData names every
// category that's NoData/not_calculated in the (fuller) private view.
type GradingResultEvent struct {
	SourcecraftUUID string `json:"sourcecraft_uuid"`

	PublicScore           *int16          `json:"public_score"`
	PublicCategoryScores  json.RawMessage `json:"public_category_scores"`
	PublicRecommendations json.RawMessage `json:"public_recommendations"`

	PrivateScore           *int16          `json:"private_score"`
	PrivateCategoryScores  json.RawMessage `json:"private_category_scores"`
	PrivateRecommendations json.RawMessage `json:"private_recommendations"`

	MissingData []string  `json:"missing_data"`
	GradedAt    time.Time `json:"graded_at"`
}

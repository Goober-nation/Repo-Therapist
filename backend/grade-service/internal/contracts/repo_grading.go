// Package contracts holds the Kafka message shapes shared between services — the machine-readable
// form of docs/kafka-contracts.md. These two topics are entirely within Repository/Grade service's
// own scope (no cross-team dependency), so they're defined here directly rather than waiting on
// wider team sign-off, per backlog/phase-3-kafka-integration.md.
package contracts

import (
	"encoding/json"
	"os"
	"strings"
	"time"
)

// Topic names default to the documented contract and can be overridden per environment; the env
// names are shared with the other services (docs/kafka-contracts.md, "Topic env vars").
var (
	TopicGradeRequested = TopicEnv("KAFKA_REPO_GRADE_REQUESTED_TOPIC", "repo.grade.requested")
	TopicRepoReady      = TopicEnv("KAFKA_REPO_READY_TOPIC", "repo.ready")

	// Grade <-> LLM Documentation exchange (docs/kafka-contracts.md, "active Grade ↔ LLM contract").
	TopicDocsGradeRequested = TopicEnv("KAFKA_DOCS_GRADE_REQUESTED_TOPIC", "repo.docs.grade.requested")
	TopicDocsGraded         = TopicEnv("KAFKA_DOCS_GRADED_TOPIC", "repo.docs.graded")
)

// TopicEnv returns the trimmed value of env var key, or def when it is unset or blank.
func TopicEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// AllTopics is every topic this exchange uses — pass to kafka.EnsureTopics at service startup so
// neither service can join a consumer group before its topic exists. Includes the Aggregator's
// two topics too (grade-service is the only service that touches those, but provisions them here
// alongside its own internal ones for the same reason).
var AllTopics = []string{TopicGradeRequested, TopicRepoReady, TopicGradingRequests, TopicGradingResults, TopicDocsGradeRequested, TopicDocsGraded}

// GradeRequested is published to ask Repository service to clone+upload a repo. Consumed by
// Repository service, produced by grade-service relaying grading.requests.
type GradeRequested struct {
	OrgSlug     string    `json:"org_slug"`
	RepoSlug    string    `json:"repo_slug"`
	RequestedAt time.Time `json:"requested_at"`
	// Token is the user's SourceCraft PAT for private grading; empty means use the service PAT.
	// Never log it.
	Token string `json:"token,omitempty"`
}

// ReadyStatus values for RepoReady.Status.
const (
	StatusReady  = "ready"
	StatusFailed = "failed"
)

// RepoReady is published once Repository service has finished (successfully or not) — consumed
// by Grade service, which pulls from S3 and scores on StatusReady, reports a failure on
// StatusFailed. SourcecraftUUID is set on a failure once the repo id was resolved; without it the
// failure cannot reach the Aggregator.
type RepoReady struct {
	OrgSlug         string `json:"org_slug"`
	RepoSlug        string `json:"repo_slug"`
	SourcecraftUUID string `json:"sourcecraft_uuid,omitempty"`
	S3Prefix        string `json:"s3_prefix,omitempty"`
	Status          string `json:"status"` // StatusReady | StatusFailed
	Error           string `json:"error,omitempty"`
}

// The Aggregator's real contract (see agents_memory/topics/aggregator-integration.md) — separate
// from the two topics above, which are Repository<->Grade service's own internal handoff.
// Grade service is the only service that talks to these two; Repository service is unaffected.
var (
	TopicGradingRequests = TopicEnv("KAFKA_GRADING_REQUESTS_TOPIC", "grading.requests")
	TopicGradingResults  = TopicEnv("KAFKA_GRADING_RESULTS_TOPIC", "grading.results")
)

// Grading visibilities. A job is graded as exactly one of these: private when the request
// carries a user token, public otherwise.
const (
	VisibilityPublic  = "public"
	VisibilityPrivate = "private"
)

// GradingRequestedEvent is published by the Aggregator to ask for a repo to be graded — the exact
// trigger for GradeRequested above, just from the Aggregator instead of a manual/local trigger.
type GradingRequestedEvent struct {
	OrgSlug     string    `json:"org_slug"`
	RepoSlug    string    `json:"repo_slug"`
	Visibility  string    `json:"visibility"`
	RequestedAt time.Time `json:"requested_at"`
	// Token present -> private grading with the user's PAT; absent -> public grading with the
	// service PAT. Never log it.
	Token string `json:"token,omitempty"`
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
// writes it to the repo_grades_* table matching Visibility and releases the repo_lock. Score is
// nil only when every category in that view is NoData. Advice holds every Recommendation of that
// view's categories, in category order. MissingData names the view's categories
// that are NoData/not_calculated. A nonempty Error means grading failed: Score, CategoryScores and
// Advice are null and the Aggregator only releases the lock.
type GradingResultEvent struct {
	SourcecraftUUID string          `json:"sourcecraft_uuid"`
	Visibility      string          `json:"visibility"` // VisibilityPublic or VisibilityPrivate
	Score           *int16          `json:"score"`
	CategoryScores  json.RawMessage `json:"category_scores"`
	Advice          json.RawMessage `json:"advice"`
	MissingData     []string        `json:"missing_data"`
	GradedAt        time.Time       `json:"graded_at"`
	Error           string          `json:"error"`
}

// Grading failure codes carried in GradingResultEvent.Error.
const (
	GradingErrRepoFailed = "REPO_FETCH_FAILED"
	GradingErrGrading    = "GRADING_FAILED"
	// GradingErrInterrupted: grade-service was shut down while grading.
	GradingErrInterrupted = "GRADING_INTERRUPTED"
	GradingErrBuildResult = "RESULT_BUILD_FAILED"
)

// DocsGradeRequested asks the LLM service to grade a repo's documentation from its S3 prefix.
type DocsGradeRequested struct {
	SourcecraftUUID string    `json:"sourcecraft_uuid"`
	S3Prefix        string    `json:"s3_prefix"`
	RequestedAt     time.Time `json:"requested_at"`
}

// DocsGraded is the LLM service's reply: grade and subgrades are integers 0-100; sections it
// didn't review are absent from Subgrades. Advice holds Russian improvement recommendations.
// A nonempty Error means the evaluation failed: Grade is null, the other fields empty, and the
// reply must be read as Documentation no-data. Grade is a pointer so null never decodes as 0.
type DocsGraded struct {
	SourcecraftUUID string         `json:"sourcecraft_uuid"`
	Grade           *int           `json:"grade"`
	Subgrades       map[string]int `json:"subgrades"`
	Advice          []string       `json:"advice"`
	Error           string         `json:"error"`
	EvaluatedAt     time.Time      `json:"evaluated_at"`
}

// Package contracts holds the Kafka message shapes shared between services — the machine-readable
// form of docs/kafka-contracts.md. These two topics are entirely within Repository/Grade service's
// own scope (no cross-team dependency), so they're defined here directly rather than waiting on
// wider team sign-off, per backlog/phase-3-kafka-integration.md.
package contracts

import (
	"os"
	"strings"
	"time"
)

// Topic names default to the documented contract and can be overridden per environment; the env
// names are shared with grade-service (docs/kafka-contracts.md, "Topic env vars").
var (
	TopicGradeRequested = TopicEnv("KAFKA_REPO_GRADE_REQUESTED_TOPIC", "repo.grade.requested")
	TopicRepoReady      = TopicEnv("KAFKA_REPO_READY_TOPIC", "repo.ready")
)

// TopicEnv returns the trimmed value of env var key, or def when it is unset or blank.
func TopicEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// AllTopics is every topic this exchange uses — pass to kafka.EnsureTopics at service startup so
// neither service can join a consumer group before its topic exists.
var AllTopics = []string{TopicGradeRequested, TopicRepoReady}

// GradeRequested is published to ask Repository service to clone+upload a repo. Consumed by
// Repository service, produced by grade-service relaying grading.requests.
// JobID is the Aggregator's grading job id; a private job's user PAT is read from Redis under
// it (tokenstore.Key). No stored token means the service PAT.
type GradeRequested struct {
	JobID       string    `json:"job_id"`
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
// by Grade service, which pulls from S3 and scores on StatusReady, reports a failure on
// StatusFailed. SourcecraftUUID is set on a failure once the repo id was resolved; without it the
// failure cannot reach the Aggregator.
// JobID echoes GradeRequested.JobID.
type RepoReady struct {
	JobID           string `json:"job_id"`
	OrgSlug         string `json:"org_slug"`
	RepoSlug        string `json:"repo_slug"`
	SourcecraftUUID string `json:"sourcecraft_uuid,omitempty"`
	S3Prefix        string `json:"s3_prefix,omitempty"`
	Status          string `json:"status"` // StatusReady | StatusFailed
	Error           string `json:"error,omitempty"`
}

// Package contracts holds the Kafka message shapes shared between services — the machine-readable
// form of docs/kafka-contracts.md. These two topics are entirely within Repository/Grade service's
// own scope (no cross-team dependency), so they're defined here directly rather than waiting on
// wider team sign-off, per backlog/phase-3-kafka-integration.md.
package contracts

import "time"

const (
	TopicGradeRequested = "repo.grade.requested"
	TopicRepoReady      = "repo.ready"
)

// AllTopics is every topic this exchange uses — pass to kafka.EnsureTopics at service startup so
// neither service can join a consumer group before its topic exists.
var AllTopics = []string{TopicGradeRequested, TopicRepoReady}

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

package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Grading views. A grade is stored and requested as exactly one of these.
const (
	VisibilityPublic  = "public"
	VisibilityPrivate = "private"
)

// GradingRequestedEvent is grading.requests (docs/kafka-contracts.md). Visibility is the repo's
// SourceCraft visibility. JobID identifies this grading run end to end; for private grading the
// caller's PAT is stored in Redis under GradingTokenKey(JobID), never in the message itself.
type GradingRequestedEvent struct {
	JobID       string    `json:"job_id"`
	OrgSlug     string    `json:"org_slug"`
	RepoSlug    string    `json:"repo_slug"`
	Visibility  string    `json:"visibility"`
	RequestedAt time.Time `json:"requested_at"`
}

// GradingTokenKey is the Redis key holding a private grading's user PAT (docs/kafka-contracts.md).
func GradingTokenKey(jobID string) string {
	return "grading:token:" + jobID
}

type LLMRecommendationsRequestedEvent struct {
	Visibility      string    `json:"visibility"`
	SourcecraftUUID uuid.UUID `json:"sourcecraft_uuid"`
	OrgSlug         string    `json:"org_slug"`
	RepoSlug        string    `json:"repo_slug"`
	Grade           LLMGrade  `json:"grade"`
	RequestedAt     time.Time `json:"requested_at"`
}

// LLMGrade is the grade in the shape the LLM service accepts: category scores are 0-100 numbers
// or null for no-data, advice is a list of recommendations with their priorities.
type LLMGrade struct {
	Score          *int16              `json:"score"`
	CategoryScores map[string]*float64 `json:"category_scores"`
	Advice         []LLMAdvice         `json:"advice"`
	MissingData    []string            `json:"missing_data"`
	GradedAt       *time.Time          `json:"graded_at"`
}

// LLMAdvice is one grade recommendation passed to the LLM: its category, priority
// (critical|high|medium|low) and text without the transitional "[priority] " tag.
type LLMAdvice struct {
	CategoryID string `json:"category_id"`
	Priority   string `json:"priority"`
	Text       string `json:"text"`
}

type LLMRecommendationsReceivedEvent struct {
	SourcecraftUUID uuid.UUID       `json:"sourcecraft_uuid"`
	RawResponse     json.RawMessage `json:"raw_response"`
	ModelUsed       string          `json:"model_used"`
	EvaluatedAt     time.Time       `json:"evaluated_at"`
	// GradedAt echoes grade.graded_at of the request: the grade this response describes.
	GradedAt   *time.Time `json:"graded_at"`
	Visibility string     `json:"visibility"`
	Error      string     `json:"error"`
}

type GradingResultReceivedEvent struct {
	SourcecraftUUID uuid.UUID `json:"sourcecraft_uuid"`

	Score          *int16          `json:"score"`
	CategoryScores json.RawMessage `json:"category_scores"`
	Advice         json.RawMessage `json:"advice"`

	MissingData []string  `json:"missing_data"`
	GradedAt    time.Time `json:"graded_at"`
	// GradeMode is how the repo was graded: public (no user token) or private (with one).
	// It is not the repo's SourceCraft visibility.
	GradeMode string `json:"grade_mode"`
	// Confidence is the low-confidence flag of a public grade; nil for private grades, failures
	// and when grade-service has the flag turned off.
	Confidence *GradeConfidence `json:"confidence"`
	// Analytics is the informational analytics object, passed through untouched; null when
	// grade-service sent none.
	Analytics json.RawMessage `json:"analytics"`
	Error     string          `json:"error"`
}

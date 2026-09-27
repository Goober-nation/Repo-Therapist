package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type GradingRequestedEvent struct {
	OrgSlug     string    `json:"org_slug"`
	RepoSlug    string    `json:"repo_slug"`
	RequestedAt time.Time `json:"requested_at"`
}

type LLMRecommendationsRequestedEvent struct {
	Visibility      string    `json:"visibility"`
	SourcecraftUUID uuid.UUID `json:"sourcecraft_uuid"`
	OrgSlug         string    `json:"org_slug"`
	RepoSlug        string    `json:"repo_slug"`
	Grade           RepoGrade `json:"grade"`
	RequestedAt     time.Time `json:"requested_at"`
}

type LLMRecommendationsReceivedEvent struct {
	SourcecraftUUID uuid.UUID       `json:"sourcecraft_uuid"`
	RawResponse     json.RawMessage `json:"raw_response"`
	ModelUsed       string          `json:"model_used"`
	EvaluatedAt     time.Time       `json:"evaluated_at"`
	Visibility      string          `json:"visibility"`
	Error           string          `json:"error"`
}

type GradingResultReceivedEvent struct {
	SourcecraftUUID uuid.UUID `json:"sourcecraft_uuid"`

	Score          *int16          `json:"score"`
	CategoryScores json.RawMessage `json:"category_scores"`
	Advice         json.RawMessage `json:"advice"`

	MissingData []string  `json:"missing_data"`
	GradedAt    time.Time `json:"graded_at"`
	Visibility  string    `json:"visibility"`
	Error       string    `json:"error"`
}

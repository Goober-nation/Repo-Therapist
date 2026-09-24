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
}

type GradingResultReceivedEvent struct {
	SourcecraftUUID uuid.UUID `json:"sourcecraft_uuid"`

	PublicScore           *int16          `json:"public_score"`
	PublicCategoryScores  json.RawMessage `json:"public_category_scores"`
	PublicRecommendations json.RawMessage `json:"public_recommendations"`

	PrivateScore           *int16          `json:"private_score"`
	PrivateCategoryScores  json.RawMessage `json:"private_category_scores"`
	PrivateRecommendations json.RawMessage `json:"private_recommendations"`

	MissingData []string  `json:"missing_data"`
	GradedAt    time.Time `json:"graded_at"`
}

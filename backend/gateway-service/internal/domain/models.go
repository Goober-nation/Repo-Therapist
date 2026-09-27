package domain

import (
	"time"

	"github.com/google/uuid"
)

type Repo struct {
	SourcecraftUUID uuid.UUID `json:"sourcecraft_uuid"`

	OrgSlug  string `json:"org_slug"`
	RepoSlug string `json:"repo_slug"`
	WebURL   string `json:"web_url"`

	Visibility     string     `json:"visibility"`
	LastActivityAt *time.Time `json:"last_activity_at"`

	RatingValue float64         `json:"rating"`
	Language    PrimaryLanguage `json:"language"`
}

type PrimaryLanguage struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type RepoGrade struct {
	SourcecraftUUID uuid.UUID `json:"sourcecraft_uuid"`

	Score          *int32 `json:"score"`
	CategoryScores []byte `json:"category_scores"`
	Advice         []byte `json:"advice"`

	MissingData []string  `json:"missing_data"`
	GradedAt    time.Time `json:"graded_at"`
}

type GetGradingResponse struct {
	Grade RepoGrade `json:"grade"`
}

type LLMRecommendations struct {
	RawResponse []byte    `json:"raw_response"`
	ModelUsed   string    `json:"model_used"`
	EvaluatedAt time.Time `json:"evaluated_at"`
}

type RankingEntry struct {
	Place           int32
	SourcecraftUUID uuid.UUID
	Name            string
	WebURL          string
	PublicScore     *int32
	Likes           *float64
	Language        PrimaryLanguage
	LastActivityAt  *time.Time
}

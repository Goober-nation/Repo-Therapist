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

// ConfidenceReason is one generic pre-written reason a public grade is low-confidence.
type ConfidenceReason struct {
	Code string
	Text string
}

// Confidence is the low-confidence flag of a public grade. It never changes the score or the order.
type Confidence struct {
	Low     bool
	Reasons []ConfidenceReason
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
	// Confidence is nil when the flag is unknown (grade stored before the flag existed).
	Confidence *Confidence
	// ScoreChange is the score now minus the score DynamicsDays ago, PlaceChange the places gained
	// (positive) or lost since then; nil when the repo was not graded back then ("new"), and
	// PlaceChange is also nil unless the list is sorted by score.
	ScoreChange *int32
	PlaceChange *int32
}

// RepoAnalytics is a repo's stored informational analytics (bus factor, ownership map) and the time
// of the grade it describes. Analytics is empty when there are none yet.
type RepoAnalytics struct {
	Analytics []byte
	GradedAt  *time.Time
}

// PublicCategory is one category of a public repo's stored grade; nil Score means no data.
type PublicCategory struct {
	ID    string
	Score *int32
}

// PublicSummary is a public repo's stored grade for the public score API, badges and widget.
// A repo that has not been graded yet has a nil Score and no Categories.
type PublicSummary struct {
	Name        string
	WebURL      string
	Score       *int32
	Categories  []PublicCategory
	MissingData []string
	GradedAt    *time.Time
	Confidence  *Confidence
}

// HistoryPoint is one stored public grading of a repo; a nil category Score means no data and a
// nil LowConfidence means the flag was unknown for that grading.
type HistoryPoint struct {
	GradedAt      time.Time
	Score         int32
	Categories    []PublicCategory
	LowConfidence *bool
}

// RankNeighbour is the repo next to the explained one; Points is the gap between the two scores.
type RankNeighbour struct {
	Place   int32
	OrgSlug string
	Name    string
	Score   int32
	Points  int32
}

// CategoryComparison compares a category with the top-10 median; the median and gap are nil when
// too few top repos have the category.
type CategoryComparison struct {
	ID          string
	Score       int32
	Top10Median *int32
	Gap         *int32
}

// PositionExplanation explains a public repo's place in the default ranking. Ranked is false for
// a public repo that has no grade yet.
type PositionExplanation struct {
	Ranked          bool
	Place           int32
	Total           int32
	Score           int32
	Above           *RankNeighbour
	Below           *RankNeighbour
	Categories      []CategoryComparison
	WeakestCategory string
}

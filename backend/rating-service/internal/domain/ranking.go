package domain

import (
	"context"
	"time"
)

type SortBy string

const (
	SortByScore        SortBy = "score"
	SortByLastActivity SortBy = "last_activity"
	SortByLikes        SortBy = "likes"
)

// RankingRequest is a page request as received from the transport layer, before defaults.
type RankingRequest struct {
	Page     int
	PageSize int
	Language string
	SortBy   SortBy
	// HideLowConfidence leaves out repos whose public grade is flagged low-confidence.
	HideLowConfidence bool
}

// RankingQuery is what the repository executes: defaults applied, page turned into limit/offset.
type RankingQuery struct {
	Language          string
	SortBy            SortBy
	HideLowConfidence bool
	Limit             int
	Offset            int
	// DynamicsDays is the window of the score and place change (RANKING_DYNAMICS_DAYS).
	DynamicsDays int
}

type RankingRow struct {
	SourcecraftUUID string
	Name            string
	WebURL          string
	PublicScore     *int16
	Likes           *float64
	Language        Language
	LastActivityAt  *time.Time
	// Confidence is the low-confidence flag of the public grade; nil when unknown (no flag row).
	Confidence *Confidence

	// PastScore and PastPlace are the repo's score and place in the same filtered list
	// DynamicsDays ago, as the repository found them; nil when it had no grade back then. The
	// service turns them into ScoreChange and PlaceChange.
	PastScore *int16
	PastPlace *int
	// ScoreChange is the score now minus the past score; PlaceChange is places gained (positive)
	// or lost (negative) since then, set only when sorting by score.
	ScoreChange *int
	PlaceChange *int
}

// ConfidenceReason is one generic pre-written reason a public grade is flagged low-confidence.
type ConfidenceReason struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// Confidence is a public grade's low-confidence flag. It never changes the score or the order.
type Confidence struct {
	Low     bool
	Reasons []ConfidenceReason
}

type Language struct {
	Name  string
	Color string
}

type RankingEntry struct {
	Place int
	RankingRow
}

type RankingPage struct {
	Entries      []RankingEntry
	TotalCount   int
	Page         int
	PageSize     int
	DynamicsDays int
}

// CategoryScore is one category of a stored grade; nil Score means the category had no data.
type CategoryScore struct {
	ID    string
	Score *int
}

// PublicSummaryRow is a public repo's stored grade as the database holds it; CategoryScores is
// grade-service's raw category_scores JSON.
type PublicSummaryRow struct {
	Name           string
	WebURL         string
	Score          *int16
	CategoryScores []byte
	MissingData    []string
	GradedAt       *time.Time
	Confidence     *Confidence
}

// PublicSummary is a public repo's stored grade with its category scores decoded and sorted.
type PublicSummary struct {
	Name        string
	WebURL      string
	Score       *int16
	Categories  []CategoryScore
	MissingData []string
	GradedAt    *time.Time
	Confidence  *Confidence
}

// HistoryRow is one stored public grading as the database holds it.
type HistoryRow struct {
	GradedAt       time.Time
	Score          int16
	CategoryScores []byte
	LowConfidence  *bool
}

// HistoryPoint is a history row with its category scores decoded.
type HistoryPoint struct {
	GradedAt      time.Time
	Score         int
	Categories    []CategoryScore
	LowConfidence *bool
}

// RankedRow is a graded public repo in the default ranking order (score, then recent activity).
type RankedRow struct {
	OrgSlug        string
	Name           string
	Score          int
	CategoryScores []byte
}

// Neighbour is the repo next to the explained one; Points is the gap between the two scores.
type Neighbour struct {
	Place   int
	OrgSlug string
	Name    string
	Score   int
	Points  int
}

// CategoryComparison compares one category of the repo with the top-10 median; Top10Median and
// Gap are nil when fewer than three of the top repos have the category.
type CategoryComparison struct {
	ID          string
	Score       int
	Top10Median *int
	Gap         *int
}

// PositionExplanation explains a place in the default ranking. Ranked is false for a public repo
// that has no grade yet; nothing else is set then.
type PositionExplanation struct {
	Ranked          bool
	Place           int
	Total           int
	Score           int
	Above           *Neighbour
	Below           *Neighbour
	Categories      []CategoryComparison
	WeakestCategory string
}

type RankingRepository interface {
	CountRanking(ctx context.Context, language string, hideLowConfidence bool) (int, error)
	ListRanking(ctx context.Context, query RankingQuery) ([]RankingRow, error)
	// GetPublicScore returns the public score of a public repo (nil when not graded yet), or
	// ErrNotFound when the repo is unknown or not public.
	GetPublicScore(ctx context.Context, orgSlug, repoSlug string) (*int16, error)
	// GetPublicSummary returns the stored public grade of a public repo (nil score and empty
	// categories when not graded yet), or ErrNotFound when the repo is unknown or not public.
	GetPublicSummary(ctx context.Context, orgSlug, repoSlug string) (PublicSummaryRow, error)
	// ListScoreHistory returns the latest limit stored public gradings of a public repo, oldest
	// first, or ErrNotFound when the repo is unknown or not public.
	ListScoreHistory(ctx context.Context, orgSlug, repoSlug string, limit int) ([]HistoryRow, error)
	// ListRanked returns every graded public repo in the default ranking order.
	ListRanked(ctx context.Context) ([]RankedRow, error)
}

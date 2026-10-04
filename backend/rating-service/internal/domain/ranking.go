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
	Entries    []RankingEntry
	TotalCount int
	Page       int
	PageSize   int
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

type RankingRepository interface {
	CountRanking(ctx context.Context, language string, hideLowConfidence bool) (int, error)
	ListRanking(ctx context.Context, query RankingQuery) ([]RankingRow, error)
	// GetPublicScore returns the public score of a public repo (nil when not graded yet), or
	// ErrNotFound when the repo is unknown or not public.
	GetPublicScore(ctx context.Context, orgSlug, repoSlug string) (*int16, error)
	// GetPublicSummary returns the stored public grade of a public repo (nil score and empty
	// categories when not graded yet), or ErrNotFound when the repo is unknown or not public.
	GetPublicSummary(ctx context.Context, orgSlug, repoSlug string) (PublicSummaryRow, error)
}

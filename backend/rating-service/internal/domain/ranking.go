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
}

// RankingQuery is what the repository executes: defaults applied, page turned into limit/offset.
type RankingQuery struct {
	Language string
	SortBy   SortBy
	Limit    int
	Offset   int
}

type RankingRow struct {
	SourcecraftUUID string
	Name            string
	WebURL          string
	PublicScore     *int16
	Likes           *float64
	Language        Language
	LastActivityAt  *time.Time
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

type RankingRepository interface {
	CountRanking(ctx context.Context, language string) (int, error)
	ListRanking(ctx context.Context, query RankingQuery) ([]RankingRow, error)
}

package domain

import (
	"context"
	"time"
)

// Level boundaries of a public score, the same as the gateway's /score levels and the badge colors.
const (
	LevelExcellent = "excellent"
	LevelHealthy   = "healthy"
	LevelAttention = "attention"
	LevelAtRisk    = "at_risk"

	ExcellentFrom = 90
	HealthyFrom   = 75
	AttentionFrom = 60
)

// AnalyticsParams are the limits of the platform analytics; zero values mean the defaults.
type AnalyticsParams struct {
	// MinLanguageRepos hides languages with fewer graded repos: an average over one or two repos
	// says nothing about the language.
	MinLanguageRepos int
	TopLanguages     int
	TopAdvice        int
	TrendWeeks       int
	// ImprovedDays is the window of the "most improved" list; ImprovedLimit its length.
	ImprovedDays  int
	ImprovedLimit int
}

type ScoreBucket struct {
	MinScore, MaxScore int
	Repos              int
}

type LevelCount struct {
	Level string
	Repos int
}

type LanguageStat struct {
	Name, Color string
	Repos       int
	AvgScore    float64
}

type CategoryAverage struct {
	ID       string
	AvgScore float64
	Repos    int
}

type AdviceStat struct {
	CategoryID, MetricID, Priority string
	Repos                          int
}

type TrendPoint struct {
	WeekStart time.Time
	AvgScore  float64
	Repos     int
}

type ImprovedRepo struct {
	OrgSlug, Name            string
	Score, PastScore, Change int
}

// PlatformAnalytics aggregates the public grades only; private grades are never read.
type PlatformAnalytics struct {
	GradedRepos  int
	AvgScore     float64
	MedianScore  float64
	Distribution []ScoreBucket
	Levels       []LevelCount
	Languages    []LanguageStat
	Categories   []CategoryAverage
	TopAdvice    []AdviceStat
	Trend        []TrendPoint
	MostImproved []ImprovedRepo
	ImprovedDays int
	MinLanguage  int
	GeneratedAt  time.Time
}

// PublicGradeRow is one public grade as the database holds it, for the export.
type PublicGradeRow struct {
	OrgSlug        string
	Name           string
	WebURL         string
	Score          int16
	Language       string
	Likes          *float64
	LastActivityAt *time.Time
	GradedAt       *time.Time
	LowConfidence  *bool
	CategoryScores []byte
}

// PublicGrade is an export entry with the category scores decoded.
type PublicGrade struct {
	OrgSlug        string
	Name           string
	WebURL         string
	Score          int
	Language       string
	Likes          *float64
	LastActivityAt *time.Time
	GradedAt       *time.Time
	LowConfidence  *bool
	Categories     []CategoryScore
}

type PublicGradesPage struct {
	Entries    []PublicGrade
	TotalCount int
	Page       int
	PageSize   int
}

// AnalyticsRepository is the read-only data access of the analytics tab and the export.
type AnalyticsRepository interface {
	// GetPlatformAnalytics aggregates every graded public repo; params carry already-defaulted limits.
	GetPlatformAnalytics(ctx context.Context, params AnalyticsParams) (PlatformAnalytics, error)
	// CountPublicGrades counts the graded public repos.
	CountPublicGrades(ctx context.Context) (int, error)
	// ListPublicGrades returns one page of public grades ordered by org and repo slug.
	ListPublicGrades(ctx context.Context, limit, offset int) ([]PublicGradeRow, error)
}

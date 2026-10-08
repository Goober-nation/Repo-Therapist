package domain

import "time"

// PlatformStats are the aggregate statistics of all graded public repositories. Public grades only.
type PlatformStats struct {
	GradedRepos  int32
	AvgScore     float64
	MedianScore  float64
	Distribution []ScoreBucket
	Levels       []LevelCount
	Languages    []LanguageStat
	Categories   []CategoryAverage
	TopAdvice    []AdviceStat
	Trend        []TrendPoint
	MostImproved []ImprovedRepo
	ImprovedDays int32
	MinLanguage  int32
	GeneratedAt  *time.Time
}

type ScoreBucket struct {
	Min, Max, Repos int32
}

type LevelCount struct {
	Level string
	Repos int32
}

type LanguageStat struct {
	Name, Color string
	Repos       int32
	AvgScore    float64
}

type CategoryAverage struct {
	ID       string
	AvgScore float64
	Repos    int32
}

type AdviceStat struct {
	CategoryID, MetricID, Priority string
	Repos                          int32
}

type TrendPoint struct {
	WeekStart *time.Time
	AvgScore  float64
	Repos     int32
}

type ImprovedRepo struct {
	OrgSlug, Name            string
	Score, PastScore, Change int32
}

// ExportedGrade is one public grade of the export; a nil category Score means no data.
type ExportedGrade struct {
	OrgSlug        string
	Repo           string
	WebURL         string
	Score          int32
	Language       string
	Likes          *float64
	LastActivityAt *time.Time
	GradedAt       *time.Time
	LowConfidence  *bool
	Categories     []PublicCategory
}

// ExportPage is one page of the export; Total is the number of graded public repos.
type ExportPage struct {
	Grades []ExportedGrade
	Total  int32
}

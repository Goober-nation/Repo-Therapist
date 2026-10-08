package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"rating-service/internal/domain"
)

const (
	DefaultAnalyticsCacheTTL = 5 * time.Minute

	// Limits of the platform analytics.
	minLanguageRepos = 3
	topLanguages     = 20
	topAdvice        = 10
	trendWeeks       = 12
	improvedLimit    = 10

	DefaultExportPageSize = 1000
	MaxExportPageSize     = 1000
)

// analyticsCache keeps the last aggregate for ttl: the aggregates scan every public grade, and the
// tab is public, so one computation serves all callers of the next few minutes.
type analyticsCache struct {
	mu      sync.Mutex
	value   domain.PlatformAnalytics
	expires time.Time
}

// SetAnalytics attaches the analytics and export data source; cacheTTL below 1 means the default.
// CacheRecorder counts analytics requests answered from the cache or computed, for monitoring.
type CacheRecorder interface {
	AnalyticsCache(hit bool)
}

// SetCacheRecorder turns on the cache counters; nil leaves them off.
func (s *RatingService) SetCacheRecorder(r CacheRecorder) { s.cacheRecorder = r }

func (s *RatingService) SetAnalytics(repo domain.AnalyticsRepository, cacheTTL time.Duration) {
	if cacheTTL < 1 {
		cacheTTL = DefaultAnalyticsCacheTTL
	}

	s.analytics = repo
	s.analyticsTTL = cacheTTL
}

// GetPlatformAnalytics returns the aggregate statistics of all graded public repos, cached for the
// configured TTL. An error is not cached.
func (s *RatingService) GetPlatformAnalytics(ctx context.Context) (domain.PlatformAnalytics, error) {
	if s.analytics == nil {
		return domain.PlatformAnalytics{}, fmt.Errorf("%w: analytics are not configured", domain.ErrInvalidArgument)
	}

	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()

	if time.Now().Before(s.cache.expires) {
		if s.cacheRecorder != nil {
			s.cacheRecorder.AnalyticsCache(true)
		}

		return s.cache.value, nil
	}

	if s.cacheRecorder != nil {
		s.cacheRecorder.AnalyticsCache(false)
	}

	value, err := s.analytics.GetPlatformAnalytics(ctx, domain.AnalyticsParams{
		MinLanguageRepos: minLanguageRepos,
		TopLanguages:     topLanguages,
		TopAdvice:        topAdvice,
		TrendWeeks:       trendWeeks,
		ImprovedDays:     s.dynamicsDays,
		ImprovedLimit:    improvedLimit,
	})
	if err != nil {
		return domain.PlatformAnalytics{}, err
	}

	s.cache.value, s.cache.expires = value, time.Now().Add(s.analyticsTTL)

	return value, nil
}

// ListPublicGrades returns one page of public grades for the export; page and page size default
// and are capped like the ranking's.
func (s *RatingService) ListPublicGrades(ctx context.Context, page, pageSize int) (domain.PublicGradesPage, error) {
	if s.analytics == nil {
		return domain.PublicGradesPage{}, fmt.Errorf("%w: analytics are not configured", domain.ErrInvalidArgument)
	}

	if page < 0 || pageSize < 0 {
		return domain.PublicGradesPage{}, fmt.Errorf("%w: page and page_size must not be negative", domain.ErrInvalidArgument)
	}

	if page == 0 {
		page = 1
	}

	if pageSize == 0 {
		pageSize = DefaultExportPageSize
	}

	if pageSize > MaxExportPageSize {
		pageSize = MaxExportPageSize
	}

	total, err := s.analytics.CountPublicGrades(ctx)
	if err != nil {
		return domain.PublicGradesPage{}, err
	}

	rows, err := s.analytics.ListPublicGrades(ctx, pageSize, (page-1)*pageSize)
	if err != nil {
		return domain.PublicGradesPage{}, err
	}

	result := domain.PublicGradesPage{
		Entries:    make([]domain.PublicGrade, 0, len(rows)),
		TotalCount: total,
		Page:       page,
		PageSize:   pageSize,
	}

	for _, row := range rows {
		categories, err := decodeCategories(row.CategoryScores)
		if err != nil {
			return domain.PublicGradesPage{}, err
		}

		result.Entries = append(result.Entries, domain.PublicGrade{
			OrgSlug:        row.OrgSlug,
			Name:           row.Name,
			WebURL:         row.WebURL,
			Score:          int(row.Score),
			Language:       row.Language,
			Likes:          row.Likes,
			LastActivityAt: row.LastActivityAt,
			GradedAt:       row.GradedAt,
			LowConfidence:  row.LowConfidence,
			Categories:     categories,
		})
	}

	return result, nil
}

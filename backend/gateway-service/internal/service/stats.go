package service

import (
	"context"
	"fmt"

	"gateway-service/internal/domain"
	"gateway-service/internal/transport/rpc/gen/rank"
)

// exportPageSize is how many grades one Rating call returns while the export streams.
const exportPageSize = 1000

// SetExportLimiter sets the limiter of the public grades export. It has its own, much smaller
// budget than the JSON API: one export reads every public grade.
func (s *GatewayService) SetExportLimiter(limiter Limiter) {
	s.exportLimiter = limiter
}

// GetPlatformStats returns the aggregate statistics of the graded public repos, behind the public
// API's per-IP limit. It only reads.
func (s *GatewayService) GetPlatformStats(ctx context.Context, clientIP string) (domain.PlatformStats, error) {
	if err := s.allowPublicAPI(ctx, clientIP); err != nil {
		return domain.PlatformStats{}, err
	}

	rpcCtx, cancel := context.WithTimeout(ctx, s.ratingTimeout)
	defer cancel()

	resp, err := s.ratingClient.GetPlatformAnalytics(rpcCtx, &rank.GetPlatformAnalyticsRequest{})
	if err != nil {
		return domain.PlatformStats{}, rpcError("get platform analytics", err)
	}

	return mapPlatformStats(resp), nil
}

func mapPlatformStats(resp *rank.GetPlatformAnalyticsResponse) domain.PlatformStats {
	stats := domain.PlatformStats{
		GradedRepos:  resp.GetGradedRepos(),
		AvgScore:     resp.GetAvgScore(),
		MedianScore:  resp.GetMedianScore(),
		Distribution: []domain.ScoreBucket{},
		Levels:       []domain.LevelCount{},
		Languages:    []domain.LanguageStat{},
		Categories:   []domain.CategoryAverage{},
		TopAdvice:    []domain.AdviceStat{},
		Trend:        []domain.TrendPoint{},
		MostImproved: []domain.ImprovedRepo{},
		ImprovedDays: resp.GetImprovedDays(),
		MinLanguage:  resp.GetMinLanguageRepos(),
	}

	if resp.GetGeneratedAt() != nil {
		generated := resp.GetGeneratedAt().AsTime()
		stats.GeneratedAt = &generated
	}

	for _, bucket := range resp.GetDistribution() {
		stats.Distribution = append(stats.Distribution, domain.ScoreBucket{
			Min: bucket.GetMinScore(), Max: bucket.GetMaxScore(), Repos: bucket.GetRepos(),
		})
	}

	for _, level := range resp.GetLevels() {
		stats.Levels = append(stats.Levels, domain.LevelCount{Level: level.GetLevel(), Repos: level.GetRepos()})
	}

	for _, language := range resp.GetLanguages() {
		stats.Languages = append(stats.Languages, domain.LanguageStat{
			Name: language.GetName(), Color: language.GetColor(), Repos: language.GetRepos(), AvgScore: language.GetAvgScore(),
		})
	}

	for _, category := range resp.GetCategories() {
		stats.Categories = append(stats.Categories, domain.CategoryAverage{
			ID: category.GetId(), AvgScore: category.GetAvgScore(), Repos: category.GetRepos(),
		})
	}

	for _, advice := range resp.GetTopAdvice() {
		stats.TopAdvice = append(stats.TopAdvice, domain.AdviceStat{
			CategoryID: advice.GetCategoryId(), MetricID: advice.GetMetricId(),
			Priority: advice.GetPriority(), Repos: advice.GetRepos(),
		})
	}

	for _, point := range resp.GetTrend() {
		trend := domain.TrendPoint{AvgScore: point.GetAvgScore(), Repos: point.GetRepos()}
		if point.GetWeekStart() != nil {
			week := point.GetWeekStart().AsTime()
			trend.WeekStart = &week
		}

		stats.Trend = append(stats.Trend, trend)
	}

	for _, repo := range resp.GetMostImproved() {
		stats.MostImproved = append(stats.MostImproved, domain.ImprovedRepo{
			OrgSlug: repo.GetOrgSlug(), Name: repo.GetName(), Score: repo.GetScore(),
			PastScore: repo.GetPastScore(), Change: repo.GetChange(),
		})
	}

	return stats
}

// ExportPublicGrades streams every public grade page by page to emit, after one check of the export
// limiter. An error before the first emit is returned as usual; one after it means the stream is
// already partly written, which the caller must not present as a complete export.
func (s *GatewayService) ExportPublicGrades(ctx context.Context, clientIP string, emit func(domain.ExportPage) error) error {
	if err := s.allowExport(ctx, clientIP); err != nil {
		return err
	}

	for page := int32(1); ; page++ {
		rpcCtx, cancel := context.WithTimeout(ctx, s.ratingTimeout)
		resp, err := s.ratingClient.ListPublicGrades(rpcCtx, &rank.ListPublicGradesRequest{Page: page, PageSize: exportPageSize})
		cancel()

		if err != nil {
			return rpcError("list public grades", err)
		}

		if err := emit(mapExportPage(resp)); err != nil {
			return err
		}

		if int64(page)*int64(resp.GetPageSize()) >= int64(resp.GetTotalCount()) || len(resp.GetEntries()) == 0 {
			return nil
		}
	}
}

func (s *GatewayService) allowExport(ctx context.Context, clientIP string) error {
	if s.exportLimiter == nil {
		return nil
	}

	allowed, err := s.exportLimiter.Allow(ctx, "rate_limit:gateway:export:ip:"+clientIP)
	if err != nil {
		return fmt.Errorf("check export rate limit: %w", err)
	}

	if !allowed {
		return domain.ErrLimitExceeded
	}

	return nil
}

func mapExportPage(resp *rank.ListPublicGradesResponse) domain.ExportPage {
	page := domain.ExportPage{Total: resp.GetTotalCount(), Grades: make([]domain.ExportedGrade, 0, len(resp.GetEntries()))}

	for _, entry := range resp.GetEntries() {
		grade := domain.ExportedGrade{
			OrgSlug:       entry.GetOrgSlug(),
			Repo:          entry.GetName(),
			WebURL:        entry.GetWebUrl(),
			Score:         entry.GetScore(),
			Language:      entry.GetLanguage(),
			Likes:         entry.Likes,
			LowConfidence: entry.LowConfidence,
			Categories:    make([]domain.PublicCategory, 0, len(entry.GetCategories())),
		}

		if entry.GetLastActivityAt() != nil {
			at := entry.GetLastActivityAt().AsTime()
			grade.LastActivityAt = &at
		}

		if entry.GetGradedAt() != nil {
			at := entry.GetGradedAt().AsTime()
			grade.GradedAt = &at
		}

		for _, category := range entry.GetCategories() {
			grade.Categories = append(grade.Categories, domain.PublicCategory{ID: category.GetId(), Score: category.Score})
		}

		page.Grades = append(page.Grades, grade)
	}

	return page
}

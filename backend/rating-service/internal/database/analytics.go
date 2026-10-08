package database

import (
	"context"
	"fmt"
	"math"
	"time"

	"rating-service/internal/domain"
)

// publicGraded is the shared FROM of every aggregate: graded public repos only. Private grades live
// in another table and are never joined, so no aggregate can leak them.
const publicGraded = `
	FROM repos r
	JOIN repo_grades_public g ON g.sourcecraft_uuid = r.sourcecraft_uuid
	WHERE r.visibility = 'public' AND g.score IS NOT NULL`

// GetPlatformAnalytics reads the aggregates in separate queries: each is small and independent, and
// the service caches the result, so one slow query does not multiply across requests.
func (r *RankingRepository) GetPlatformAnalytics(ctx context.Context, p domain.AnalyticsParams) (domain.PlatformAnalytics, error) {
	result := domain.PlatformAnalytics{ImprovedDays: p.ImprovedDays, MinLanguage: p.MinLanguageRepos}

	if err := r.readOverview(ctx, &result); err != nil {
		return domain.PlatformAnalytics{}, err
	}

	if err := r.readLanguages(ctx, p, &result); err != nil {
		return domain.PlatformAnalytics{}, err
	}

	if err := r.readCategories(ctx, &result); err != nil {
		return domain.PlatformAnalytics{}, err
	}

	if err := r.readAdvice(ctx, p, &result); err != nil {
		return domain.PlatformAnalytics{}, err
	}

	if err := r.readTrend(ctx, p, &result); err != nil {
		return domain.PlatformAnalytics{}, err
	}

	if err := r.readImproved(ctx, p, &result); err != nil {
		return domain.PlatformAnalytics{}, err
	}

	result.GeneratedAt = time.Now().UTC()

	return result, nil
}

// readOverview fills the totals, the ten-point distribution and the level counts in one pass.
func (r *RankingRepository) readOverview(ctx context.Context, out *domain.PlatformAnalytics) error {
	var (
		avg, median                   *float64
		excellent, healthy, attention int
		atRisk                        int
		buckets                       [10]int
	)

	err := r.db.QueryRow(ctx, fmt.Sprintf(`
		SELECT count(*), avg(g.score), percentile_cont(0.5) WITHIN GROUP (ORDER BY g.score),
			count(*) FILTER (WHERE g.score >= %[1]d),
			count(*) FILTER (WHERE g.score >= %[2]d AND g.score < %[1]d),
			count(*) FILTER (WHERE g.score >= %[3]d AND g.score < %[2]d),
			count(*) FILTER (WHERE g.score < %[3]d),
			count(*) FILTER (WHERE g.score < 10), count(*) FILTER (WHERE g.score BETWEEN 10 AND 19),
			count(*) FILTER (WHERE g.score BETWEEN 20 AND 29), count(*) FILTER (WHERE g.score BETWEEN 30 AND 39),
			count(*) FILTER (WHERE g.score BETWEEN 40 AND 49), count(*) FILTER (WHERE g.score BETWEEN 50 AND 59),
			count(*) FILTER (WHERE g.score BETWEEN 60 AND 69), count(*) FILTER (WHERE g.score BETWEEN 70 AND 79),
			count(*) FILTER (WHERE g.score BETWEEN 80 AND 89), count(*) FILTER (WHERE g.score >= 90)
		%[4]s`,
		domain.ExcellentFrom, domain.HealthyFrom, domain.AttentionFrom, publicGraded),
	).Scan(&out.GradedRepos, &avg, &median, &excellent, &healthy, &attention, &atRisk,
		&buckets[0], &buckets[1], &buckets[2], &buckets[3], &buckets[4],
		&buckets[5], &buckets[6], &buckets[7], &buckets[8], &buckets[9])
	if err != nil {
		return fmt.Errorf("read analytics overview: %w", err)
	}

	if avg != nil {
		out.AvgScore = round1(*avg)
	}

	if median != nil {
		out.MedianScore = round1(*median)
	}

	out.Levels = []domain.LevelCount{
		{Level: domain.LevelExcellent, Repos: excellent},
		{Level: domain.LevelHealthy, Repos: healthy},
		{Level: domain.LevelAttention, Repos: attention},
		{Level: domain.LevelAtRisk, Repos: atRisk},
	}

	out.Distribution = make([]domain.ScoreBucket, 0, len(buckets))

	for i, repos := range buckets {
		bucket := domain.ScoreBucket{MinScore: i * 10, MaxScore: i*10 + 9, Repos: repos}
		if i == len(buckets)-1 {
			bucket.MaxScore = 100
		}

		out.Distribution = append(out.Distribution, bucket)
	}

	return nil
}

func (r *RankingRepository) readLanguages(ctx context.Context, p domain.AnalyticsParams, out *domain.PlatformAnalytics) error {
	rows, err := r.db.Query(ctx, `
		SELECT r.main_language, COALESCE(max(r.language_color), ''), count(*), avg(g.score)
		FROM repos r
		JOIN repo_grades_public g ON g.sourcecraft_uuid = r.sourcecraft_uuid
		WHERE r.visibility = 'public' AND g.score IS NOT NULL AND COALESCE(r.main_language, '') <> ''
		GROUP BY r.main_language
		HAVING count(*) >= $1
		ORDER BY count(*) DESC, avg(g.score) DESC, r.main_language
		LIMIT $2`, p.MinLanguageRepos, p.TopLanguages)
	if err != nil {
		return fmt.Errorf("read analytics languages: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			stat domain.LanguageStat
			avg  float64
		)

		if err := rows.Scan(&stat.Name, &stat.Color, &stat.Repos, &avg); err != nil {
			return fmt.Errorf("scan analytics language: %w", err)
		}

		stat.AvgScore = round1(avg)
		out.Languages = append(out.Languages, stat)
	}

	return rows.Err()
}

// readCategories averages each category over the repos that have data in it: a no-data category is
// skipped, never counted as zero. The CASE keeps a malformed (non-object) value from failing the query.
func (r *RankingRepository) readCategories(ctx context.Context, out *domain.PlatformAnalytics) error {
	rows, err := r.db.Query(ctx, `
		SELECT c.key, avg((c.value->>'score')::float8), count(*)
		FROM repos r
		JOIN repo_grades_public g ON g.sourcecraft_uuid = r.sourcecraft_uuid
		CROSS JOIN LATERAL jsonb_each(CASE WHEN jsonb_typeof(g.category_scores) = 'object'
			THEN g.category_scores ELSE '{}'::jsonb END) c
		WHERE r.visibility = 'public' AND g.score IS NOT NULL
			AND jsonb_typeof(c.value) = 'object'
			AND NOT COALESCE((c.value->>'no_data')::boolean, false)
			AND c.value->>'score' IS NOT NULL
		GROUP BY c.key
		ORDER BY c.key`)
	if err != nil {
		return fmt.Errorf("read analytics categories: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			item domain.CategoryAverage
			avg  float64
		)

		if err := rows.Scan(&item.ID, &avg, &item.Repos); err != nil {
			return fmt.Errorf("scan analytics category: %w", err)
		}

		item.AvgScore = round1(avg)
		out.Categories = append(out.Categories, item)
	}

	return rows.Err()
}

// readAdvice counts how many repos carry each (category, metric) advice item. Advice stored before
// 2026-10 has no category_id/metric_id and is skipped.
func (r *RankingRepository) readAdvice(ctx context.Context, p domain.AnalyticsParams, out *domain.PlatformAnalytics) error {
	rows, err := r.db.Query(ctx, `
		SELECT a.value->>'category_id', a.value->>'metric_id',
			COALESCE(mode() WITHIN GROUP (ORDER BY a.value->>'priority'), ''), count(DISTINCT r.sourcecraft_uuid)
		FROM repos r
		JOIN repo_grades_public g ON g.sourcecraft_uuid = r.sourcecraft_uuid
		CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(g.advice) = 'array'
			THEN g.advice ELSE '[]'::jsonb END) a
		WHERE r.visibility = 'public' AND g.score IS NOT NULL
			AND COALESCE(a.value->>'category_id', '') <> '' AND COALESCE(a.value->>'metric_id', '') <> ''
		GROUP BY 1, 2
		ORDER BY 4 DESC, 1, 2
		LIMIT $1`, p.TopAdvice)
	if err != nil {
		return fmt.Errorf("read analytics advice: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item domain.AdviceStat
		if err := rows.Scan(&item.CategoryID, &item.MetricID, &item.Priority, &item.Repos); err != nil {
			return fmt.Errorf("scan analytics advice: %w", err)
		}

		out.TopAdvice = append(out.TopAdvice, item)
	}

	return rows.Err()
}

// readTrend averages, per week, the latest score of each repo graded in that week. It follows the
// repos graded that week rather than a fixed set, so it reflects what the platform measured then.
func (r *RankingRepository) readTrend(ctx context.Context, p domain.AnalyticsParams, out *domain.PlatformAnalytics) error {
	rows, err := r.db.Query(ctx, `
		WITH weekly AS (
			SELECT date_trunc('week', h.graded_at) AS week_start, h.sourcecraft_uuid,
				(array_agg(h.score ORDER BY h.graded_at DESC))[1] AS score
			FROM repo_grade_history h
			JOIN repos r ON r.sourcecraft_uuid = h.sourcecraft_uuid
			WHERE r.visibility = 'public'
				AND h.graded_at >= date_trunc('week', now()) - make_interval(weeks => $1::int)
			GROUP BY 1, 2
		)
		SELECT week_start, avg(score), count(*) FROM weekly GROUP BY week_start ORDER BY week_start`, p.TrendWeeks)
	if err != nil {
		return fmt.Errorf("read analytics trend: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			point domain.TrendPoint
			avg   float64
		)

		if err := rows.Scan(&point.WeekStart, &avg, &point.Repos); err != nil {
			return fmt.Errorf("scan analytics trend: %w", err)
		}

		point.AvgScore = round1(avg)
		out.Trend = append(out.Trend, point)
	}

	return rows.Err()
}

// readImproved lists the biggest score gains since ImprovedDays ago. A repo graded only recently has
// no earlier grade to compare with and is left out.
func (r *RankingRepository) readImproved(ctx context.Context, p domain.AnalyticsParams, out *domain.PlatformAnalytics) error {
	rows, err := r.db.Query(ctx, `
		WITH past AS (
			SELECT DISTINCT ON (h.sourcecraft_uuid) h.sourcecraft_uuid, h.score
			FROM repo_grade_history h
			WHERE h.graded_at <= now() - make_interval(days => $1::int)
			ORDER BY h.sourcecraft_uuid, h.graded_at DESC
		)
		SELECT r.org_slug, r.repo_slug, g.score, p.score, g.score - p.score
		FROM repos r
		JOIN repo_grades_public g ON g.sourcecraft_uuid = r.sourcecraft_uuid
		JOIN past p ON p.sourcecraft_uuid = r.sourcecraft_uuid
		WHERE r.visibility = 'public' AND g.score IS NOT NULL AND g.score > p.score
		ORDER BY g.score - p.score DESC, g.score DESC, r.org_slug, r.repo_slug
		LIMIT $2`, p.ImprovedDays, p.ImprovedLimit)
	if err != nil {
		return fmt.Errorf("read analytics improved: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item domain.ImprovedRepo
		if err := rows.Scan(&item.OrgSlug, &item.Name, &item.Score, &item.PastScore, &item.Change); err != nil {
			return fmt.Errorf("scan analytics improved: %w", err)
		}

		out.MostImproved = append(out.MostImproved, item)
	}

	return rows.Err()
}

func (r *RankingRepository) CountPublicGrades(ctx context.Context) (int, error) {
	var count int
	if err := r.db.QueryRow(ctx, "SELECT count(*) "+publicGraded).Scan(&count); err != nil {
		return 0, fmt.Errorf("count public grades: %w", err)
	}

	return count, nil
}

func (r *RankingRepository) ListPublicGrades(ctx context.Context, limit, offset int) ([]domain.PublicGradeRow, error) {
	rows, err := r.db.Query(ctx, `
		SELECT r.org_slug, r.repo_slug, COALESCE(r.web_url, ''), g.score, COALESCE(r.main_language, ''),
			r.sourcecraft_rating, r.last_activity_at, g.graded_at, c.low_confidence, g.category_scores
		FROM repos r
		JOIN repo_grades_public g ON g.sourcecraft_uuid = r.sourcecraft_uuid
		LEFT JOIN repo_confidence_public c ON c.sourcecraft_uuid = r.sourcecraft_uuid
		WHERE r.visibility = 'public' AND g.score IS NOT NULL
		ORDER BY r.org_slug, r.repo_slug
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list public grades: %w", err)
	}
	defer rows.Close()

	result := make([]domain.PublicGradeRow, 0, limit)

	for rows.Next() {
		var row domain.PublicGradeRow
		if err := rows.Scan(&row.OrgSlug, &row.Name, &row.WebURL, &row.Score, &row.Language,
			&row.Likes, &row.LastActivityAt, &row.GradedAt, &row.LowConfidence, &row.CategoryScores); err != nil {
			return nil, fmt.Errorf("scan public grade: %w", err)
		}

		result = append(result, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate public grades: %w", err)
	}

	return result, nil
}

func round1(value float64) float64 {
	return math.Round(value*10) / 10
}

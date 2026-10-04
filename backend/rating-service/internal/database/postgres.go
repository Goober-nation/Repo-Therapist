package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"rating-service/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// orderClauses is the only source of ORDER BY text — the requested sort picks an entry, user
// input never reaches the SQL string. Every sort is descending with NULLs last and falls back to
// public_score, so health stays the dominant ranking factor; slugs make the order stable.
var orderClauses = map[domain.SortBy]string{
	domain.SortByScore:        "g.score DESC NULLS LAST, r.last_activity_at DESC NULLS LAST, r.org_slug, r.repo_slug",
	domain.SortByLastActivity: "r.last_activity_at DESC NULLS LAST, g.score DESC NULLS LAST, r.org_slug, r.repo_slug",
	domain.SortByLikes:        "r.sourcecraft_rating DESC NULLS LAST, g.score DESC NULLS LAST, r.org_slug, r.repo_slug",
}

type RankingRepository struct {
	db *pgxpool.Pool
}

func NewRankingRepository(db *pgxpool.Pool) *RankingRepository {
	return &RankingRepository{
		db: db,
	}
}

func (r *RankingRepository) CountRanking(ctx context.Context, language string, hideLowConfidence bool) (int, error) {
	query, args := buildCountQuery(language, hideLowConfidence)

	var count int
	if err := r.db.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count ranking: %w", err)
	}

	return count, nil
}

func (r *RankingRepository) ListRanking(ctx context.Context, q domain.RankingQuery) ([]domain.RankingRow, error) {
	query, args, err := buildListQuery(q)
	if err != nil {
		return nil, err
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list ranking: %w", err)
	}
	defer rows.Close()

	result := make([]domain.RankingRow, 0, q.Limit)

	for rows.Next() {
		var (
			row     domain.RankingRow
			low     *bool
			reasons []byte
		)

		if err := rows.Scan(
			&row.SourcecraftUUID,
			&row.Name,
			&row.WebURL,
			&row.PublicScore,
			&row.Likes,
			&row.Language.Name,
			&row.Language.Color,
			&row.LastActivityAt,
			&low,
			&reasons,
		); err != nil {
			return nil, fmt.Errorf("scan ranking row: %w", err)
		}

		if row.Confidence, err = decodeConfidence(low, reasons); err != nil {
			return nil, err
		}

		result = append(result, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ranking rows: %w", err)
	}

	return result, nil
}

func (r *RankingRepository) GetPublicScore(ctx context.Context, orgSlug, repoSlug string) (*int16, error) {
	var score *int16

	err := r.db.QueryRow(ctx, `
		SELECT g.score
		FROM repos r
		LEFT JOIN repo_grades_public g ON g.sourcecraft_uuid = r.sourcecraft_uuid
		WHERE r.org_slug = $1 AND r.repo_slug = $2 AND r.visibility = 'public'
		`,
		orgSlug,
		repoSlug,
	).Scan(&score)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get public score: %w", err)
	}

	return score, nil
}

// GetPublicSummary reads a public repo's stored public grade. A repo without a grade row still
// comes back (nil score), so the caller can tell "not graded yet" from "unknown".
func (r *RankingRepository) GetPublicSummary(ctx context.Context, orgSlug, repoSlug string) (domain.PublicSummaryRow, error) {
	var (
		row     domain.PublicSummaryRow
		low     *bool
		reasons []byte
	)

	err := r.db.QueryRow(ctx, `
		SELECT r.repo_slug, COALESCE(r.web_url, ''), g.score, g.category_scores, g.missing_data, g.graded_at,
			c.low_confidence, c.reasons
		FROM repos r
		LEFT JOIN repo_grades_public g ON g.sourcecraft_uuid = r.sourcecraft_uuid
		LEFT JOIN repo_confidence_public c ON c.sourcecraft_uuid = r.sourcecraft_uuid
		WHERE r.org_slug = $1 AND r.repo_slug = $2 AND r.visibility = 'public'
		`,
		orgSlug,
		repoSlug,
	).Scan(&row.Name, &row.WebURL, &row.Score, &row.CategoryScores, &row.MissingData, &row.GradedAt, &low, &reasons)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PublicSummaryRow{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.PublicSummaryRow{}, fmt.Errorf("get public summary: %w", err)
	}

	if row.Confidence, err = decodeConfidence(low, reasons); err != nil {
		return domain.PublicSummaryRow{}, err
	}

	return row, nil
}

// whereClause returns the shared filter for the count and list queries. Placeholders start at $1.
func whereClause(language string, hideLowConfidence bool) (string, []any) {
	var b strings.Builder
	var args []any

	// Only repos with a public grade are ranked; private-only or ungraded repos are left out.
	b.WriteString("WHERE r.visibility = 'public'" +
		" AND EXISTS (SELECT 1 FROM repo_grades_public pg WHERE pg.sourcecraft_uuid = r.sourcecraft_uuid)")

	if language != "" {
		args = append(args, language)
		fmt.Fprintf(&b, " AND lower(r.main_language) = lower($%d)", len(args))
	}

	// A missing flag row is "unknown" and is never hidden: only an explicit low-confidence flag is.
	if hideLowConfidence {
		b.WriteString(" AND NOT EXISTS (SELECT 1 FROM repo_confidence_public c" +
			" WHERE c.sourcecraft_uuid = r.sourcecraft_uuid AND c.low_confidence)")
	}

	return b.String(), args
}

// decodeConfidence builds the flag from its columns; a nil low means there is no flag row.
func decodeConfidence(low *bool, reasons []byte) (*domain.Confidence, error) {
	if low == nil {
		return nil, nil
	}

	confidence := &domain.Confidence{Low: *low, Reasons: []domain.ConfidenceReason{}}
	if len(reasons) > 0 {
		if err := json.Unmarshal(reasons, &confidence.Reasons); err != nil {
			return nil, fmt.Errorf("decode confidence reasons: %w", err)
		}
	}

	return confidence, nil
}

func buildCountQuery(language string, hideLowConfidence bool) (string, []any) {
	where, args := whereClause(language, hideLowConfidence)

	return "SELECT count(*) FROM repos r " + where, args
}

func buildListQuery(q domain.RankingQuery) (string, []any, error) {
	order, ok := orderClauses[q.SortBy]
	if !ok {
		return "", nil, fmt.Errorf("%w: unknown sort %q", domain.ErrInvalidArgument, q.SortBy)
	}

	where, args := whereClause(q.Language, q.HideLowConfidence)

	args = append(args, q.Limit, q.Offset)

	query := fmt.Sprintf(`
		SELECT
			r.sourcecraft_uuid::text,
			r.repo_slug,
			COALESCE(r.web_url, ''),
			g.score,
			r.sourcecraft_rating,
			COALESCE(r.main_language, ''),
			COALESCE(r.language_color, ''),
			r.last_activity_at,
			c.low_confidence,
			c.reasons
		FROM repos r
		JOIN repo_grades_public g ON g.sourcecraft_uuid = r.sourcecraft_uuid
		LEFT JOIN repo_confidence_public c ON c.sourcecraft_uuid = r.sourcecraft_uuid
		%s
		ORDER BY %s
		LIMIT $%d OFFSET $%d
		`,
		where,
		order,
		len(args)-1,
		len(args),
	)

	return query, args, nil
}

package database

import (
	"context"
	"fmt"
	"strings"

	"rating-service/internal/domain"

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

func (r *RankingRepository) CountRanking(ctx context.Context, language string) (int, error) {
	query, args := buildCountQuery(language)

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
		var row domain.RankingRow

		if err := rows.Scan(
			&row.SourcecraftUUID,
			&row.Name,
			&row.WebURL,
			&row.PublicScore,
			&row.Likes,
			&row.Language.Name,
			&row.Language.Color,
			&row.LastActivityAt,
		); err != nil {
			return nil, fmt.Errorf("scan ranking row: %w", err)
		}

		result = append(result, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ranking rows: %w", err)
	}

	return result, nil
}

// whereClause returns the shared filter for the count and list queries. Placeholders start at $1.
func whereClause(language string) (string, []any) {
	var b strings.Builder
	var args []any

	// Only repos with a public grade are ranked; private-only or ungraded repos are left out.
	b.WriteString("WHERE r.visibility = 'public'" +
		" AND EXISTS (SELECT 1 FROM repo_grades_public pg WHERE pg.sourcecraft_uuid = r.sourcecraft_uuid)")

	if language != "" {
		args = append(args, language)
		fmt.Fprintf(&b, " AND lower(r.main_language) = lower($%d)", len(args))
	}

	return b.String(), args
}

func buildCountQuery(language string) (string, []any) {
	where, args := whereClause(language)

	return "SELECT count(*) FROM repos r " + where, args
}

func buildListQuery(q domain.RankingQuery) (string, []any, error) {
	order, ok := orderClauses[q.SortBy]
	if !ok {
		return "", nil, fmt.Errorf("%w: unknown sort %q", domain.ErrInvalidArgument, q.SortBy)
	}

	where, args := whereClause(q.Language)

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
			r.last_activity_at
		FROM repos r
		JOIN repo_grades_public g ON g.sourcecraft_uuid = r.sourcecraft_uuid
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

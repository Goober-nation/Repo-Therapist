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

func (r *RankingRepository) CountRanking(ctx context.Context, language string, hideLowConfidence bool, extra domain.RankingExtras) (int, error) {
	query, args := buildCountQuery(language, hideLowConfidence, extra)

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
			&row.PastScore,
			&row.PastPlace,
			&row.GradeCount,
			&row.GradedAt,
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
func whereClause(language string, hideLowConfidence bool, extra domain.RankingExtras) (string, []any) {
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

	writeExtras(&b, &args, extra)

	return b.String(), args
}

// writeExtras adds the fact filters. Facts live in repo_facts_public; comparing a NULL (unknown)
// member to anything is never true, so an unknown fact matches neither "has" nor "lacks", and a repo
// without a facts row is simply left out of any facts filter.
func writeExtras(b *strings.Builder, args *[]any, extra domain.RankingExtras) {
	var conds []string

	add := func(format string, value any) {
		*args = append(*args, value)
		conds = append(conds, fmt.Sprintf(format, len(*args)))
	}

	switch extra.Size {
	case "small":
		add("f.loc < $%d", extra.SmallLOC)
	case "medium":
		add("f.loc >= $%d", extra.SmallLOC)
		add("f.loc < $%d", extra.LargeLOC)
	case "large":
		add("f.loc >= $%d", extra.LargeLOC)
	}

	if extra.HasCI != nil {
		add("f.has_ci = $%d", *extra.HasCI)
	}

	if extra.HasSecurityPolicy != nil {
		add("f.has_security_policy = $%d", *extra.HasSecurityPolicy)
	}

	if len(conds) > 0 {
		b.WriteString(" AND EXISTS (SELECT 1 FROM repo_facts_public f WHERE f.sourcecraft_uuid = r.sourcecraft_uuid AND " +
			strings.Join(conds, " AND ") + ")")
	}

	if topic := strings.TrimSpace(extra.Topic); topic != "" {
		*args = append(*args, "%"+likeEscaper.Replace(topic)+"%")
		fmt.Fprintf(b, " AND (r.repo_slug ILIKE $%[1]d ESCAPE '\\' OR COALESCE(r.description, '') ILIKE $%[1]d ESCAPE '\\')", len(*args))
	}
}

// likeEscaper makes the user's topic text literal inside a LIKE pattern.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

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

func buildCountQuery(language string, hideLowConfidence bool, extra domain.RankingExtras) (string, []any) {
	where, args := whereClause(language, hideLowConfidence, extra)

	return "SELECT count(*) FROM repos r " + where, args
}

func buildListQuery(q domain.RankingQuery) (string, []any, error) {
	order, ok := orderClauses[q.SortBy]
	if !ok {
		return "", nil, fmt.Errorf("%w: unknown sort %q", domain.ErrInvalidArgument, q.SortBy)
	}

	where, args := whereClause(q.Language, q.HideLowConfidence, q.Extra)

	args = append(args, q.DynamicsDays, q.Limit, q.Offset)
	daysArg, limitArg, offsetArg := len(args)-2, len(args)-1, len(args)

	// past: each repo of the current filtered list with its latest score from DynamicsDays ago,
	// ranked by that score with the same tie-breaks as the live score order. A repo graded only
	// recently has no row there and shows no change.
	query := fmt.Sprintf(`
		WITH filtered AS (
			SELECT r.sourcecraft_uuid, r.last_activity_at, r.org_slug, r.repo_slug
			FROM repos r
			%[1]s
		), past AS (
			SELECT DISTINCT ON (h.sourcecraft_uuid) h.sourcecraft_uuid, h.score
			FROM repo_grade_history h
			JOIN filtered f ON f.sourcecraft_uuid = h.sourcecraft_uuid
			WHERE h.graded_at <= now() - make_interval(days => $%[2]d::int)
			ORDER BY h.sourcecraft_uuid, h.graded_at DESC
		), past_rank AS (
			SELECT p.sourcecraft_uuid, p.score AS past_score,
				row_number() OVER (ORDER BY p.score DESC, f.last_activity_at DESC NULLS LAST, f.org_slug, f.repo_slug) AS past_place
			FROM past p
			JOIN filtered f ON f.sourcecraft_uuid = p.sourcecraft_uuid
		)
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
			c.reasons,
			pr.past_score,
			pr.past_place::int,
			(SELECT count(*) FROM repo_grade_history h WHERE h.sourcecraft_uuid = r.sourcecraft_uuid)::int,
			g.graded_at
		FROM repos r
		JOIN repo_grades_public g ON g.sourcecraft_uuid = r.sourcecraft_uuid
		LEFT JOIN repo_confidence_public c ON c.sourcecraft_uuid = r.sourcecraft_uuid
		LEFT JOIN past_rank pr ON pr.sourcecraft_uuid = r.sourcecraft_uuid
		%[1]s
		ORDER BY %[3]s
		LIMIT $%[4]d OFFSET $%[5]d
		`,
		where,
		daysArg,
		order,
		limitArg,
		offsetArg,
	)

	return query, args, nil
}

// ListScoreHistory returns the latest limit stored public gradings of a public repo, oldest first.
func (r *RankingRepository) ListScoreHistory(ctx context.Context, orgSlug, repoSlug string, limit int) ([]domain.HistoryRow, error) {
	var exists bool
	if err := r.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM repos WHERE org_slug = $1 AND repo_slug = $2 AND visibility = 'public')`,
		orgSlug, repoSlug,
	).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check repo for history: %w", err)
	}

	if !exists {
		return nil, domain.ErrNotFound
	}

	rows, err := r.db.Query(ctx, `
		SELECT h.graded_at, h.score, h.category_scores, h.low_confidence
		FROM repo_grade_history h
		JOIN repos r ON r.sourcecraft_uuid = h.sourcecraft_uuid
		WHERE r.org_slug = $1 AND r.repo_slug = $2 AND r.visibility = 'public'
		ORDER BY h.graded_at DESC
		LIMIT $3
		`,
		orgSlug, repoSlug, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list score history: %w", err)
	}
	defer rows.Close()

	var result []domain.HistoryRow

	for rows.Next() {
		var row domain.HistoryRow
		if err := rows.Scan(&row.GradedAt, &row.Score, &row.CategoryScores, &row.LowConfidence); err != nil {
			return nil, fmt.Errorf("scan history row: %w", err)
		}

		result = append(result, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate history rows: %w", err)
	}

	// Newest first came out of the query so LIMIT keeps the latest points; callers want oldest first.
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result, nil
}

// ListRanked returns every graded public repo in the default ranking order (the "score" sort).
func (r *RankingRepository) ListRanked(ctx context.Context) ([]domain.RankedRow, error) {
	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT r.org_slug, r.repo_slug, g.score, g.category_scores
		FROM repos r
		JOIN repo_grades_public g ON g.sourcecraft_uuid = r.sourcecraft_uuid
		WHERE r.visibility = 'public' AND g.score IS NOT NULL
		ORDER BY %s
		`, orderClauses[domain.SortByScore]))
	if err != nil {
		return nil, fmt.Errorf("list ranked: %w", err)
	}
	defer rows.Close()

	var result []domain.RankedRow

	for rows.Next() {
		var row domain.RankedRow
		if err := rows.Scan(&row.OrgSlug, &row.Name, &row.Score, &row.CategoryScores); err != nil {
			return nil, fmt.Errorf("scan ranked row: %w", err)
		}

		result = append(result, row)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ranked rows: %w", err)
	}

	return result, nil
}

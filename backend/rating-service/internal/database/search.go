package database

import (
	"context"
	"fmt"
	"strings"

	"rating-service/internal/domain"
)

// Search over public_repo_catalog. A name matches when it contains the query or is
// trigram-similar to it (pg_trgm's % operator, migration 000013). Matches are ordered exact name,
// exact repo slug, repo slug prefix, full name prefix, substring, fuzzy; then by similarity and
// SourceCraft rating. $1 is the lowercased query, $2 the same escaped for LIKE.

const repoNameExpr = "lower(c.org_slug || '/' || c.repo_slug)"

const repoMatch = `($1 = '' OR ` + repoNameExpr + ` LIKE '%' || $2 || '%' ESCAPE '\' OR ` + repoNameExpr + ` % $1)
	AND ($3 = '' OR lower(c.org_slug) = lower($3))`

const repoOrder = `CASE
		WHEN $1 = '' THEN 6
		WHEN ` + repoNameExpr + ` = $1 THEN 0
		WHEN lower(c.repo_slug) = $1 THEN 1
		WHEN lower(c.repo_slug) LIKE $2 || '%' ESCAPE '\' THEN 2
		WHEN ` + repoNameExpr + ` LIKE $2 || '%' ESCAPE '\' THEN 3
		WHEN ` + repoNameExpr + ` LIKE '%' || $2 || '%' ESCAPE '\' THEN 4
		ELSE 5
	END,
	similarity(` + repoNameExpr + `, $1) DESC,
	c.sourcecraft_rating DESC,
	c.org_slug, c.repo_slug`

const orgNameExpr = "lower(org_slug)"

const orgMatch = `$1 = '' OR ` + orgNameExpr + ` LIKE '%' || $2 || '%' ESCAPE '\' OR ` + orgNameExpr + ` % $1`

const orgOrder = `CASE
		WHEN $1 = '' THEN 4
		WHEN ` + orgNameExpr + ` = $1 THEN 0
		WHEN ` + orgNameExpr + ` LIKE $2 || '%' ESCAPE '\' THEN 1
		WHEN ` + orgNameExpr + ` LIKE '%' || $2 || '%' ESCAPE '\' THEN 2
		ELSE 3
	END,
	similarity(` + orgNameExpr + `, $1) DESC,
	count(*) DESC,
	org_slug`

// escapeLike makes s match itself literally in a LIKE pattern with ESCAPE '\'.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (r *RankingRepository) SearchRepos(ctx context.Context, query, org string, offset, limit int) ([]domain.CatalogRepo, int, error) {
	args := []any{query, escapeLike(query), org}

	var total int
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM public_repo_catalog c WHERE `+repoMatch, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count catalog repos: %w", err)
	}

	if total == 0 {
		return []domain.CatalogRepo{}, 0, nil
	}

	rows, err := r.db.Query(ctx, `
		SELECT c.sourcecraft_uuid::text, c.org_slug, c.repo_slug, COALESCE(c.description, ''),
			COALESCE(c.web_url, ''), COALESCE(c.main_language, ''), COALESCE(c.language_color, ''),
			c.sourcecraft_rating, c.last_activity_at, g.score
		FROM public_repo_catalog c
		LEFT JOIN repo_grades_public g ON g.sourcecraft_uuid = c.sourcecraft_uuid
		WHERE `+repoMatch+`
		ORDER BY `+repoOrder+`
		OFFSET $4 LIMIT $5`, append(args, offset, limit)...)
	if err != nil {
		return nil, 0, fmt.Errorf("search catalog repos: %w", err)
	}
	defer rows.Close()

	result := make([]domain.CatalogRepo, 0, limit)

	for rows.Next() {
		var repo domain.CatalogRepo

		if err := rows.Scan(
			&repo.SourcecraftUUID,
			&repo.OrgSlug,
			&repo.RepoSlug,
			&repo.Description,
			&repo.WebURL,
			&repo.Language.Name,
			&repo.Language.Color,
			&repo.Likes,
			&repo.LastActivityAt,
			&repo.PublicScore,
		); err != nil {
			return nil, 0, fmt.Errorf("scan catalog repo: %w", err)
		}

		result = append(result, repo)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate catalog repos: %w", err)
	}

	return result, total, nil
}

func (r *RankingRepository) SearchOrgs(ctx context.Context, query string, offset, limit int) ([]domain.CatalogOrg, int, error) {
	args := []any{query, escapeLike(query)}

	var total int
	if err := r.db.QueryRow(ctx, `SELECT count(DISTINCT org_slug) FROM public_repo_catalog WHERE `+orgMatch, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count catalog orgs: %w", err)
	}

	if total == 0 {
		return []domain.CatalogOrg{}, 0, nil
	}

	rows, err := r.db.Query(ctx, `
		SELECT org_slug, count(*)
		FROM public_repo_catalog
		WHERE `+orgMatch+`
		GROUP BY org_slug
		ORDER BY `+orgOrder+`
		OFFSET $3 LIMIT $4`, append(args, offset, limit)...)
	if err != nil {
		return nil, 0, fmt.Errorf("search catalog orgs: %w", err)
	}
	defer rows.Close()

	result := make([]domain.CatalogOrg, 0, limit)

	for rows.Next() {
		var org domain.CatalogOrg

		if err := rows.Scan(&org.Slug, &org.RepoCount); err != nil {
			return nil, 0, fmt.Errorf("scan catalog org: %w", err)
		}

		result = append(result, org)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate catalog orgs: %w", err)
	}

	return result, total, nil
}

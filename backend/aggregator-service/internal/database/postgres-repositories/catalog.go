package postgres_repositories

import (
	"context"
	"errors"
	"fmt"

	"aggregator-service/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CatalogRepository struct {
	db *pgxpool.Pool
}

func NewCatalogRepository(db *pgxpool.Pool) *CatalogRepository {
	return &CatalogRepository{db: db}
}

const catalogRefreshColumns = `id, started_at, finished_at, status, trigger, actor_hash, pages, repos, error`

func scanRefresh(row pgx.Row) (domain.CatalogRefresh, error) {
	var r domain.CatalogRefresh

	err := row.Scan(&r.ID, &r.StartedAt, &r.FinishedAt, &r.Status, &r.Trigger, &r.ActorHash, &r.Pages, &r.Repos, &r.Error)

	return r, err
}

func (r *CatalogRepository) StartRefresh(ctx context.Context, trigger, actorHash string) (int64, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id int64

	err = tx.QueryRow(ctx,
		`INSERT INTO catalog_refreshes (trigger, actor_hash) VALUES ($1, $2) RETURNING id`, trigger, actorHash,
	).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return 0, domain.ErrCatalogRefreshActive
		}

		return 0, fmt.Errorf("insert catalog refresh: %w", err)
	}

	if _, err := tx.Exec(ctx, `TRUNCATE public_repo_catalog_staging`); err != nil {
		return 0, fmt.Errorf("empty catalog staging: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	return id, nil
}

func (r *CatalogRepository) StagePage(ctx context.Context, id int64, repos []domain.Repo, pages, total int) error {
	rows := make([][]any, 0, len(repos))
	for _, repo := range repos {
		rows = append(rows, []any{
			repo.SourcecraftUUID, repo.OrgSlug, repo.RepoSlug, nullIfEmpty(repo.Language.Name),
			nullIfEmpty(repo.Language.Color), repo.RatingValue, repo.LastActivityAt, nullIfEmpty(repo.WebURL),
			nullIfEmpty(repo.Description),
		})
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.CopyFrom(ctx, pgx.Identifier{"public_repo_catalog_staging"},
		[]string{"sourcecraft_uuid", "org_slug", "repo_slug", "main_language", "language_color",
			"sourcecraft_rating", "last_activity_at", "web_url", "description"},
		pgx.CopyFromRows(rows))
	if err != nil {
		return fmt.Errorf("stage catalog page: %w", err)
	}

	if _, err := tx.Exec(ctx, `UPDATE catalog_refreshes SET pages = $2, repos = $3 WHERE id = $1`, id, pages, total); err != nil {
		return fmt.Errorf("update catalog refresh progress: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *CatalogRepository) CommitRefresh(ctx context.Context, id int64) (int, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Only a refresh that is still running may replace the catalog: a cancel in between wins.
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM catalog_refreshes WHERE id = $1 FOR UPDATE`, id).Scan(&status); err != nil {
		return 0, fmt.Errorf("lock catalog refresh: %w", err)
	}

	if status != domain.CatalogRefreshRunning {
		return 0, domain.ErrCatalogRefreshNotRunning
	}

	if _, err := tx.Exec(ctx, `DELETE FROM public_repo_catalog`); err != nil {
		return 0, fmt.Errorf("clear catalog: %w", err)
	}

	// The listing may repeat a repo or reuse a slug while it shifts; the latest row wins either way.
	tag, err := tx.Exec(ctx, `
		INSERT INTO public_repo_catalog (sourcecraft_uuid, org_slug, repo_slug, main_language, language_color,
			sourcecraft_rating, last_activity_at, web_url, description, fetched_at)
		SELECT DISTINCT ON (sourcecraft_uuid) sourcecraft_uuid, org_slug, repo_slug, main_language, language_color,
			sourcecraft_rating, last_activity_at, web_url, description, fetched_at
		FROM public_repo_catalog_staging
		ORDER BY sourcecraft_uuid, fetched_at DESC`)
	if err != nil {
		return 0, fmt.Errorf("fill catalog: %w", err)
	}

	size := int(tag.RowsAffected())

	if _, err := tx.Exec(ctx, `TRUNCATE public_repo_catalog_staging`); err != nil {
		return 0, fmt.Errorf("empty catalog staging: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE catalog_refreshes SET status = 'completed', finished_at = now(), repos = $2 WHERE id = $1`, id, size,
	); err != nil {
		return 0, fmt.Errorf("complete catalog refresh: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	return size, nil
}

func (r *CatalogRepository) FinishRefresh(ctx context.Context, id int64, status, code string) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE catalog_refreshes SET status = $2, error = $3, finished_at = now() WHERE id = $1 AND status = 'running'`,
		id, status, code)
	if err != nil {
		return fmt.Errorf("finish catalog refresh: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrCatalogRefreshNotRunning
	}

	return nil
}

func (r *CatalogRepository) FailOrphanedRefreshes(ctx context.Context) error {
	_, err := r.db.Exec(ctx,
		`UPDATE catalog_refreshes SET status = 'failed', error = $1, finished_at = now() WHERE status = 'running'`,
		domain.CatalogErrorInterrupted)
	if err != nil {
		return fmt.Errorf("fail orphaned catalog refreshes: %w", err)
	}

	return nil
}

func (r *CatalogRepository) GetRefresh(ctx context.Context, id int64) (domain.CatalogRefresh, error) {
	refresh, err := scanRefresh(r.db.QueryRow(ctx,
		`SELECT `+catalogRefreshColumns+` FROM catalog_refreshes WHERE id = $1`, id))
	if err != nil {
		return domain.CatalogRefresh{}, fmt.Errorf("get catalog refresh: %w", err)
	}

	return refresh, nil
}

func (r *CatalogRepository) Status(ctx context.Context) (domain.CatalogStatus, error) {
	var status domain.CatalogStatus

	err := r.db.QueryRow(ctx, `SELECT count(*), max(fetched_at) FROM public_repo_catalog`).Scan(&status.Size, &status.UpdatedAt)
	if err != nil {
		return domain.CatalogStatus{}, fmt.Errorf("catalog size: %w", err)
	}

	// The running refresh sorts first; otherwise the newest.
	current, err := scanRefresh(r.db.QueryRow(ctx,
		`SELECT `+catalogRefreshColumns+` FROM catalog_refreshes ORDER BY (status = 'running') DESC, id DESC LIMIT 1`))
	switch {
	case err == nil:
		status.Current = &current
	case !errors.Is(err, pgx.ErrNoRows):
		return domain.CatalogStatus{}, fmt.Errorf("current catalog refresh: %w", err)
	}

	completed, err := scanRefresh(r.db.QueryRow(ctx,
		`SELECT `+catalogRefreshColumns+` FROM catalog_refreshes WHERE status = 'completed' ORDER BY id DESC LIMIT 1`))
	switch {
	case err == nil:
		status.LastCompleted = &completed
	case !errors.Is(err, pgx.ErrNoRows):
		return domain.CatalogStatus{}, fmt.Errorf("last completed catalog refresh: %w", err)
	}

	return status, nil
}

func (r *CatalogRepository) Size(ctx context.Context) (int, error) {
	var size int
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM public_repo_catalog`).Scan(&size); err != nil {
		return 0, fmt.Errorf("catalog size: %w", err)
	}

	return size, nil
}

const catalogRepoColumns = `r.sourcecraft_uuid, r.org_slug, r.repo_slug, coalesce(r.main_language, ''),
	coalesce(r.language_color, ''), r.sourcecraft_rating, r.last_activity_at, coalesce(r.web_url, ''),
	coalesce(r.description, '')`

func scanCatalogRepo(rows pgx.Rows, extra ...any) (domain.Repo, error) {
	repo := domain.Repo{Visibility: domain.VisibilityPublic}

	dest := []any{&repo.SourcecraftUUID, &repo.OrgSlug, &repo.RepoSlug, &repo.Language.Name, &repo.Language.Color,
		&repo.RatingValue, &repo.LastActivityAt, &repo.WebURL, &repo.Description}

	err := rows.Scan(append(dest, extra...)...)

	return repo, err
}

func (r *CatalogRepository) TopRated(ctx context.Context, limit int) ([]domain.Repo, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+catalogRepoColumns+`
		FROM public_repo_catalog r
		ORDER BY r.sourcecraft_rating DESC, r.org_slug, r.repo_slug
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("top rated catalog repos: %w", err)
	}
	defer rows.Close()

	var repos []domain.Repo

	for rows.Next() {
		repo, err := scanCatalogRepo(rows)
		if err != nil {
			return nil, fmt.Errorf("scan catalog repo: %w", err)
		}

		repos = append(repos, repo)
	}

	return repos, rows.Err()
}

func (r *CatalogRepository) Select(ctx context.Context, filter domain.CatalogFilter, limit int) ([]domain.CatalogEntry, int, error) {
	args := batchCriteriaArgs(filter.Languages, filter.MinScore, filter.MaxScore, filter.OlderThanDays, filter.Repos, filter.OnlyRegrade)

	rows, err := r.db.Query(ctx, `
		SELECT `+catalogRepoColumns+`, g.graded_at, count(*) OVER ()
		FROM public_repo_catalog r
		LEFT JOIN repo_grades_public g ON g.sourcecraft_uuid = r.sourcecraft_uuid
		WHERE `+batchCriteria+`
		ORDER BY r.sourcecraft_rating DESC, r.org_slug, r.repo_slug
		LIMIT $7`,
		append(args, limit)...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("select catalog repos: %w", err)
	}
	defer rows.Close()

	var (
		entries []domain.CatalogEntry
		matched int
	)

	for rows.Next() {
		var entry domain.CatalogEntry

		entry.Repo, err = scanCatalogRepo(rows, &entry.GradedAt, &matched)
		if err != nil {
			return nil, 0, fmt.Errorf("scan catalog repo: %w", err)
		}

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate catalog repos: %w", err)
	}

	return entries, matched, nil
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

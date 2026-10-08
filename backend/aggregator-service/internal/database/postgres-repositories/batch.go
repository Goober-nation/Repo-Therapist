package postgres_repositories

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"aggregator-service/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type BatchRepository struct {
	db *pgxpool.Pool
}

func NewBatchRepository(db *pgxpool.Pool) *BatchRepository {
	return &BatchRepository{db: db}
}

// SelectBatchRepos picks public repos only. Every criterion is a parameter; unset ones are NULL and
// drop out of the WHERE. A score or age criterion on a repo that was never graded does not match,
// except the age one, where "never graded" counts as older than any age.
func (r *BatchRepository) SelectBatchRepos(ctx context.Context, filter domain.BatchFilter, limit int) ([]domain.BatchCandidate, bool, error) {
	var languages, picked []string
	if len(filter.Languages) > 0 {
		languages = filter.Languages
	}

	if len(filter.Repos) > 0 {
		picked = make([]string, len(filter.Repos))
		for i, repo := range filter.Repos {
			picked[i] = strings.ToLower(repo)
		}
	}

	rows, err := r.db.Query(ctx, `
		SELECT r.sourcecraft_uuid, r.org_slug, r.repo_slug, r.last_activity_at, g.graded_at
		FROM repos r
		LEFT JOIN repo_grades_public g ON g.sourcecraft_uuid = r.sourcecraft_uuid
		WHERE r.visibility = 'public'
		  AND ($1::text[] IS NULL OR lower(r.main_language) = ANY($1))
		  AND ($2::int IS NULL OR g.score >= $2)
		  AND ($3::int IS NULL OR g.score <= $3)
		  AND ($4::int IS NULL OR g.graded_at IS NULL OR g.graded_at < now() - make_interval(days => $4))
		  AND ($5::text[] IS NULL OR lower(r.org_slug || '/' || r.repo_slug) = ANY($5))
		ORDER BY r.org_slug, r.repo_slug
		LIMIT $6`,
		languages, filter.MinScore, filter.MaxScore, filter.OlderThanDays, picked, limit+1,
	)
	if err != nil {
		return nil, false, fmt.Errorf("select batch repos: %w", err)
	}
	defer rows.Close()

	var repos []domain.BatchCandidate

	for rows.Next() {
		var c domain.BatchCandidate
		if err := rows.Scan(&c.RepoID, &c.OrgSlug, &c.RepoSlug, &c.LastActivityAt, &c.GradedAt); err != nil {
			return nil, false, fmt.Errorf("scan batch repo: %w", err)
		}

		repos = append(repos, c)
	}

	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate batch repos: %w", err)
	}

	if len(repos) > limit {
		return repos[:limit], true, nil
	}

	return repos, false, nil
}

func (r *BatchRepository) BatchGradeInfo(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]domain.BatchGrade, error) {
	out := make(map[uuid.UUID]domain.BatchGrade, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	rows, err := r.db.Query(ctx,
		`SELECT sourcecraft_uuid, score, graded_at FROM repo_grades_public WHERE sourcecraft_uuid = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("batch grade info: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id    uuid.UUID
			score *int16
			grade domain.BatchGrade
		)
		if err := rows.Scan(&id, &score, &grade.GradedAt); err != nil {
			return nil, fmt.Errorf("scan batch grade info: %w", err)
		}

		if score != nil {
			v := int(*score)
			grade.Score = &v
		}

		out[id] = grade
	}

	return out, rows.Err()
}

func (r *BatchRepository) GetBatchRepo(ctx context.Context, id uuid.UUID) (domain.Repo, error) {
	var repo domain.Repo

	err := r.db.QueryRow(ctx,
		`SELECT sourcecraft_uuid, org_slug, repo_slug, visibility FROM repos WHERE sourcecraft_uuid = $1`, id,
	).Scan(&repo.SourcecraftUUID, &repo.OrgSlug, &repo.RepoSlug, &repo.Visibility)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Repo{}, domain.ErrRepositoryNotFound
	}

	if err != nil {
		return domain.Repo{}, fmt.Errorf("get batch repo: %w", err)
	}

	return repo, nil
}

func (r *BatchRepository) AvgGradeSeconds(ctx context.Context) (float64, error) {
	var avg *float64

	err := r.db.QueryRow(ctx, `
		SELECT avg(extract(epoch FROM finished_at - started_at)) FROM (
			SELECT started_at, finished_at FROM regrade_batch_items
			WHERE status = 'ok' AND started_at IS NOT NULL AND finished_at IS NOT NULL
			ORDER BY finished_at DESC LIMIT 100
		) recent`).Scan(&avg)
	if err != nil {
		return 0, fmt.Errorf("average grade time: %w", err)
	}

	if avg == nil {
		return 0, nil
	}

	return *avg, nil
}

func (r *BatchRepository) CreateBatch(ctx context.Context, actorHash string, filter domain.BatchFilter, repos []domain.BatchCandidate) (int64, error) {
	filterJSON, err := json.Marshal(filter)
	if err != nil {
		return 0, fmt.Errorf("encode filter: %w", err)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id int64

	err = tx.QueryRow(ctx,
		`INSERT INTO regrade_batches (actor_hash, filter) VALUES ($1, $2) RETURNING id`,
		actorHash, filterJSON,
	).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return 0, domain.ErrBatchActive
		}

		return 0, fmt.Errorf("insert batch: %w", err)
	}

	batch := &pgx.Batch{}
	for _, repo := range repos {
		batch.Queue(`INSERT INTO regrade_batch_items (batch_id, repo_id, org_slug, repo_slug) VALUES ($1, $2, $3, $4)`,
			id, repo.RepoID, repo.OrgSlug, repo.RepoSlug)
	}

	results := tx.SendBatch(ctx, batch)
	for range repos {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()

			return 0, fmt.Errorf("insert batch item: %w", err)
		}
	}

	if err := results.Close(); err != nil {
		return 0, fmt.Errorf("insert batch items: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	return id, nil
}

const batchSelect = `
	SELECT b.id, b.created_at, b.finished_at, b.status, b.actor_hash, b.filter,
	       count(i.*),
	       count(i.*) FILTER (WHERE i.status = 'pending'),
	       count(i.*) FILTER (WHERE i.status = 'running'),
	       count(i.*) FILTER (WHERE i.status = 'ok'),
	       count(i.*) FILTER (WHERE i.status = 'failed'),
	       count(i.*) FILTER (WHERE i.status = 'skipped')
	FROM regrade_batches b
	LEFT JOIN regrade_batch_items i ON i.batch_id = b.id`

func scanBatch(row pgx.Row) (domain.Batch, error) {
	var (
		b          domain.Batch
		filterJSON []byte
	)

	err := row.Scan(&b.ID, &b.CreatedAt, &b.FinishedAt, &b.Status, &b.ActorHash, &filterJSON,
		&b.Total, &b.Pending, &b.Running, &b.OK, &b.Failed, &b.Skipped)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Batch{}, domain.ErrBatchNotFound
		}

		return domain.Batch{}, fmt.Errorf("scan batch: %w", err)
	}

	if err := json.Unmarshal(filterJSON, &b.Filter); err != nil {
		return domain.Batch{}, fmt.Errorf("decode batch filter: %w", err)
	}

	return b, nil
}

func (r *BatchRepository) GetBatch(ctx context.Context, id int64) (domain.Batch, error) {
	return scanBatch(r.db.QueryRow(ctx, batchSelect+` WHERE b.id = $1 GROUP BY b.id`, id))
}

func (r *BatchRepository) GetActiveBatch(ctx context.Context) (domain.Batch, error) {
	return scanBatch(r.db.QueryRow(ctx, batchSelect+` WHERE b.status IN ('running', 'paused') GROUP BY b.id`))
}

func (r *BatchRepository) ListBatches(ctx context.Context, limit int) ([]domain.Batch, error) {
	rows, err := r.db.Query(ctx, batchSelect+` GROUP BY b.id ORDER BY b.id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list batches: %w", err)
	}
	defer rows.Close()

	var batches []domain.Batch

	for rows.Next() {
		batch, err := scanBatch(rows)
		if err != nil {
			return nil, err
		}

		batches = append(batches, batch)
	}

	return batches, rows.Err()
}

func (r *BatchRepository) ListBatchItems(ctx context.Context, id int64, status string, limit int) ([]domain.BatchItem, error) {
	rows, err := r.db.Query(ctx, `
		SELECT repo_id, org_slug, repo_slug, status, error, started_at, finished_at
		FROM regrade_batch_items
		WHERE batch_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY org_slug, repo_slug
		LIMIT $3`, id, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list batch items: %w", err)
	}
	defer rows.Close()

	var items []domain.BatchItem

	for rows.Next() {
		var item domain.BatchItem
		if err := rows.Scan(&item.RepoID, &item.OrgSlug, &item.RepoSlug, &item.Status, &item.Error,
			&item.StartedAt, &item.FinishedAt); err != nil {
			return nil, fmt.Errorf("scan batch item: %w", err)
		}

		items = append(items, item)
	}

	return items, rows.Err()
}

func (r *BatchRepository) SetBatchStatus(ctx context.Context, id int64, from []string, to string) (bool, error) {
	tag, err := r.db.Exec(ctx, `UPDATE regrade_batches SET status = $3 WHERE id = $1 AND status = ANY($2)`, id, from, to)
	if err != nil {
		return false, fmt.Errorf("set batch status: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

func (r *BatchRepository) FinishBatch(ctx context.Context, id int64, status string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE regrade_batches SET status = $2, finished_at = now() WHERE id = $1 AND status IN ('running', 'paused')`,
		id, status)
	if err != nil {
		return fmt.Errorf("finish batch: %w", err)
	}

	return nil
}

func (r *BatchRepository) ClaimNextBatchItem(ctx context.Context, id int64) (domain.BatchItem, bool, error) {
	var item domain.BatchItem

	err := r.db.QueryRow(ctx, `
		UPDATE regrade_batch_items SET status = 'running', started_at = now()
		WHERE (batch_id, repo_id) = (
			SELECT batch_id, repo_id FROM regrade_batch_items
			WHERE batch_id = $1 AND status = 'pending'
			  AND EXISTS (SELECT 1 FROM regrade_batches WHERE id = $1 AND status = 'running')
			ORDER BY org_slug, repo_slug
			LIMIT 1
			FOR UPDATE SKIP LOCKED)
		RETURNING repo_id, org_slug, repo_slug, status, error, started_at, finished_at`, id,
	).Scan(&item.RepoID, &item.OrgSlug, &item.RepoSlug, &item.Status, &item.Error, &item.StartedAt, &item.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.BatchItem{}, false, nil
	}

	if err != nil {
		return domain.BatchItem{}, false, fmt.Errorf("claim batch item: %w", err)
	}

	return item, true, nil
}

func (r *BatchRepository) FinishBatchItem(ctx context.Context, id int64, repoID uuid.UUID, status, errCode string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE regrade_batch_items SET status = $3, error = $4, finished_at = now() WHERE batch_id = $1 AND repo_id = $2`,
		id, repoID, status, errCode)
	if err != nil {
		return fmt.Errorf("finish batch item: %w", err)
	}

	return nil
}

func (r *BatchRepository) SkipPendingItems(ctx context.Context, id int64) (int, error) {
	tag, err := r.db.Exec(ctx,
		`UPDATE regrade_batch_items SET status = 'skipped', finished_at = now() WHERE batch_id = $1 AND status = 'pending'`, id)
	if err != nil {
		return 0, fmt.Errorf("skip pending items: %w", err)
	}

	return int(tag.RowsAffected()), nil
}

func (r *BatchRepository) RequeueRunningItems(ctx context.Context, id int64) error {
	_, err := r.db.Exec(ctx,
		`UPDATE regrade_batch_items SET status = 'pending', started_at = NULL WHERE batch_id = $1 AND status = 'running'`, id)
	if err != nil {
		return fmt.Errorf("requeue running items: %w", err)
	}

	return nil
}

func (r *BatchRepository) SkipOrphanedItems(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `
		UPDATE regrade_batch_items SET status = 'skipped', finished_at = now()
		WHERE status = 'running'
		  AND batch_id IN (SELECT id FROM regrade_batches WHERE status IN ('cancelled', 'completed'))`)
	if err != nil {
		return fmt.Errorf("skip orphaned items: %w", err)
	}

	return nil
}

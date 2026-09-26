package postgres_repositories

import (
	"aggregator-service/internal/domain"
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RepoRepository struct {
	db *pgxpool.Pool
}

func NewRepoRepository(db *pgxpool.Pool) *RepoRepository {
	return &RepoRepository{
		db: db,
	}
}

type GradesRepository struct {
	db *pgxpool.Pool
}

func NewGradesRepository(db *pgxpool.Pool) *GradesRepository {
	return &GradesRepository{
		db: db,
	}
}

type LLMRepository struct {
	db *pgxpool.Pool
}

func NewLLMRepository(db *pgxpool.Pool) *LLMRepository {
	return &LLMRepository{
		db: db,
	}
}

func (r *RepoRepository) CreateRepository(
	ctx context.Context,
	repo domain.Repo,
) (uuid.UUID, error) {
	id := uuid.New()

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(
		ctx,
		`
		INSERT INTO repos (
			sourcecraft_uuid,
			org_slug,
			repo_slug,
			visibility,
			last_activity_at
		)
		VALUES ($1, $2, $3, $4, $5)
		`,
		id,
		repo.OrgSlug,
		repo.RepoSlug,
		repo.Visibility,
		repo.LastActivityAt,
	)
	if err != nil {
		return uuid.Nil, mapRepositoryError(err)
	}

	_, err = tx.Exec(
		ctx,
		`
		INSERT INTO repo_lock (
			sourcecraft_uuid,
			status
		)
		VALUES ($1, 'free')
		`,
		id,
	)
	if err != nil {
		return uuid.Nil, mapRepositoryError(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}

	return id, nil
}

func (r *RepoRepository) UpdateRepository(ctx context.Context, repo domain.Repo) (uuid.UUID, error) {
	tag, err := r.db.Exec(
		ctx,
		`
		UPDATE repos
		SET
			org_slug = $2,
			repo_slug = $3,
			visibility = $4,
			last_activity_at = $5
		WHERE sourcecraft_uuid = $1
		`,
		repo.SourcecraftUUID,
		repo.OrgSlug,
		repo.RepoSlug,
		repo.Visibility,
		repo.LastActivityAt,
	)

	if err != nil {
		return uuid.Nil, mapRepositoryError(err)
	}

	if tag.RowsAffected() == 0 {
		return uuid.Nil, mapRepositoryError(pgx.ErrNoRows)
	}

	return repo.SourcecraftUUID, nil
}

func (r *RepoRepository) DeleteRepository(ctx context.Context, repoID uuid.UUID) error {
	tag, err := r.db.Exec(
		ctx,
		`
		DELETE FROM repos
		WHERE sourcecraft_uuid = $1
		`,
		repoID,
	)

	if err != nil {
		return mapRepositoryError(err)
	}

	if tag.RowsAffected() == 0 {
		return mapRepositoryError(pgx.ErrNoRows)
	}

	return nil
}

func (r *RepoRepository) GetRepositoryByID(ctx context.Context, repoID uuid.UUID) (domain.Repo, error) {
	var repo domain.Repo

	err := r.db.QueryRow(
		ctx,
		`
		SELECT
			sourcecraft_uuid,
			org_slug,
			repo_slug,
			visibility,
			last_activity_at
		FROM repos
		WHERE sourcecraft_uuid = $1
		`,
		repoID,
	).Scan(
		&repo.SourcecraftUUID,
		&repo.OrgSlug,
		&repo.RepoSlug,
		&repo.Visibility,
		&repo.LastActivityAt,
	)

	if err != nil {
		return domain.Repo{}, mapRepositoryError(err)
	}

	return repo, nil
}

func (r *RepoRepository) GetRepositoryBySlugs(ctx context.Context, orgSlug string, repoSlug string) (domain.Repo, error) {
	var repo domain.Repo

	err := r.db.QueryRow(
		ctx,
		`
		SELECT
			sourcecraft_uuid,
			org_slug,
			repo_slug,
			visibility,
			last_activity_at
		FROM repos
		WHERE org_slug = $1
		  AND repo_slug = $2
		`,
		orgSlug,
		repoSlug,
	).Scan(
		&repo.SourcecraftUUID,
		&repo.OrgSlug,
		&repo.RepoSlug,
		&repo.Visibility,
		&repo.LastActivityAt,
	)

	if err != nil {
		return domain.Repo{}, mapRepositoryError(err)
	}

	return repo, nil
}

func (r *GradesRepository) UpsertGrade(
	ctx context.Context,
	grade domain.RepoGrade,
) error {
	_, err := r.db.Exec(
		ctx,
		`
		INSERT INTO repo_grades (
			sourcecraft_uuid,
			public_score,
			public_category_scores,
			public_recommendations,
			private_score,
			private_category_scores,
			private_recommendations,
			missing_data,
			graded_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		)
		ON CONFLICT (sourcecraft_uuid)
		DO UPDATE SET
			public_score = EXCLUDED.public_score,
			public_category_scores = EXCLUDED.public_category_scores,
			public_recommendations = EXCLUDED.public_recommendations,
			private_score = EXCLUDED.private_score,
			private_category_scores = EXCLUDED.private_category_scores,
			private_recommendations = EXCLUDED.private_recommendations,
			missing_data = EXCLUDED.missing_data,
			graded_at = EXCLUDED.graded_at
		`,
		grade.SourcecraftUUID,
		grade.PublicScore,
		grade.PublicCategoryScores,
		grade.PublicRecommendations,
		grade.PrivateScore,
		grade.PrivateCategoryScores,
		grade.PrivateRecommendations,
		grade.MissingData,
		grade.GradedAt,
	)
	if err != nil {
		return mapGradeError(err)
	}

	return nil
}

func (r *GradesRepository) DeleteGrade(ctx context.Context, repoID uuid.UUID) error {
	tag, err := r.db.Exec(
		ctx,
		`
		DELETE FROM repo_grades
		WHERE sourcecraft_uuid = $1
		`,
		repoID,
	)

	if err != nil {
		return mapGradeError(err)
	}

	if tag.RowsAffected() == 0 {
		return mapGradeError(pgx.ErrNoRows)
	}

	return nil
}

func (r *GradesRepository) GetGradeByRepoID(ctx context.Context, repoID uuid.UUID) (domain.RepoGrade, error) {
	var grade domain.RepoGrade

	err := r.db.QueryRow(
		ctx,
		`
		SELECT
			sourcecraft_uuid,
			public_score,
			public_category_scores,
			public_recommendations,
			private_score,
			private_category_scores,
			private_recommendations,
			missing_data,
			graded_at
		FROM repo_grades
		WHERE sourcecraft_uuid = $1
		`,
		repoID,
	).Scan(
		&grade.SourcecraftUUID,
		&grade.PublicScore,
		&grade.PublicCategoryScores,
		&grade.PublicRecommendations,
		&grade.PrivateScore,
		&grade.PrivateCategoryScores,
		&grade.PrivateRecommendations,
		&grade.MissingData,
		&grade.GradedAt,
	)

	if err != nil {
		return domain.RepoGrade{}, mapGradeError(err)
	}

	return grade, nil
}

func (r *LLMRepository) UpsertPublicResponse(ctx context.Context, response domain.LLMRecommendationsReceivedEvent) error {
	_, err := r.db.Exec(
		ctx,
		`
        INSERT INTO repo_llm_responses_public (
            sourcecraft_uuid,
            raw_response,
            model_used,
            evaluated_at
        )
        VALUES ($1, $2, $3, $4)
        ON CONFLICT (sourcecraft_uuid)
        DO UPDATE SET
            raw_response = EXCLUDED.raw_response,
            model_used = EXCLUDED.model_used,
            evaluated_at = EXCLUDED.evaluated_at
        `,
		response.SourcecraftUUID,
		response.RawResponse,
		response.ModelUsed,
		response.EvaluatedAt,
	)
	if err != nil {
		return mapLLMResponseError(err)
	}

	return nil
}

func (r *LLMRepository) DeletePublicResponse(ctx context.Context, repoID uuid.UUID) error {
	tag, err := r.db.Exec(
		ctx,
		`
		DELETE FROM repo_llm_responses_public
		WHERE sourcecraft_uuid = $1
		`,
		repoID,
	)

	if err != nil {
		return mapLLMResponseError(err)
	}

	if tag.RowsAffected() == 0 {
		return mapLLMResponseError(pgx.ErrNoRows)
	}

	return nil
}

func (r *LLMRepository) GetPublicResponseByRepoID(ctx context.Context, repoID uuid.UUID) (domain.RepoLLMResponsePublic, error) {
	var response domain.RepoLLMResponsePublic

	err := r.db.QueryRow(
		ctx,
		`
		SELECT
			sourcecraft_uuid,
			raw_response,
			model_used,
			evaluated_at
		FROM repo_llm_responses_public
		WHERE sourcecraft_uuid = $1
		`,
		repoID,
	).Scan(
		&response.SourcecraftUUID,
		&response.RawResponse,
		&response.ModelUsed,
		&response.EvaluatedAt,
	)

	if err != nil {
		return domain.RepoLLMResponsePublic{}, mapLLMResponseError(err)
	}

	return response, nil
}

func (r *LLMRepository) UpsertPrivateResponse(
	ctx context.Context,
	response domain.LLMRecommendationsReceivedEvent,
) error {
	_, err := r.db.Exec(
		ctx,
		`
        INSERT INTO repo_llm_responses_private (
            sourcecraft_uuid,
            raw_response,
            model_used,
            evaluated_at
        )
        VALUES ($1, $2, $3, $4)
        ON CONFLICT (sourcecraft_uuid)
        DO UPDATE SET
            raw_response = EXCLUDED.raw_response,
            model_used = EXCLUDED.model_used,
            evaluated_at = EXCLUDED.evaluated_at
        `,
		response.SourcecraftUUID,
		response.RawResponse,
		response.ModelUsed,
		response.EvaluatedAt,
	)
	if err != nil {
		return mapLLMResponseError(err)
	}

	return nil
}

func (r *LLMRepository) DeletePrivateResponse(ctx context.Context, repoID uuid.UUID) error {
	tag, err := r.db.Exec(
		ctx,
		`
		DELETE FROM repo_llm_responses_private
		WHERE sourcecraft_uuid = $1
		`,
		repoID,
	)

	if err != nil {
		return mapLLMResponseError(err)
	}

	if tag.RowsAffected() == 0 {
		return mapLLMResponseError(pgx.ErrNoRows)
	}

	return nil
}

func (r *LLMRepository) GetPrivateResponseByRepoID(ctx context.Context, repoID uuid.UUID) (domain.RepoLLMResponsePrivate, error) {
	var response domain.RepoLLMResponsePrivate

	err := r.db.QueryRow(
		ctx,
		`
		SELECT
			sourcecraft_uuid,
			raw_response,
			model_used,
			evaluated_at
		FROM repo_llm_responses_private
		WHERE sourcecraft_uuid = $1
		`,
		repoID,
	).Scan(
		&response.SourcecraftUUID,
		&response.RawResponse,
		&response.ModelUsed,
		&response.EvaluatedAt,
	)

	if err != nil {
		return domain.RepoLLMResponsePrivate{}, mapLLMResponseError(err)
	}

	return response, nil
}

func (r *RepoRepository) GetRepositoryLock(ctx context.Context, repoID uuid.UUID) (domain.RepoLock, error) {
	var lock domain.RepoLock

	err := r.db.QueryRow(
		ctx,
		`
		SELECT
			sourcecraft_uuid,
			status,
			status_updated_at
		FROM repo_lock
		WHERE sourcecraft_uuid = $1
		`,
		repoID,
	).Scan(
		&lock.SourcecraftUUID,
		&lock.Status,
		&lock.StatusUpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.RepoLock{}, mapRepositoryError(pgx.ErrNoRows)
		}

		return domain.RepoLock{}, mapRepositoryError(err)

	}

	return lock, nil
}
func (r *RepoRepository) AcquireRepositoryLock(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(
		ctx,
		`
		UPDATE repo_lock
		SET status = 'busy',
		    status_updated_at = now()
		WHERE sourcecraft_uuid = $1
		  AND status = 'free'
		`,
		id,
	)
	if err != nil {
		return false, mapRepositoryError(err)
	}

	return tag.RowsAffected() == 1, nil
}

func (r *RepoRepository) ReleaseRepositoryLock(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(
		ctx,
		`
		UPDATE repo_lock
		SET status = 'free',
		    status_updated_at = now()
		WHERE sourcecraft_uuid = $1
		`,
		id,
	)

	return mapRepositoryError(err)
}

package postgres_repositories

import (
	"context"

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

func (r *RepoRepository) CreateRepository(ctx context.Context, repo Repo) (uuid.UUID, error) {
	repo.SourcecraftUUID = uuid.New()

	_, err := r.db.Exec(
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
		repo.SourcecraftUUID,
		repo.OrgSlug,
		repo.RepoSlug,
		repo.Visibility,
		repo.LastActivityAt,
	)

	if err != nil {
		return uuid.Nil, err
	}

	return repo.SourcecraftUUID, nil
}

func (r *RepoRepository) UpdateRepository(ctx context.Context, repo Repo) (uuid.UUID, error) {
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
		return uuid.Nil, err
	}

	if tag.RowsAffected() == 0 {
		return uuid.Nil, pgx.ErrNoRows
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
		return err
	}

	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}

	return nil
}

func (r *RepoRepository) GetRepositoryByID(ctx context.Context, repoID uuid.UUID) (Repo, error) {
	var repo Repo

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
		return Repo{}, err
	}

	return repo, nil
}

func (r *RepoRepository) GetRepositoryBySlugs(ctx context.Context, orgSlug string, repoSlug string) (Repo, error) {
	var repo Repo

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
		return Repo{}, err
	}

	return repo, nil
}

func (r *GradesRepository) CreateGrade(ctx context.Context, grade RepoGrade) error {
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
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9
		)
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

	return err
}

func (r *GradesRepository) UpdateGrade(ctx context.Context, grade RepoGrade) error {
	tag, err := r.db.Exec(
		ctx,
		`
		UPDATE repo_grades
		SET
			public_score = $2,
			public_category_scores = $3,
			public_recommendations = $4,
			private_score = $5,
			private_category_scores = $6,
			private_recommendations = $7,
			missing_data = $8,
			graded_at = $9
		WHERE sourcecraft_uuid = $1
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
		return err
	}

	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
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
		return err
	}

	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}

	return nil
}

func (r *GradesRepository) GetGradeByRepoID(ctx context.Context, repoID uuid.UUID) (RepoGrade, error) {
	var grade RepoGrade

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
		return RepoGrade{}, err
	}

	return grade, nil
}

func (r *LLMRepository) CreatePublicResponse(ctx context.Context, response RepoLLMResponsePublic) error {
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
		`,
		response.SourcecraftUUID,
		response.RawResponse,
		response.ModelUsed,
		response.EvaluatedAt,
	)

	return err
}

func (r *LLMRepository) UpdatePublicResponse(ctx context.Context, response RepoLLMResponsePublic) error {
	tag, err := r.db.Exec(
		ctx,
		`
		UPDATE repo_llm_responses_public
		SET
			raw_response = $2,
			model_used = $3,
			evaluated_at = $4
		WHERE sourcecraft_uuid = $1
		`,
		response.SourcecraftUUID,
		response.RawResponse,
		response.ModelUsed,
		response.EvaluatedAt,
	)

	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
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
		return err
	}

	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}

	return nil
}

func (r *LLMRepository) GetPublicResponseByRepoID(ctx context.Context, repoID uuid.UUID) (RepoLLMResponsePublic, error) {
	var response RepoLLMResponsePublic

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
		return RepoLLMResponsePublic{}, err
	}

	return response, nil
}

func (r *LLMRepository) CreatePrivateResponse(ctx context.Context, response RepoLLMResponsePrivate) error {
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
		`,
		response.SourcecraftUUID,
		response.RawResponse,
		response.ModelUsed,
		response.EvaluatedAt,
	)

	return err
}

func (r *LLMRepository) UpdatePrivateResponse(ctx context.Context, response RepoLLMResponsePrivate) error {
	tag, err := r.db.Exec(
		ctx,
		`
		UPDATE repo_llm_responses_private
		SET
			raw_response = $2,
			model_used = $3,
			evaluated_at = $4
		WHERE sourcecraft_uuid = $1
		`,
		response.SourcecraftUUID,
		response.RawResponse,
		response.ModelUsed,
		response.EvaluatedAt,
	)

	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
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
		return err
	}

	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}

	return nil
}

func (r *LLMRepository) GetPrivateResponseByRepoID(ctx context.Context, repoID uuid.UUID) (RepoLLMResponsePrivate, error) {
	var response RepoLLMResponsePrivate

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
		return RepoLLMResponsePrivate{}, err
	}

	return response, nil
}

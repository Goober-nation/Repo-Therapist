package domain

import (
	"aggregator-service/internal/database/postgres-repositories"
	"context"

	"github.com/google/uuid"
)

type RepoRepository interface {
	CreateRepository(ctx context.Context, repo postgres_repositories.Repo) (uuid.UUID, error)
	UpdateRepository(ctx context.Context, repo postgres_repositories.Repo) (uuid.UUID, error)
	DeleteRepository(ctx context.Context, repoID uuid.UUID) error
	GetRepositoryByID(ctx context.Context, repoID uuid.UUID) (postgres_repositories.Repo, error)
	GetRepositoryBySlugs(ctx context.Context, orgSlug, repoSlug string) (postgres_repositories.Repo, error)
}

type GradesRepository interface {
	CreateGrade(ctx context.Context, grade postgres_repositories.RepoGrade) error
	UpdateGrade(ctx context.Context, grade postgres_repositories.RepoGrade) error
	DeleteGrade(ctx context.Context, repoID uuid.UUID) error
	GetGradeByRepoID(ctx context.Context, repoID uuid.UUID) (postgres_repositories.RepoGrade, error)
}

type LLMRepository interface {
	CreatePublicResponse(ctx context.Context, response postgres_repositories.RepoLLMResponsePublic) error
	UpdatePublicResponse(ctx context.Context, response postgres_repositories.RepoLLMResponsePublic) error
	DeletePublicResponse(ctx context.Context, repoID uuid.UUID) error
	GetPublicResponseByRepoID(ctx context.Context, repoID uuid.UUID) (postgres_repositories.RepoLLMResponsePublic, error)

	CreatePrivateResponse(ctx context.Context, response postgres_repositories.RepoLLMResponsePrivate) error
	UpdatePrivateResponse(ctx context.Context, response postgres_repositories.RepoLLMResponsePrivate) error
	DeletePrivateResponse(ctx context.Context, repoID uuid.UUID) error
	GetPrivateResponseByRepoID(ctx context.Context, repoID uuid.UUID) (postgres_repositories.RepoLLMResponsePrivate, error)
}

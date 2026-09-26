package domain

import (
	"context"

	"github.com/google/uuid"
)

type RepoRepository interface {
	CreateRepository(ctx context.Context, repo Repo) (uuid.UUID, error)
	UpdateRepository(ctx context.Context, repo Repo) (uuid.UUID, error)
	DeleteRepository(ctx context.Context, repoID uuid.UUID) error
	GetRepositoryByID(ctx context.Context, repoID uuid.UUID) (Repo, error)
	GetRepositoryBySlugs(ctx context.Context, orgSlug, repoSlug string) (Repo, error)
	GetRepositoryLock(ctx context.Context, id uuid.UUID) (RepoLock, error)
	AcquireRepositoryLock(ctx context.Context, id uuid.UUID) (bool, error)
	ReleaseRepositoryLock(ctx context.Context, id uuid.UUID) error
}

type GradesRepository interface {
	UpsertGrade(ctx context.Context, grade RepoGrade) error

	DeleteGrade(ctx context.Context, repoID uuid.UUID) error
	GetGradeByRepoID(ctx context.Context, repoID uuid.UUID) (RepoGrade, error)
}

type LLMRepository interface {
	UpsertPublicResponse(ctx context.Context, response LLMRecommendationsReceivedEvent) error
	UpsertPrivateResponse(ctx context.Context, response LLMRecommendationsReceivedEvent) error
	DeletePublicResponse(ctx context.Context, repoID uuid.UUID) error
	GetPublicResponseByRepoID(ctx context.Context, repoID uuid.UUID) (RepoLLMResponsePublic, error)

	DeletePrivateResponse(ctx context.Context, repoID uuid.UUID) error
	GetPrivateResponseByRepoID(ctx context.Context, repoID uuid.UUID) (RepoLLMResponsePrivate, error)
}

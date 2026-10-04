package domain

import (
	"context"
	"encoding/json"
	"time"

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
	UpsertPublicGrade(ctx context.Context, grade RepoGrade) error
	UpsertPrivateGrade(ctx context.Context, grade RepoGrade) error
	GetPublicGradeByRepoID(ctx context.Context, repoID uuid.UUID) (RepoGrade, error)
	GetPrivateGradeByRepoID(ctx context.Context, repoID uuid.UUID) (RepoGrade, error)
	// UpsertPublicConfidence stores the low-confidence flag of a public grade;
	// DeletePublicConfidence removes it (grade-service sent none), so a stale flag never survives.
	UpsertPublicConfidence(ctx context.Context, repoID uuid.UUID, confidence GradeConfidence, gradedAt *time.Time) error
	DeletePublicConfidence(ctx context.Context, repoID uuid.UUID) error

	// Analytics rows, per grade mode. Get returns ErrGradeNotFound when there is no row.
	UpsertPublicAnalytics(ctx context.Context, repoID uuid.UUID, analytics json.RawMessage, gradedAt *time.Time) error
	UpsertPrivateAnalytics(ctx context.Context, repoID uuid.UUID, analytics json.RawMessage, gradedAt *time.Time) error
	DeletePublicAnalytics(ctx context.Context, repoID uuid.UUID) error
	DeletePrivateAnalytics(ctx context.Context, repoID uuid.UUID) error
	GetPublicAnalytics(ctx context.Context, repoID uuid.UUID) (RepoAnalytics, error)
	GetPrivateAnalytics(ctx context.Context, repoID uuid.UUID) (RepoAnalytics, error)

	// UpsertPublicFacts stores the filterable facts of a public grade; DeletePublicFacts removes
	// them (grade-service sent none), so a stale fact never survives.
	UpsertPublicFacts(ctx context.Context, repoID uuid.UUID, facts RepoFacts, gradedAt *time.Time) error
	DeletePublicFacts(ctx context.Context, repoID uuid.UUID) error
	// HasPublicFacts reports whether the repo has a facts row (even one whose members are all unknown).
	HasPublicFacts(ctx context.Context, repoID uuid.UUID) (bool, error)

	// AppendPublicHistory adds one point to the public score history (docs/ranking.md). It is
	// append-only and idempotent: a point with the same repo and graded_at is ignored. A grade
	// without a score or graded_at has no history point and is skipped without error.
	AppendPublicHistory(ctx context.Context, grade RepoGrade) error
}

type LLMRepository interface {
	UpsertPublicResponse(ctx context.Context, response LLMRecommendationsReceivedEvent) error
	UpsertPrivateResponse(ctx context.Context, response LLMRecommendationsReceivedEvent) error
	DeletePublicResponse(ctx context.Context, repoID uuid.UUID) error
	GetPublicResponseByRepoID(ctx context.Context, repoID uuid.UUID) (RepoLLMResponsePublic, error)

	DeletePrivateResponse(ctx context.Context, repoID uuid.UUID) error
	GetPrivateResponseByRepoID(ctx context.Context, repoID uuid.UUID) (RepoLLMResponsePrivate, error)
}

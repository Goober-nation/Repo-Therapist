package domain

import "errors"

var (
	ErrRepositoryNotFound = errors.New("repository not found")
	ErrRepositoryExists   = errors.New("repository already exists")

	ErrGradeNotFound = errors.New("grade not found")
	ErrGradeExists   = errors.New("grade already exists")

	ErrLLMResponseNotFound = errors.New("llm response not found")
	ErrLLMResponseExists   = errors.New("llm response already exists")

	ErrRepoLocked = errors.New("repository is locked")

	ErrInvalidRepository = errors.New("invalid repository")
	ErrInvalidGrade      = errors.New("invalid grade")

	ErrSourceCraftUnavailable = errors.New("sourcecraft service unavailable")
	ErrAppSecUnavailable      = errors.New("appsec service unavailable")
	ErrCIUnavailable          = errors.New("ci service unavailable")
	ErrLLMUnavailable         = errors.New("llm service unavailable")

	ErrGradingFailed = errors.New("repository grading failed")

	ErrNoAccess = errors.New("no access rights")

	GradingPending           = errors.New("grading pending")
	ErrExternalAPI           = errors.New("external api error")
	ErrRecommendationsFailed = errors.New("recommendations failed")
	ErrNoGrade               = errors.New("no grading found")
	ErrGradeOutdated         = errors.New("grade is outdated")
)

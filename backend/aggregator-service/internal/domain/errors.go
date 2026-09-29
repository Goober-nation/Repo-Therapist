package domain

import (
	"errors"
	"time"
)

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

	ErrNoAccess        = errors.New("no access rights")
	ErrUnauthenticated = errors.New("missing or invalid user token")

	GradingPending           = errors.New("grading pending")
	ErrExternalAPI           = errors.New("external api error")
	ErrRecommendationsFailed = errors.New("recommendations failed")
	ErrNoGrade               = errors.New("no grading found")
	ErrGradeOutdated         = errors.New("grade is outdated")
	ErrTimeout               = errors.New("wrapper timeout")
	ErrGradingRateLimited    = errors.New("grading rate limited")
	// ErrGradingInterrupted: the last grading of this repo failed or was cut short (its result
	// carried an error code); reported once so the user can start it again.
	ErrGradingInterrupted = errors.New("grading interrupted")
)

// RateLimitedError reports that the service-wide grading limit is reached and when a slot frees
// up. It matches ErrGradingRateLimited with errors.Is.
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e RateLimitedError) Error() string {
	return ErrGradingRateLimited.Error()
}

func (e RateLimitedError) Unwrap() error {
	return ErrGradingRateLimited
}

package service

import (
	"aggregator-service/internal/domain"
	"errors"
	"testing"
	"time"
)

func TestForceGradingRegradesCurrentGrade(t *testing.T) {
	f := newFixture("public")
	score := int16(80)
	f.grades.public[testRepoID] = domain.RepoGrade{SourcecraftUUID: testRepoID, Score: &score, GradedAt: gradedAt(testNow)}

	if _, err := f.svc.ForceGrading(userCtx(""), "org", "repo"); !errors.Is(err, domain.GradingPending) {
		t.Fatalf("err = %v, want GradingPending", err)
	}
	if len(f.grading.events) != 1 {
		t.Fatalf("published %d grading requests, want 1", len(f.grading.events))
	}
}

func TestForceGradingJoinsRunningGrading(t *testing.T) {
	f := newFixture("public")

	_, _ = f.svc.ForceGrading(userCtx(""), "org", "repo")
	_, err := f.svc.ForceGrading(userCtx(""), "org", "repo")

	if !errors.Is(err, domain.GradingPending) || len(f.grading.events) != 1 {
		t.Fatalf("err = %v events = %d, want GradingPending and one grading", err, len(f.grading.events))
	}
}

func TestForceGradingRetriesAfterFailure(t *testing.T) {
	f := newFixture("public")
	score := int16(60)
	f.grades.public[testRepoID] = domain.RepoGrade{SourcecraftUUID: testRepoID, Score: &score, GradedAt: gradedAt(testNow)}
	f.svc.gradingFailed(llmKey(testRepoID, "public"))

	if _, err := f.svc.ForceGrading(userCtx(""), "org", "repo"); !errors.Is(err, domain.GradingPending) {
		t.Fatalf("err = %v, want a new grading", err)
	}
}

func TestForceGradingReportsGradingLimit(t *testing.T) {
	f := newFixture("public")
	f.svc.GradeLimiter = NewGradeLimiter(1, 10*time.Minute)
	f.svc.GradeLimiter.Reserve(testNow)
	score := int16(70)
	f.grades.public[testRepoID] = domain.RepoGrade{SourcecraftUUID: testRepoID, Score: &score, GradedAt: gradedAt(testNow)}

	_, err := f.svc.ForceGrading(userCtx(""), "org", "repo")
	if !errors.As(err, new(domain.RateLimitedError)) {
		t.Fatalf("err = %v, want the grading limit", err)
	}
}

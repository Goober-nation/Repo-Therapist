package service

import (
	"aggregator-service/internal/domain"
	"errors"
	"testing"
	"time"
)

func TestFailedGradingIsReportedOnceThenRetried(t *testing.T) {
	f := newFixture("public")

	_, _ = f.svc.GetGrading(userCtx(""), "org", "repo")
	if err := f.svc.SaveGradingResult(userCtx(""), domain.RepoGrade{SourcecraftUUID: testRepoID}, "public", "GRADING_INTERRUPTED"); err == nil {
		t.Fatal("failure result saved without error")
	}
	if !f.repos.lockFree {
		t.Fatal("lock still busy after a failure result")
	}

	_, err := f.svc.GetGrading(userCtx(""), "org", "repo")
	if !errors.Is(err, domain.ErrGradingInterrupted) {
		t.Fatalf("first poll after failure err = %v, want ErrGradingInterrupted", err)
	}
	if len(f.grading.events) != 1 {
		t.Fatalf("published %d gradings, want 1 (failure must not start a new one)", len(f.grading.events))
	}

	_, err = f.svc.GetGrading(userCtx(""), "org", "repo")
	if !errors.Is(err, domain.GradingPending) || len(f.grading.events) != 2 {
		t.Fatalf("retry err = %v events = %d, want a new grading", err, len(f.grading.events))
	}
}

func TestFailedRegradeKeepsServingOutdatedGrade(t *testing.T) {
	f := newFixture("public")
	score := int16(60)
	f.grades.public[testRepoID] = domain.RepoGrade{
		SourcecraftUUID: testRepoID,
		Score:           &score,
		GradedAt:        gradedAt(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
	}
	f.svc.gradingFailed(llmKey(testRepoID, "public"))

	grade, err := f.svc.GetGrading(userCtx(""), "org", "repo")
	if err != nil || grade == nil || *grade.Score != 60 {
		t.Fatalf("grade=%+v err=%v, want the outdated grade", grade, err)
	}
}

func TestOldGradingFailureIsForgotten(t *testing.T) {
	f := newFixture("public")
	f.svc.gradingFailures[llmKey(testRepoID, "public")] = testNow.Add(-gradingFailureTTL - time.Minute)

	_, err := f.svc.GetGrading(userCtx(""), "org", "repo")
	if !errors.Is(err, domain.GradingPending) {
		t.Fatalf("err = %v, want a fresh grading", err)
	}
}

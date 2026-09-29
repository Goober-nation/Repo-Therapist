package service

import (
	"aggregator-service/internal/domain"
	"errors"
	"testing"
	"time"
)

func TestGradeLimiterSlidingWindow(t *testing.T) {
	limiter := NewGradeLimiter(2, 10*time.Minute)
	start := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

	if ok, _ := limiter.Reserve(start); !ok {
		t.Fatal("first start rejected")
	}
	if ok, _ := limiter.Reserve(start.Add(time.Minute)); !ok {
		t.Fatal("second start rejected")
	}

	ok, wait := limiter.Reserve(start.Add(2 * time.Minute))
	if ok || wait != 8*time.Minute {
		t.Fatalf("third start: ok=%v wait=%v, want rejected with 8m wait", ok, wait)
	}

	if ok, _ := limiter.Reserve(start.Add(10*time.Minute + time.Second)); !ok {
		t.Fatal("start after the oldest left the window rejected")
	}
}

func TestGradeLimiterCancelFreesSlot(t *testing.T) {
	limiter := NewGradeLimiter(1, 10*time.Minute)
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

	limiter.Reserve(now)
	limiter.Cancel(now)

	if ok, _ := limiter.Reserve(now.Add(time.Second)); !ok {
		t.Fatal("cancelled slot not freed")
	}
}

func TestGradeLimiterDisabled(t *testing.T) {
	limiter := NewGradeLimiter(0, 10*time.Minute)
	now := time.Now()

	for i := 0; i < 100; i++ {
		if ok, _ := limiter.Reserve(now); !ok {
			t.Fatal("disabled limiter rejected a start")
		}
	}
}

func TestGetGradingOverLimitReleasesLockAndReportsWait(t *testing.T) {
	f := newFixture("public")
	f.svc.GradeLimiter = NewGradeLimiter(1, 10*time.Minute)
	f.svc.GradeLimiter.Reserve(testNow.Add(-4 * time.Minute))

	_, err := f.svc.GetGrading(userCtx(""), "org", "repo")

	var limited domain.RateLimitedError
	if !errors.As(err, &limited) || limited.RetryAfter != 6*time.Minute {
		t.Fatalf("err = %v, want RateLimitedError with 6m wait", err)
	}
	if len(f.grading.events) != 0 {
		t.Errorf("published %d grading requests, want 0", len(f.grading.events))
	}
	if !f.repos.lockFree {
		t.Error("repo lock left busy after a rejected start")
	}
}

func TestGetGradingOverLimitServesOutdatedGrade(t *testing.T) {
	f := newFixture("public")
	f.svc.GradeLimiter = NewGradeLimiter(1, 10*time.Minute)
	f.svc.GradeLimiter.Reserve(testNow)
	score := int16(70)
	// Graded before the repo's last activity, so it is outdated.
	f.grades.public[testRepoID] = domain.RepoGrade{
		SourcecraftUUID: testRepoID,
		Score:           &score,
		GradedAt:        gradedAt(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
	}

	grade, err := f.svc.GetGrading(userCtx(""), "org", "repo")
	if err != nil || grade == nil || *grade.Score != 70 {
		t.Fatalf("grade=%+v err=%v, want the outdated grade", grade, err)
	}
}

func TestJoiningRunningGradingDoesNotCount(t *testing.T) {
	f := newFixture("public")
	f.svc.GradeLimiter = NewGradeLimiter(1, 10*time.Minute)

	_, _ = f.svc.GetGrading(userCtx(""), "org", "repo")
	_, err := f.svc.GetGrading(userCtx(""), "org", "repo")

	if !errors.Is(err, domain.GradingPending) {
		t.Fatalf("second call err = %v, want GradingPending (lock held, not rate limited)", err)
	}
}

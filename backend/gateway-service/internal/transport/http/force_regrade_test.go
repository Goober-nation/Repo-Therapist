package http

import (
	"fmt"
	stdhttp "net/http"
	"strings"
	"testing"
	"time"

	"gateway-service/internal/domain"
)

func TestForceRegradeRouteForwardsSessionAndForce(t *testing.T) {
	svc := &fakeService{}
	rec := serve(t, svc, "POST", "/grading/my-org/my-repo/regrade", map[string]string{
		"X-Admin-Request": "1",
		"Cookie":          "session_id=sess-1",
		"PAT":             "Bearer secret",
	})

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if !svc.grading.Force || svc.grading.SessionID != "sess-1" || svc.grading.OrgSlug != "my-org" || svc.grading.RepoSlug != "my-repo" || svc.pat != "secret" {
		t.Errorf("service got %+v pat=%q", svc.grading, svc.pat)
	}
}

func TestForceRegradeNeedsRequestHeader(t *testing.T) {
	svc := &fakeService{}
	rec := serve(t, svc, "POST", "/grading/o/r/regrade", map[string]string{"Cookie": "session_id=sess-1"})

	if rec.Code != stdhttp.StatusForbidden || svc.called {
		t.Fatalf("status = %d called = %v, want 403 without reaching the service", rec.Code, svc.called)
	}
}

func TestPlainGradingNeverForces(t *testing.T) {
	svc := &fakeService{}
	serve(t, svc, "GET", "/grading/o/r", map[string]string{"Cookie": "session_id=sess-1"})

	if svc.grading.Force {
		t.Error("GET /grading must not force")
	}
}

func TestForceRegradeCooldownSetsRetryAfter(t *testing.T) {
	err := fmt.Errorf("force: %w", domain.ForceRegradeCooldownError{RetryAfter: 30 * time.Minute})
	rec := serve(t, &fakeService{err: err}, "POST", "/grading/o/r/regrade", map[string]string{"X-Admin-Request": "1"})

	if rec.Code != stdhttp.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "1800" {
		t.Errorf("Retry-After = %q, want 1800", got)
	}
	if !strings.Contains(rec.Body.String(), "force regrade cooldown") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestForceRegradeWithoutSessionIs401(t *testing.T) {
	err := fmt.Errorf("%w: no session", domain.ErrUnauthenticated)
	rec := serve(t, &fakeService{err: err}, "POST", "/grading/o/r/regrade", map[string]string{"X-Admin-Request": "1"})

	if rec.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

package transport

import (
	"testing"
	"time"

	"repo-therapist/grade-service/internal/contracts"
)

func TestJobStoreTakeRemoves(t *testing.T) {
	s := newJobStore()
	s.put("org/repo", "tok", contracts.VisibilityPrivate)

	j := s.take("org/repo")
	if j.token != "tok" || j.visibility != contracts.VisibilityPrivate {
		t.Fatalf("take = %+v; want token tok, private", j)
	}
	if j := s.take("org/repo"); j.token != "" || j.visibility != contracts.VisibilityPublic {
		t.Errorf("second take = %+v; want public job with no token", j)
	}
}

func TestJobStoreExpiry(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	s := newJobStore()
	s.now = func() time.Time { return now }
	s.put("old/repo", "tok", contracts.VisibilityPrivate)

	now = now.Add(jobTTL + time.Minute)
	if j := s.take("old/repo"); j.token != "" || j.visibility != contracts.VisibilityPublic {
		t.Errorf("expired job = %+v; want public job with no token", j)
	}

	s.put("a/b", "x", contracts.VisibilityPrivate)
	now = now.Add(jobTTL + time.Minute)
	s.put("c/d", "y", contracts.VisibilityPrivate)
	if _, exists := s.jobs["a/b"]; exists {
		t.Error("put should sweep expired jobs")
	}
}

func TestGradingVisibility(t *testing.T) {
	cases := []struct {
		visibility, token string
		want              string
		ok                bool
	}{
		{"public", "", contracts.VisibilityPublic, true},
		{"", "", contracts.VisibilityPublic, true},
		{"public", "tok", contracts.VisibilityPrivate, true},
		{"private", "tok", contracts.VisibilityPrivate, true},
		{"private", "", "", false},
		{"internal", "", "", false},
	}
	for _, c := range cases {
		got, ok := gradingVisibility(contracts.GradingRequestedEvent{Visibility: c.visibility, Token: c.token})
		if got != c.want || ok != c.ok {
			t.Errorf("visibility=%q token=%t: got (%q, %v), want (%q, %v)", c.visibility, c.token != "", got, ok, c.want, c.ok)
		}
	}
}

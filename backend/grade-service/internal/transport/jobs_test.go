package transport

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"repo-therapist/grade-service/internal/contracts"
)

// fakeTokens is an in-memory tokenstore.Store.
type fakeTokens struct {
	tokens  map[string]string
	deleted []string
	getErr  error
}

func (f *fakeTokens) GetToken(_ context.Context, jobID string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	return f.tokens[jobID], nil
}

func (f *fakeTokens) DeleteToken(_ context.Context, jobID string) error {
	delete(f.tokens, jobID)
	f.deleted = append(f.deleted, jobID)
	return nil
}

func TestJobStoreTakeRemoves(t *testing.T) {
	s := newJobStore()
	s.put("job-1", contracts.VisibilityPrivate)

	j, ok := s.take("job-1")
	if !ok || j.visibility != contracts.VisibilityPrivate {
		t.Fatalf("take = %+v, %v; want private", j, ok)
	}
	if _, ok := s.take("job-1"); ok {
		t.Error("second take found the job again")
	}
}

func TestJobStoreOverlappingJobsOfOneRepoAreSeparate(t *testing.T) {
	s := newJobStore()
	s.put("job-a", contracts.VisibilityPrivate)
	s.put("job-b", contracts.VisibilityPublic)

	if j, ok := s.take("job-b"); !ok || j.visibility != contracts.VisibilityPublic {
		t.Fatalf("job-b = %+v, %v; want public", j, ok)
	}
	if j, ok := s.take("job-a"); !ok || j.visibility != contracts.VisibilityPrivate {
		t.Fatalf("job-a = %+v, %v; want private (not overwritten by job-b)", j, ok)
	}
}

func TestJobStoreExpiry(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	s := newJobStore()
	s.now = func() time.Time { return now }
	s.put("old", contracts.VisibilityPrivate)

	now = now.Add(s.ttl() + time.Minute)
	if _, ok := s.take("old"); ok {
		t.Error("expired job was returned")
	}

	s.put("a", contracts.VisibilityPrivate)
	now = now.Add(s.ttl() + time.Minute)
	s.put("c", contracts.VisibilityPrivate)
	if _, exists := s.jobs["a"]; exists {
		t.Error("put should sweep expired jobs")
	}
}

func TestGradingVisibility(t *testing.T) {
	cases := []struct {
		visibility string
		hasToken   bool
		want       string
		ok         bool
	}{
		{"public", false, contracts.VisibilityPublic, true},
		{"", false, contracts.VisibilityPublic, true},
		{"public", true, contracts.VisibilityPrivate, true},
		{"private", true, contracts.VisibilityPrivate, true},
		{"private", false, "", false},
		{"internal", false, "", false},
	}
	for _, c := range cases {
		got, ok := gradingVisibility(contracts.GradingRequestedEvent{Visibility: c.visibility}, c.hasToken)
		if got != c.want || ok != c.ok {
			t.Errorf("visibility=%q token=%t: got (%q, %v), want (%q, %v)", c.visibility, c.hasToken, got, ok, c.want, c.ok)
		}
	}
}

func TestReadyVisibility(t *testing.T) {
	private := job{visibility: contracts.VisibilityPrivate}
	public := job{visibility: contracts.VisibilityPublic}
	cases := []struct {
		name  string
		j     job
		known bool
		token string
		want  string
		ok    bool
	}{
		{"known private with token", private, true, "tok", contracts.VisibilityPrivate, true},
		{"known private token expired", private, true, "", contracts.VisibilityPrivate, false},
		{"known public", public, true, "", contracts.VisibilityPublic, true},
		{"unknown with token", job{}, false, "tok", contracts.VisibilityPrivate, true},
		{"unknown without token", job{}, false, "", contracts.VisibilityPublic, true},
	}
	for _, c := range cases {
		got, ok := readyVisibility(c.j, c.known, c.token)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestRelayGradingRequestResolvesModeByJobIDAndSendsNoToken(t *testing.T) {
	tokens := &fakeTokens{tokens: map[string]string{"job-private": "user-pat"}}
	jobs := newJobStore()

	for _, tc := range []struct {
		jobID, visibility, want string
	}{
		{"job-private", "private", contracts.VisibilityPrivate},
		{"job-public", "public", contracts.VisibilityPublic},
	} {
		value, _ := json.Marshal(contracts.GradingRequestedEvent{JobID: tc.jobID, OrgSlug: "o", RepoSlug: "r", Visibility: tc.visibility})
		out, ok, err := relayGradingRequest(context.Background(), jobs, tokens, value)
		if err != nil || !ok || out.JobID != tc.jobID {
			t.Fatalf("%s: out=%+v ok=%v err=%v", tc.jobID, out, ok, err)
		}
		payload, _ := json.Marshal(out)
		if strings.Contains(string(payload), "user-pat") || strings.Contains(string(payload), `"token"`) {
			t.Fatalf("repo.grade.requested carries the token: %s", payload)
		}
		if j, _ := jobs.take(tc.jobID); j.visibility != tc.want {
			t.Errorf("%s: remembered %q, want %q", tc.jobID, j.visibility, tc.want)
		}
	}
	if len(tokens.deleted) != 0 {
		t.Error("relay must not delete the token: Repository and Security still need it")
	}
}

func TestRelayGradingRequestDropsAndFails(t *testing.T) {
	jobs := newJobStore()
	tokens := &fakeTokens{tokens: map[string]string{}}

	for _, value := range []string{`not json`, `{"org_slug":"o","repo_slug":"r"}`, `{"job_id":"j","org_slug":"o","repo_slug":"r","visibility":"private"}`} {
		if _, ok, err := relayGradingRequest(context.Background(), jobs, tokens, []byte(value)); ok || err != nil {
			t.Errorf("%s: ok=%v err=%v, want dropped", value, ok, err)
		}
	}

	tokens.getErr = errors.New("redis down")
	if _, _, err := relayGradingRequest(context.Background(), jobs, tokens, []byte(`{"job_id":"j","visibility":"public"}`)); err == nil {
		t.Error("a Redis error must be returned so the message is redelivered")
	}
}

func TestTokenReleaserDeletesOnce(t *testing.T) {
	tokens := &fakeTokens{tokens: map[string]string{"job": "user-pat"}}
	release := tokenReleaser(tokens, "job")
	release()
	release()
	if len(tokens.deleted) != 1 || tokens.tokens["job"] != "" {
		t.Fatalf("deleted = %v, want exactly one delete of job", tokens.deleted)
	}
}

// A request left uncommitted by a crash comes back hours later; relaying it would clone the repo
// for a job nobody waits for, so it is dropped. Fresh and timeless requests still go through.
func TestRelayGradingRequestDropsStaleRequests(t *testing.T) {
	t.Setenv("GRADE_REQUEST_MAX_AGE", "")
	tokens := &fakeTokens{tokens: map[string]string{}}

	for _, tc := range []struct {
		name        string
		requestedAt time.Time
		want        bool
	}{
		{"stale", time.Now().Add(-10 * time.Hour), false},
		{"fresh", time.Now().Add(-time.Minute), true},
		{"no time", time.Time{}, true},
	} {
		value, _ := json.Marshal(contracts.GradingRequestedEvent{JobID: "j-" + tc.name, OrgSlug: "o", RepoSlug: "r", Visibility: "public", RequestedAt: tc.requestedAt})
		_, ok, err := relayGradingRequest(context.Background(), newJobStore(), tokens, value)
		if err != nil || ok != tc.want {
			t.Errorf("%s: ok=%v err=%v, want ok=%v", tc.name, ok, err, tc.want)
		}
	}

	t.Setenv("GRADE_REQUEST_MAX_AGE", "0")
	value, _ := json.Marshal(contracts.GradingRequestedEvent{JobID: "j-off", OrgSlug: "o", RepoSlug: "r", Visibility: "public", RequestedAt: time.Now().Add(-10 * time.Hour)})
	if _, ok, _ := relayGradingRequest(context.Background(), newJobStore(), tokens, value); !ok {
		t.Error("GRADE_REQUEST_MAX_AGE=0 must turn the check off")
	}
}

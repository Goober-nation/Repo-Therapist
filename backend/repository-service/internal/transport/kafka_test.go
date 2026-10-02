package transport

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"repo-therapist/repository-service/internal/contracts"
	"repo-therapist/repository-service/internal/service"
)

func TestHandleRequestFailureKeepsResolvedUUID(t *testing.T) {
	orig := cloneAndUpload
	t.Cleanup(func() { cloneAndUpload = orig })
	req := contracts.GradeRequested{OrgSlug: "org", RepoSlug: "repo"}

	cloneAndUpload = func(context.Context, string, string, string, string) (*service.Result, error) {
		return &service.Result{SourcecraftUUID: "uuid-1"}, errors.New("clone failed")
	}
	got := handleRequest(context.Background(), fakeTokens{}, req)
	if got.Status != contracts.StatusFailed || got.SourcecraftUUID != "uuid-1" || got.Error != "clone failed" {
		t.Fatalf("failure after id resolution = %+v, want failed with uuid-1", got)
	}

	cloneAndUpload = func(context.Context, string, string, string, string) (*service.Result, error) {
		return nil, errors.New("repo not found")
	}
	got = handleRequest(context.Background(), fakeTokens{}, req)
	if got.Status != contracts.StatusFailed || got.SourcecraftUUID != "" {
		t.Fatalf("failure before id resolution = %+v, want failed without uuid", got)
	}

	cloneAndUpload = func(context.Context, string, string, string, string) (*service.Result, error) {
		return &service.Result{SourcecraftUUID: "uuid-2", S3Prefix: "repos/uuid-2"}, nil
	}
	got = handleRequest(context.Background(), fakeTokens{}, req)
	if got.Status != contracts.StatusReady || got.SourcecraftUUID != "uuid-2" || got.S3Prefix != "repos/uuid-2" {
		t.Fatalf("success = %+v", got)
	}
}

type fakeTokens map[string]string

func (f fakeTokens) GetToken(_ context.Context, jobID string) (string, error) {
	if jobID == "redis-down" {
		return "", errors.New("redis unavailable")
	}
	return f[jobID], nil
}

func TestHandleRequestResolvesTokenByJobID(t *testing.T) {
	orig := cloneAndUpload
	t.Cleanup(func() { cloneAndUpload = orig })
	var gotJob, gotToken string
	cloneAndUpload = func(_ context.Context, _, _, jobID, token string) (*service.Result, error) {
		gotJob, gotToken = jobID, token
		return &service.Result{SourcecraftUUID: "u", S3Prefix: service.S3Prefix("u", jobID)}, nil
	}
	tokens := fakeTokens{"job-private": "user-pat"}

	ready := handleRequest(context.Background(), tokens, contracts.GradeRequested{JobID: "job-private", OrgSlug: "o", RepoSlug: "r"})
	if gotJob != "job-private" || gotToken != "user-pat" {
		t.Fatalf("clone got job=%q token set=%t, want job-private with the stored token", gotJob, gotToken != "")
	}
	if ready.JobID != "job-private" || ready.S3Prefix != "repos/u/job-private" {
		t.Fatalf("ready = %+v, want job id echoed and a per-job prefix", ready)
	}
	payload, _ := json.Marshal(ready)
	if strings.Contains(string(payload), "user-pat") || strings.Contains(string(payload), `"token"`) {
		t.Fatalf("repo.ready carries the token: %s", payload)
	}

	handleRequest(context.Background(), tokens, contracts.GradeRequested{JobID: "job-public", OrgSlug: "o", RepoSlug: "r"})
	if gotToken != "" {
		t.Fatal("job without a stored token must use the service PAT (empty token)")
	}
}

func TestHandleRequestFailsWhenTokenStoreFails(t *testing.T) {
	orig := cloneAndUpload
	t.Cleanup(func() { cloneAndUpload = orig })
	called := false
	cloneAndUpload = func(context.Context, string, string, string, string) (*service.Result, error) {
		called = true
		return nil, nil
	}
	ready := handleRequest(context.Background(), fakeTokens{}, contracts.GradeRequested{JobID: "redis-down"})
	if called || ready.Status != contracts.StatusFailed || ready.JobID != "redis-down" {
		t.Fatalf("ready = %+v called=%t, want failed without cloning", ready, called)
	}
}

func TestGradeRequestedHasNoTokenField(t *testing.T) {
	var m map[string]any
	payload, _ := json.Marshal(contracts.GradeRequested{JobID: "j", OrgSlug: "o", RepoSlug: "r"})
	_ = json.Unmarshal(payload, &m)
	if _, ok := m["token"]; ok || m["job_id"] != "j" {
		t.Fatalf("payload = %s", payload)
	}
}

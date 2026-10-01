package transport

import (
	"context"
	"errors"
	"testing"

	"repo-therapist/repository-service/internal/contracts"
	"repo-therapist/repository-service/internal/service"
)

func TestHandleRequestFailureKeepsResolvedUUID(t *testing.T) {
	orig := cloneAndUpload
	t.Cleanup(func() { cloneAndUpload = orig })
	req := contracts.GradeRequested{OrgSlug: "org", RepoSlug: "repo"}

	cloneAndUpload = func(context.Context, string, string, string) (*service.Result, error) {
		return &service.Result{SourcecraftUUID: "uuid-1"}, errors.New("clone failed")
	}
	got := handleRequest(context.Background(), req)
	if got.Status != contracts.StatusFailed || got.SourcecraftUUID != "uuid-1" || got.Error != "clone failed" {
		t.Fatalf("failure after id resolution = %+v, want failed with uuid-1", got)
	}

	cloneAndUpload = func(context.Context, string, string, string) (*service.Result, error) {
		return nil, errors.New("repo not found")
	}
	got = handleRequest(context.Background(), req)
	if got.Status != contracts.StatusFailed || got.SourcecraftUUID != "" {
		t.Fatalf("failure before id resolution = %+v, want failed without uuid", got)
	}

	cloneAndUpload = func(context.Context, string, string, string) (*service.Result, error) {
		return &service.Result{SourcecraftUUID: "uuid-2", S3Prefix: "repos/uuid-2"}, nil
	}
	got = handleRequest(context.Background(), req)
	if got.Status != contracts.StatusReady || got.SourcecraftUUID != "uuid-2" || got.S3Prefix != "repos/uuid-2" {
		t.Fatalf("success = %+v", got)
	}
}

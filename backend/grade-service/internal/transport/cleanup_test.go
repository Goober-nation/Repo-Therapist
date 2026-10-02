package transport

import (
	"context"
	"errors"
	"testing"
)

func TestWithRepoDataCleanupRunsAfterPublishedResult(t *testing.T) {
	var cleaned []string
	handler := withRepoDataCleanup(func(repoID string) { cleaned = append(cleaned, repoID) },
		func(context.Context, []byte) error { return nil })

	if err := handler(context.Background(), []byte(`{"sourcecraft_uuid":"u1","s3_prefix":"repos/u1/j1","status":"failed"}`)); err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if len(cleaned) != 1 || cleaned[0] != "repos/u1/j1" {
		t.Errorf("cleaned = %v; want [repos/u1/j1]", cleaned)
	}
}

func TestWithRepoDataCleanupKeepsDataOnHandlerError(t *testing.T) {
	cleaned := 0
	wantErr := errors.New("publish failed")
	handler := withRepoDataCleanup(func(string) { cleaned++ },
		func(context.Context, []byte) error { return wantErr })

	if err := handler(context.Background(), []byte(`{"sourcecraft_uuid":"u1","s3_prefix":"repos/u1/j1"}`)); !errors.Is(err, wantErr) {
		t.Fatalf("handler error = %v; want %v", err, wantErr)
	}
	if cleaned != 0 {
		t.Errorf("cleanup ran %d times; want 0 (message will be redelivered)", cleaned)
	}
}

func TestWithRepoDataCleanupSkipsMessagesWithoutPrefix(t *testing.T) {
	cleaned := 0
	handler := withRepoDataCleanup(func(string) { cleaned++ },
		func(context.Context, []byte) error { return nil })

	for _, value := range []string{`not json`, `{"org_slug":"o","repo_slug":"r","sourcecraft_uuid":"u1"}`} {
		if err := handler(context.Background(), []byte(value)); err != nil {
			t.Fatalf("handler(%q) error = %v", value, err)
		}
	}
	if cleaned != 0 {
		t.Errorf("cleanup ran %d times; want 0", cleaned)
	}
}

func TestRepoDataCleanupDisabledByKeep(t *testing.T) {
	calls := 0
	del := func(context.Context, string) (int, error) { calls++; return 0, nil }
	if repoDataCleanup(true, del) != nil {
		t.Fatal("repoDataCleanup(keep=true) != nil")
	}
	repoDataCleanup(false, del)("repos/u1/j1")
	if calls != 1 {
		t.Errorf("delete calls = %d; want 1", calls)
	}
}

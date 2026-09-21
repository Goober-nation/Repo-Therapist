//go:build manual

// Manual verification for ListKeys/GetObject/FetchBatch against a real MinIO instance —
// not part of `go test ./...` (needs the docker-compose MinIO + already-uploaded test repo
// running). Run explicitly: go test -tags=manual ./internal/storage/ -run TestBatchReadManual -v
package storage

import (
	"context"
	"testing"
)

func TestBatchReadManual(t *testing.T) {
	ctx := context.Background()
	c, err := NewClient(ctx, Config{
		Endpoint:  "localhost:9000",
		AccessKey: "repotherapist",
		SecretKey: "repotherapist123",
		Bucket:    "repo-content",
	})
	if err != nil {
		t.Fatal(err)
	}

	prefix := "repos/019b2102-fa21-7ab7-8ce1-116af406fb5e"
	keys, err := c.ListKeys(ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) == 0 {
		t.Fatal("expected at least one key, got none — did you run the repository-service test upload first?")
	}
	t.Logf("listed %d keys under %s", len(keys), prefix)

	results := c.FetchBatch(ctx, keys, 4)
	if len(results) != len(keys) {
		t.Fatalf("expected %d results, got %d", len(keys), len(results))
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("fetch %s: %v", r.Key, r.Err)
		}
		if len(r.Data) == 0 {
			t.Errorf("fetch %s: empty data", r.Key)
		}
	}
	t.Logf("fetched %d objects in parallel batches successfully", len(results))
}

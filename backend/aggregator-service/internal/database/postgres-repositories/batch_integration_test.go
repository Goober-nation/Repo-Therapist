package postgres_repositories

import (
	"context"
	"errors"
	"os"
	"testing"

	"aggregator-service/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Needs a Postgres with migration 000009 applied; skipped without POSTGRES_DSN. It creates three public
// repos and one private repo in the organisation "zz-batch-test" and removes them (and its batches)
// afterwards. Every selection is limited to those slugs, so real repos are never touched.
func TestBatchRepositoryLifecycle(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN not set")
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	const org = "zz-batch-test"

	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM regrade_batches WHERE actor_hash = 'feedfacefeedface'`)
		_, _ = pool.Exec(ctx, `DELETE FROM repo_grades_public WHERE sourcecraft_uuid IN (SELECT sourcecraft_uuid FROM repos WHERE org_slug = $1)`, org)
		_, _ = pool.Exec(ctx, `DELETE FROM repos WHERE org_slug = $1`, org)
	}
	cleanup()
	defer cleanup()

	type seed struct {
		slug, visibility, language string
		score                      *int
		ageDays                    int
	}

	score := func(v int) *int { return &v }

	seeds := []seed{
		{"go-low", "public", "Go", score(30), 40},
		{"go-high", "public", "Go", score(90), 2},
		{"rust-never", "public", "Rust", nil, 0},
		{"private-go", "private", "Go", score(30), 40},
	}

	ids := map[string]uuid.UUID{}

	for _, s := range seeds {
		id := uuid.New()
		ids[s.slug] = id

		if _, err := pool.Exec(ctx, `INSERT INTO repos (sourcecraft_uuid, org_slug, repo_slug, visibility, main_language) VALUES ($1, $2, $3, $4, $5)`,
			id, org, s.slug, s.visibility, s.language); err != nil {
			t.Fatal(err)
		}

		if s.score != nil {
			if _, err := pool.Exec(ctx, `INSERT INTO repo_grades_public (sourcecraft_uuid, score, graded_at) VALUES ($1, $2, now() - make_interval(days => $3))`,
				id, *s.score, s.ageDays); err != nil {
				t.Fatal(err)
			}
		}
	}

	repo := NewBatchRepository(pool)
	picked := []string{org + "/go-low", org + "/go-high", org + "/rust-never", org + "/private-go"}

	slugsOf := func(f domain.BatchFilter) []string {
		f.Repos = picked

		got, capped, err := repo.SelectBatchRepos(ctx, f, 50)
		if err != nil || capped {
			t.Fatalf("select %+v: %v capped=%v", f, err, capped)
		}

		var out []string
		for _, c := range got {
			out = append(out, c.RepoSlug)
		}

		return out
	}

	same := func(name string, got []string, want ...string) {
		t.Helper()

		if len(got) != len(want) {
			t.Fatalf("%s: got %v, want %v", name, got, want)
		}

		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: got %v, want %v", name, got, want)
			}
		}
	}

	same("picked only: private is never selected", slugsOf(domain.BatchFilter{}), "go-high", "go-low", "rust-never")
	same("language, case-insensitive", slugsOf(domain.BatchFilter{Languages: []string{"go"}}), "go-high", "go-low")
	same("score range", slugsOf(domain.BatchFilter{MinScore: score(10), MaxScore: score(50)}), "go-low")
	same("min score skips ungraded", slugsOf(domain.BatchFilter{MinScore: score(0)}), "go-high", "go-low")
	same("older than 30 days includes never graded", slugsOf(domain.BatchFilter{OlderThanDays: score(30)}), "go-low", "rust-never")

	capped, wasCapped, err := repo.SelectBatchRepos(ctx, domain.BatchFilter{Repos: picked}, 2)
	if err != nil || !wasCapped || len(capped) != 2 {
		t.Fatalf("cap: %v %v %v", capped, wasCapped, err)
	}

	// Columns the full repository loader cannot scan (NULL) must not matter to the batch.
	loaded, err := repo.GetBatchRepo(ctx, ids["go-low"])
	if err != nil || loaded.OrgSlug != org || loaded.RepoSlug != "go-low" || loaded.Visibility != "public" {
		t.Fatalf("GetBatchRepo = %+v, %v", loaded, err)
	}

	if _, err := repo.GetBatchRepo(ctx, uuid.New()); !errors.Is(err, domain.ErrRepositoryNotFound) {
		t.Fatalf("unknown repo: %v", err)
	}

	candidates, _, _ := repo.SelectBatchRepos(ctx, domain.BatchFilter{Repos: picked}, 50)

	id, err := repo.CreateBatch(ctx, "feedfacefeedface", domain.BatchFilter{Languages: []string{"go"}, Repos: picked}, candidates)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.CreateBatch(ctx, "feedfacefeedface", domain.BatchFilter{All: true}, candidates); !errors.Is(err, domain.ErrBatchActive) {
		t.Fatalf("second active batch: %v", err)
	}

	batch, err := repo.GetBatch(ctx, id)
	if err != nil || batch.Total != 3 || batch.Pending != 3 || batch.Status != domain.BatchRunning || len(batch.Filter.Languages) != 1 {
		t.Fatalf("batch = %+v, %v", batch, err)
	}

	active, err := repo.GetActiveBatch(ctx)
	if err != nil || active.ID != id {
		t.Fatalf("active = %+v, %v", active, err)
	}

	first, ok, err := repo.ClaimNextBatchItem(ctx, id)
	if err != nil || !ok || first.RepoSlug != "go-high" || first.Status != domain.BatchItemRunning {
		t.Fatalf("claim = %+v ok=%v err=%v", first, ok, err)
	}

	// A paused batch hands out nothing.
	if changed, err := repo.SetBatchStatus(ctx, id, []string{domain.BatchRunning}, domain.BatchPaused); err != nil || !changed {
		t.Fatalf("pause: %v %v", changed, err)
	}

	if _, ok, _ := repo.ClaimNextBatchItem(ctx, id); ok {
		t.Fatal("a paused batch must not hand out items")
	}

	if changed, _ := repo.SetBatchStatus(ctx, id, []string{domain.BatchRunning}, domain.BatchPaused); changed {
		t.Fatal("pausing a paused batch must report no change")
	}

	if _, err := repo.SetBatchStatus(ctx, id, []string{domain.BatchPaused}, domain.BatchRunning); err != nil {
		t.Fatal(err)
	}

	if err := repo.RequeueRunningItems(ctx, id); err != nil {
		t.Fatal(err)
	}

	again, _, _ := repo.ClaimNextBatchItem(ctx, id)
	if again.RepoSlug != "go-high" {
		t.Fatalf("a requeued item must be claimed again, got %s", again.RepoSlug)
	}

	if err := repo.FinishBatchItem(ctx, id, again.RepoID, domain.BatchItemOK, ""); err != nil {
		t.Fatal(err)
	}

	second, _, _ := repo.ClaimNextBatchItem(ctx, id)
	if err := repo.FinishBatchItem(ctx, id, second.RepoID, domain.BatchItemFailed, domain.BatchErrorTimeout); err != nil {
		t.Fatal(err)
	}

	failed, err := repo.ListBatchItems(ctx, id, domain.BatchItemFailed, 10)
	if err != nil || len(failed) != 1 || failed[0].Error != domain.BatchErrorTimeout {
		t.Fatalf("failed = %+v, %v", failed, err)
	}

	skipped, err := repo.SkipPendingItems(ctx, id)
	if err != nil || skipped != 1 {
		t.Fatalf("skipped %d, %v", skipped, err)
	}

	if avg, err := repo.AvgGradeSeconds(ctx); err != nil || avg < 0 {
		t.Fatalf("avg = %v, %v", avg, err)
	}

	if err := repo.FinishBatch(ctx, id, domain.BatchCancelled); err != nil {
		t.Fatal(err)
	}

	// An item left running in a finished batch is cleared at startup.
	if err := repo.SkipOrphanedItems(ctx); err != nil {
		t.Fatal(err)
	}

	done, _ := repo.GetBatch(ctx, id)
	if done.Status != domain.BatchCancelled || done.FinishedAt == nil || done.OK != 1 || done.Failed != 1 || done.Skipped != 1 {
		t.Fatalf("done = %+v", done)
	}

	if _, err := repo.GetActiveBatch(ctx); !errors.Is(err, domain.ErrBatchNotFound) {
		t.Fatalf("no active batch expected, got %v", err)
	}

	if _, err := repo.GetBatch(ctx, -1); !errors.Is(err, domain.ErrBatchNotFound) {
		t.Fatalf("unknown batch: %v", err)
	}

	// A new batch is allowed once the old one is over.
	if _, err := repo.CreateBatch(ctx, "feedfacefeedface", domain.BatchFilter{All: true}, candidates[:1]); err != nil {
		t.Fatalf("batch after a finished one: %v", err)
	}
}

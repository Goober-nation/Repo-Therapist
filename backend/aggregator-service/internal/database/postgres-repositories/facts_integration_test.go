package postgres_repositories

import (
	"context"
	"os"
	"testing"
	"time"

	"aggregator-service/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Needs a Postgres with migration 000007 applied; skipped without POSTGRES_DSN. It creates its own
// repository and removes everything afterwards.
func TestRepositoryDescriptionAndFactsRoundTrip(t *testing.T) {
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

	id := uuid.New()
	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM repo_facts_public WHERE sourcecraft_uuid = $1`,
			`DELETE FROM repo_lock WHERE sourcecraft_uuid = $1`,
			`DELETE FROM repos WHERE sourcecraft_uuid = $1`,
		} {
			_, _ = pool.Exec(ctx, q, id)
		}
	}
	defer cleanup()

	repos := NewRepoRepository(pool)
	repo := domain.Repo{SourcecraftUUID: id, OrgSlug: "zz-facts-org", RepoSlug: "zz-facts-" + id.String()[:8], Visibility: "public", Description: "Первое описание"}
	if _, err := repos.CreateRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}

	var description string
	read := func() string {
		if err := pool.QueryRow(ctx, `SELECT COALESCE(description, '') FROM repos WHERE sourcecraft_uuid = $1`, id).Scan(&description); err != nil {
			t.Fatal(err)
		}
		return description
	}
	if got := read(); got != "Первое описание" {
		t.Errorf("description after create = %q", got)
	}

	repo.Description = "Обновлённое описание"
	if _, err := repos.UpdateRepository(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "Обновлённое описание" {
		t.Errorf("description after update = %q", got)
	}

	grades := NewGradesRepository(pool)
	files, loc, ci := int32(14), int32(2600), true
	at := time.Now()
	if err := grades.UpsertPublicFacts(ctx, id, domain.RepoFacts{Files: &files, LOC: &loc, HasCI: &ci}, &at); err != nil {
		t.Fatal(err)
	}

	var gotFiles, gotLOC *int32
	var gotCI, gotSecurity *bool
	query := func() {
		if err := pool.QueryRow(ctx, `SELECT files, loc, has_ci, has_security_policy FROM repo_facts_public WHERE sourcecraft_uuid = $1`, id).Scan(&gotFiles, &gotLOC, &gotCI, &gotSecurity); err != nil {
			t.Fatal(err)
		}
	}
	query()
	if gotFiles == nil || *gotFiles != 14 || gotLOC == nil || *gotLOC != 2600 || gotCI == nil || !*gotCI || gotSecurity != nil {
		t.Errorf("facts = %v %v %v %v, want 14, 2600, true and an unknown (NULL) security policy", gotFiles, gotLOC, gotCI, gotSecurity)
	}

	// A second grading replaces the row, including turning a known fact back to unknown.
	secure := true
	if err := grades.UpsertPublicFacts(ctx, id, domain.RepoFacts{HasSecurityPolicy: &secure}, &at); err != nil {
		t.Fatal(err)
	}
	query()
	if gotFiles != nil || gotCI != nil || gotSecurity == nil || !*gotSecurity {
		t.Errorf("after the second upsert: %v %v %v, want only the security policy known", gotFiles, gotCI, gotSecurity)
	}

	if err := grades.DeletePublicFacts(ctx, id); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM repo_facts_public WHERE sourcecraft_uuid = $1`, id).Scan(&n); err != nil || n != 0 {
		t.Errorf("rows after delete = %d (err %v), want 0", n, err)
	}
}

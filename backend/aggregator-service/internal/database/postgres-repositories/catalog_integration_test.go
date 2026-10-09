package postgres_repositories

import (
	"context"
	"os"
	"testing"

	"aggregator-service/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Needs a Postgres with migrations up to 000011 applied; skipped without POSTGRES_DSN. It runs one
// refresh with repos of the organisation "zz-catalog-test" and restores nothing else: run it only
// against a disposable database, the catalog it commits replaces the stored one.
func TestCatalogRefreshAndSelect(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" || os.Getenv("CATALOG_TEST_DISPOSABLE_DB") != "1" {
		t.Skip("POSTGRES_DSN and CATALOG_TEST_DISPOSABLE_DB=1 not set")
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	const org = "zz-catalog-test"

	catalog := NewCatalogRepository(pool)

	repo := func(slug, language string, rating float64) domain.Repo {
		return domain.Repo{SourcecraftUUID: uuid.New(), OrgSlug: org, RepoSlug: slug, Visibility: domain.VisibilityPublic,
			Language: domain.PrimaryLanguage{Name: language}, RatingValue: rating}
	}

	graded, fresh, rust := repo("go-graded", "Go", 2), repo("go-new", "Go", 7), repo("rust-new", "Rust", 4)

	id, err := catalog.StartRefresh(ctx, domain.CatalogTriggerAdmin, "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := catalog.StartRefresh(ctx, domain.CatalogTriggerAdmin, ""); err != domain.ErrCatalogRefreshActive {
		t.Fatalf("second refresh: %v", err)
	}

	// The same repo on two pages is stored once.
	if err := catalog.StagePage(ctx, id, []domain.Repo{graded, fresh}, 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := catalog.StagePage(ctx, id, []domain.Repo{rust, fresh}, 2, 4); err != nil {
		t.Fatal(err)
	}

	size, err := catalog.CommitRefresh(ctx, id)
	if err != nil || size != 3 {
		t.Fatalf("commit: size %d, %v", size, err)
	}

	// A grade needs its repo row.
	if _, err := pool.Exec(ctx, `INSERT INTO repos (sourcecraft_uuid, org_slug, repo_slug, visibility) VALUES ($1, $2, $3, 'public')`,
		graded.SourcecraftUUID, org, graded.RepoSlug); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO repo_grades_public (sourcecraft_uuid, score, graded_at) VALUES ($1, 50, now())`, graded.SourcecraftUUID); err != nil {
		t.Fatal(err)
	}

	slugs := func(f domain.CatalogFilter, limit int) ([]string, int) {
		entries, matched, err := catalog.Select(ctx, f, limit)
		if err != nil {
			t.Fatal(err)
		}

		var out []string
		for _, e := range entries {
			out = append(out, e.Repo.RepoSlug)
		}

		return out, matched
	}

	if got, matched := slugs(domain.CatalogFilter{}, 2); matched != 3 || len(got) != 2 || got[0] != "go-new" || got[1] != "rust-new" {
		t.Errorf("limit 2: %v of %d, want the two highest rated of 3", got, matched)
	}
	if got, _ := slugs(domain.CatalogFilter{OnlyRegrade: true}, 10); len(got) != 1 || got[0] != "go-graded" {
		t.Errorf("only regrade: %v", got)
	}
	if got, _ := slugs(domain.CatalogFilter{Languages: []string{"go"}, MinScore: intPtr(10)}, 10); len(got) != 1 || got[0] != "go-graded" {
		t.Errorf("language + score: %v", got)
	}
	if got, _ := slugs(domain.CatalogFilter{Repos: []string{org + "/rust-new"}}, 10); len(got) != 1 || got[0] != "rust-new" {
		t.Errorf("picked: %v", got)
	}
}

func intPtr(v int) *int { return &v }

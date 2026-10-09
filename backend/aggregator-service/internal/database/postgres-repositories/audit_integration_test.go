package postgres_repositories

import (
	"context"
	"os"
	"testing"
	"time"

	"aggregator-service/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Needs a Postgres with migration 000008 applied; skipped without POSTGRES_DSN (e.g.
// POSTGRES_DSN=postgres://user:pass@localhost:8051/repotherapist?sslmode=disable). The rows it writes
// carry the actor hash "feedfacefeedface" and are removed afterwards.
func TestAuditRepositoryInsertListAndPurge(t *testing.T) {
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

	const marker = "feedfacefeedface"

	cleanup := func() { _, _ = pool.Exec(ctx, `DELETE FROM audit_log WHERE actor_hash = $1`, marker) }
	cleanup()
	defer cleanup()

	repo := NewAuditRepository(pool)

	events := []domain.AuditEvent{
		{ActorKind: "admin", ActorHash: marker, Action: "admin.denied", TargetKind: "route", Target: "/admin/audit", Outcome: "denied", Details: []byte(`{"reason":"not_admin"}`)},
		{ActorKind: "admin", ActorHash: marker, Action: "batch_grading.start", TargetKind: "batch", Target: "b1", Outcome: "ok"},
		{ActorKind: "admin", ActorHash: marker, Action: "batch_grading.cancel", TargetKind: "batch", Target: "b1", Outcome: "ok"},
	}

	for _, event := range events {
		if err := repo.InsertAudit(ctx, event); err != nil {
			t.Fatalf("insert %s: %v", event.Action, err)
		}
	}

	all, err := repo.ListAudit(ctx, domain.AuditFilter{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}

	var mine []domain.AuditEntry

	for _, entry := range all {
		if entry.ActorHash == marker {
			mine = append(mine, entry)
		}
	}

	if len(mine) != 3 || mine[0].Action != "batch_grading.cancel" || mine[2].Action != "admin.denied" {
		t.Fatalf("newest-first order broken: %+v", mine)
	}

	if string(mine[2].Details) != `{"reason": "not_admin"}` && string(mine[2].Details) != `{"reason":"not_admin"}` {
		t.Errorf("details = %s", mine[2].Details)
	}

	if string(mine[1].Details) != `{}` {
		t.Errorf("missing details must be stored as {}, got %s", mine[1].Details)
	}

	prefix, err := repo.ListAudit(ctx, domain.AuditFilter{Limit: 200, Action: "regrade."})
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range prefix {
		if len(entry.Action) < 8 || entry.Action[:8] != "regrade." {
			t.Errorf("prefix filter returned %q", entry.Action)
		}
	}

	exact, _ := repo.ListAudit(ctx, domain.AuditFilter{Limit: 200, Action: "admin.denied", Outcome: "denied"})
	found := false

	for _, entry := range exact {
		found = found || entry.ActorHash == marker
	}

	if !found {
		t.Error("exact action plus outcome filter lost the entry")
	}

	older, err := repo.ListAudit(ctx, domain.AuditFilter{Limit: 200, BeforeID: mine[0].ID})
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range older {
		if entry.ID >= mine[0].ID {
			t.Errorf("cursor returned id %d, not below %d", entry.ID, mine[0].ID)
		}
	}

	// Purge: age one row beyond the cutoff, delete before it, only that row goes.
	if _, err := pool.Exec(ctx, `UPDATE audit_log SET occurred_at = now() - interval '40 days' WHERE id = $1`, mine[2].ID); err != nil {
		t.Fatal(err)
	}

	removed, err := repo.DeleteAuditBefore(ctx, time.Now().AddDate(0, 0, -30))
	if err != nil {
		t.Fatal(err)
	}

	if removed < 1 {
		t.Errorf("removed = %d, want at least the aged row", removed)
	}

	left, _ := repo.ListAudit(ctx, domain.AuditFilter{Limit: 200})
	count := 0

	for _, entry := range left {
		if entry.ActorHash == marker {
			count++
		}
	}

	if count != 2 {
		t.Errorf("rows left after purge = %d, want 2", count)
	}
}

// The scope filter splits the log by action prefix. Rows carry the actor hash "beadbeadbeadbead".
func TestAuditRepositoryScopeSplitsUserActionsFromTheAdminJournal(t *testing.T) {
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

	const marker = "beadbeadbeadbead"

	cleanup := func() { _, _ = pool.Exec(ctx, `DELETE FROM audit_log WHERE actor_hash = $1`, marker) }
	cleanup()
	defer cleanup()

	repo := NewAuditRepository(pool)

	for _, action := range []string{"grading.request", "recommendations.request", "admin.denied", "batch_grading.start", "gradingx.other"} {
		if err := repo.InsertAudit(ctx, domain.AuditEvent{ActorKind: "user", ActorHash: marker, Action: action, Outcome: "ok"}); err != nil {
			t.Fatalf("insert %s: %v", action, err)
		}
	}

	actionsOf := func(scope string) map[string]bool {
		entries, err := repo.ListAudit(ctx, domain.AuditFilter{Limit: 200, Scope: scope})
		if err != nil {
			t.Fatalf("scope %q: %v", scope, err)
		}

		got := map[string]bool{}

		for _, entry := range entries {
			if entry.ActorHash == marker {
				got[entry.Action] = true
			}
		}

		return got
	}

	users, admin, all := actionsOf(domain.AuditScopeUsers), actionsOf(domain.AuditScopeAdmin), actionsOf("")

	if len(users) != 2 || !users["grading.request"] || !users["recommendations.request"] {
		t.Errorf("users scope = %v, want exactly the two user actions", users)
	}

	if len(admin) != 3 || users["admin.denied"] || !admin["admin.denied"] || !admin["batch_grading.start"] || !admin["gradingx.other"] {
		t.Errorf("admin scope = %v, want the other three", admin)
	}

	if len(all) != 5 {
		t.Errorf("no scope = %v, want all five", all)
	}
}

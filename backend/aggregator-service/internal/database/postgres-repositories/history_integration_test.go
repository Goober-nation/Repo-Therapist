package postgres_repositories

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"aggregator-service/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Needs a Postgres with migration 000006 applied and at least one repo; skipped without
// POSTGRES_DSN (e.g. POSTGRES_DSN=postgres://user:pass@localhost:8051/repotherapist?sslmode=disable).
func TestAppendPublicHistoryIsAppendOnlyAndIdempotent(t *testing.T) {
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

	var repoID domain.RepoGrade
	if err := pool.QueryRow(ctx, `SELECT sourcecraft_uuid FROM repos LIMIT 1`).Scan(&repoID.SourcecraftUUID); err != nil {
		t.Skipf("no repo to attach a history point to: %v", err)
	}

	// A point in year 2000 cannot collide with a real grading; the test removes only it.
	gradedAt := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM repo_grade_history WHERE sourcecraft_uuid = $1 AND graded_at = $2`, repoID.SourcecraftUUID, gradedAt)
	}
	cleanup()
	defer cleanup()

	score := int16(61)
	grade := domain.RepoGrade{
		SourcecraftUUID: repoID.SourcecraftUUID,
		Score:           &score,
		CategoryScores:  json.RawMessage(`{"activity":{"score":70}}`),
		GradedAt:        &gradedAt,
		Confidence:      &domain.GradeConfidence{LowConfidence: true},
	}

	repo := NewGradesRepository(pool)
	for i := 0; i < 2; i++ {
		if err := repo.AppendPublicHistory(ctx, grade); err != nil {
			t.Fatalf("append %d: %v", i+1, err)
		}
	}

	var n int
	var gotScore int16
	var low *bool
	err = pool.QueryRow(ctx,
		`SELECT count(*), max(score), bool_or(low_confidence) FROM repo_grade_history WHERE sourcecraft_uuid = $1 AND graded_at = $2`,
		grade.SourcecraftUUID, gradedAt).Scan(&n, &gotScore, &low)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || gotScore != 61 || low == nil || !*low {
		t.Errorf("rows=%d score=%d low=%v after appending the same grading twice, want 1, 61, true", n, gotScore, low)
	}

	// A later grading of the same repo is a new point; the earlier one is untouched.
	later := gradedAt.Add(72 * time.Hour)
	defer func() {
		_, _ = pool.Exec(ctx, `DELETE FROM repo_grade_history WHERE sourcecraft_uuid = $1 AND graded_at = $2`, repoID.SourcecraftUUID, later)
	}()
	score2 := int16(65)
	grade.Score, grade.GradedAt, grade.Confidence = &score2, &later, nil
	if err := repo.AppendPublicHistory(ctx, grade); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM repo_grade_history WHERE sourcecraft_uuid = $1 AND graded_at IN ($2, $3)`,
		grade.SourcecraftUUID, gradedAt, later).Scan(&n); err != nil || n != 2 {
		t.Errorf("points after a second grading = %d (err %v), want 2", n, err)
	}

	// No score or no graded_at: nothing to record, and no error.
	grade.Score = nil
	if err := repo.AppendPublicHistory(ctx, grade); err != nil {
		t.Errorf("a grade without a score should be skipped, got %v", err)
	}
}

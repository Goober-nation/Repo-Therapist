package database

import (
	"context"
	"os"
	"testing"
	"time"

	"rating-service/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Needs a Postgres with migration 000006 applied; skipped without POSTGRES_DSN. It creates its own
// repos in a made-up language (so the filtered list holds only them) and removes them afterwards.
func TestListRankingDynamicsAndHistoryAgainstPostgres(t *testing.T) {
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

	const lang = "ZZDynamicsTest"
	ids := map[string]string{}

	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM repo_grade_history WHERE sourcecraft_uuid IN (SELECT sourcecraft_uuid FROM repos WHERE main_language = $1)`,
			`DELETE FROM repo_grades_public WHERE sourcecraft_uuid IN (SELECT sourcecraft_uuid FROM repos WHERE main_language = $1)`,
			`DELETE FROM repos WHERE main_language = $1`,
		} {
			_, _ = pool.Exec(ctx, q, lang)
		}
	}
	cleanup()
	defer cleanup()

	now := time.Now()
	activity := now.Add(-time.Hour)

	// name -> score now. Past scores (8 days ago, a week-long window): A 60, B 85, C 70; D has no
	// old grade. So the past order is B, C, A and the current one is A, D, B, C.
	current := map[string]int{"zz-a": 90, "zz-d": 85, "zz-b": 80, "zz-c": 70}
	for name, score := range current {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO repos (sourcecraft_uuid, org_slug, repo_slug, visibility, last_activity_at, main_language)
			VALUES (gen_random_uuid(), 'zz-org', $1, 'public', $2, $3) RETURNING sourcecraft_uuid::text`,
			name, activity, lang).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[name] = id

		if _, err := pool.Exec(ctx, `INSERT INTO repo_grades_public (sourcecraft_uuid, score, category_scores, graded_at) VALUES ($1, $2, '{"activity":{"score":50}}', $3)`, id, score, now); err != nil {
			t.Fatal(err)
		}
	}

	addPoint := func(name string, daysAgo float64, score int) {
		at := now.Add(-time.Duration(daysAgo * 24 * float64(time.Hour)))
		if _, err := pool.Exec(ctx, `INSERT INTO repo_grade_history (sourcecraft_uuid, graded_at, score, category_scores) VALUES ($1, $2, $3, '{"activity":{"score":40}}')`, ids[name], at, score); err != nil {
			t.Fatal(err)
		}
	}
	addPoint("zz-a", 8, 60)
	addPoint("zz-b", 8, 85)
	addPoint("zz-c", 8, 70)
	addPoint("zz-c", 2, 72) // a later point inside the window must not be used for the past
	addPoint("zz-a", 0.1, 90)

	repo := NewRankingRepository(pool)

	rows, err := repo.ListRanking(ctx, domain.RankingQuery{Language: lang, SortBy: domain.SortByScore, Limit: 20, DynamicsDays: 7})
	if err != nil {
		t.Fatal(err)
	}

	type want struct {
		name      string
		pastScore *int16
		pastPlace *int
		grades    int
	}
	exp := []want{{"zz-a", i16(60), ip(3), 2}, {"zz-d", nil, nil, 0}, {"zz-b", i16(85), ip(1), 1}, {"zz-c", i16(70), ip(2), 2}}
	if len(rows) != len(exp) {
		t.Fatalf("rows = %d, want %d", len(rows), len(exp))
	}

	for i, e := range exp {
		row := rows[i]
		if row.Name != e.name {
			t.Fatalf("row %d = %s, want %s (current order A, D, B, C)", i, row.Name, e.name)
		}
		if (row.PastScore == nil) != (e.pastScore == nil) || (e.pastScore != nil && *row.PastScore != *e.pastScore) {
			t.Errorf("%s past score = %v, want %v", e.name, row.PastScore, e.pastScore)
		}
		if row.GradeCount != e.grades {
			t.Errorf("%s grade count = %d, want %d stored history points", e.name, row.GradeCount, e.grades)
		}
		if (row.PastPlace == nil) != (e.pastPlace == nil) || (e.pastPlace != nil && *row.PastPlace != *e.pastPlace) {
			t.Errorf("%s past place = %v, want %v", e.name, row.PastPlace, e.pastPlace)
		}
	}

	// A window longer than any history finds no past at all: everything is "new".
	rows, err = repo.ListRanking(ctx, domain.RankingQuery{Language: lang, SortBy: domain.SortByScore, Limit: 20, DynamicsDays: 30})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.PastScore != nil || row.PastPlace != nil {
			t.Errorf("%s has a past grade 30 days ago, want none", row.Name)
		}
	}

	history, err := repo.ListScoreHistory(ctx, "zz-org", "zz-c", 200)
	if err != nil || len(history) != 2 || history[0].Score != 70 || history[1].Score != 72 || !history[0].GradedAt.Before(history[1].GradedAt) {
		t.Errorf("history = %+v err %v, want 70 then 72 oldest first", history, err)
	}

	limited, err := repo.ListScoreHistory(ctx, "zz-org", "zz-c", 1)
	if err != nil || len(limited) != 1 || limited[0].Score != 72 {
		t.Errorf("limited history = %+v err %v, want only the latest point (72)", limited, err)
	}

	if _, err := repo.ListScoreHistory(ctx, "zz-org", "no-such-repo", 200); err != domain.ErrNotFound {
		t.Errorf("unknown repo err = %v, want ErrNotFound", err)
	}

	ranked, err := repo.ListRanked(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, r := range ranked {
		if r.OrgSlug == "zz-org" {
			order = append(order, r.Name)
		}
	}
	if len(order) != 4 || order[0] != "zz-a" || order[1] != "zz-d" || order[2] != "zz-b" || order[3] != "zz-c" {
		t.Errorf("ListRanked order of the test repos = %v, want a, d, b, c", order)
	}
}

func i16(v int16) *int16 { return &v }
func ip(v int) *int      { return &v }

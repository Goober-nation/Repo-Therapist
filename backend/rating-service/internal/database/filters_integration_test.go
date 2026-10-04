package database

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"

	"rating-service/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Needs a Postgres with migration 000007 applied; skipped without POSTGRES_DSN. It creates its own
// repos in a made-up language (so the filtered list holds only them) and removes them afterwards.
func TestRankingFactFiltersAgainstPostgres(t *testing.T) {
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

	const lang = "ZZFilterTest"
	cleanup := func() {
		for _, q := range []string{
			`DELETE FROM repo_facts_public WHERE sourcecraft_uuid IN (SELECT sourcecraft_uuid FROM repos WHERE main_language = $1)`,
			`DELETE FROM repo_grades_public WHERE sourcecraft_uuid IN (SELECT sourcecraft_uuid FROM repos WHERE main_language = $1)`,
			`DELETE FROM repos WHERE main_language = $1`,
		} {
			_, _ = pool.Exec(ctx, q, lang)
		}
	}
	cleanup()
	defer cleanup()

	type fact struct {
		loc     *int
		ci, sec *bool
		noFacts bool
		desc    string
		score   int
	}
	i := func(v int) *int { return &v }
	b := func(v bool) *bool { return &v }

	repos := map[string]fact{
		"zz-tiny":     {loc: i(300), ci: b(false), sec: b(false), desc: "A tiny CLI helper", score: 90},
		"zz-medium":   {loc: i(5000), ci: b(true), sec: b(false), desc: "REST API for payments", score: 80},
		"zz-large":    {loc: i(50000), ci: b(true), sec: b(true), desc: "Big 100%_real platform", score: 70},
		"zz-edge-low": {loc: i(2000), ci: b(true), sec: b(true), desc: "Exactly at the small limit", score: 60},
		"zz-edge-hi":  {loc: i(20000), ci: nil, sec: nil, desc: "Exactly at the large limit, facts partly unknown", score: 50},
		"zz-nofacts":  {noFacts: true, desc: "Graded before facts existed", score: 40},
	}

	for name, f := range repos {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO repos (sourcecraft_uuid, org_slug, repo_slug, visibility, last_activity_at, main_language, description)
			VALUES (gen_random_uuid(), 'zz-org', $1, 'public', $2, $3, $4) RETURNING sourcecraft_uuid::text`,
			name, time.Now(), lang, f.desc).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO repo_grades_public (sourcecraft_uuid, score, category_scores, graded_at) VALUES ($1, $2, '{}', now())`, id, f.score); err != nil {
			t.Fatal(err)
		}
		if !f.noFacts {
			if _, err := pool.Exec(ctx, `INSERT INTO repo_facts_public (sourcecraft_uuid, loc, has_ci, has_security_policy, graded_at) VALUES ($1, $2, $3, $4, now())`, id, f.loc, f.ci, f.sec); err != nil {
				t.Fatal(err)
			}
		}
	}

	repo := NewRankingRepository(pool)
	names := func(extra domain.RankingExtras) []string {
		extra.SmallLOC, extra.LargeLOC = 2000, 20000
		rows, err := repo.ListRanking(ctx, domain.RankingQuery{Language: lang, SortBy: domain.SortByScore, Limit: 50, DynamicsDays: 7, Extra: extra})
		if err != nil {
			t.Fatal(err)
		}
		total, err := repo.CountRanking(ctx, lang, false, domain.RankingExtras{Size: extra.Size, SmallLOC: 2000, LargeLOC: 20000, HasCI: extra.HasCI, HasSecurityPolicy: extra.HasSecurityPolicy, Topic: extra.Topic})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range rows {
			out = append(out, r.Name)
		}
		if total != len(out) {
			t.Errorf("count = %d but the list has %d rows (%v): the count and the list must use the same filters", total, len(out), out)
		}
		sort.Strings(out)
		return out
	}
	equal := func(got []string, want ...string) bool {
		sort.Strings(want)
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	yes, no := true, false
	cases := []struct {
		name  string
		extra domain.RankingExtras
		want  []string
	}{
		{"no filter lists everything, with or without facts", domain.RankingExtras{}, []string{"zz-tiny", "zz-medium", "zz-large", "zz-edge-low", "zz-edge-hi", "zz-nofacts"}},
		{"small is below the small limit", domain.RankingExtras{Size: "small"}, []string{"zz-tiny"}},
		{"medium starts at the small limit and ends below the large one", domain.RankingExtras{Size: "medium"}, []string{"zz-medium", "zz-edge-low"}},
		{"large starts at the large limit", domain.RankingExtras{Size: "large"}, []string{"zz-large", "zz-edge-hi"}},
		{"has CI", domain.RankingExtras{HasCI: &yes}, []string{"zz-medium", "zz-large", "zz-edge-low"}},
		{"lacks CI: an unknown CI fact is not 'lacks'", domain.RankingExtras{HasCI: &no}, []string{"zz-tiny"}},
		{"has a security policy", domain.RankingExtras{HasSecurityPolicy: &yes}, []string{"zz-large", "zz-edge-low"}},
		{"lacks a security policy", domain.RankingExtras{HasSecurityPolicy: &no}, []string{"zz-tiny", "zz-medium"}},
		{"filters combine with AND", domain.RankingExtras{Size: "medium", HasCI: &yes, HasSecurityPolicy: &no}, []string{"zz-medium"}},
		{"topic in the description, any case", domain.RankingExtras{Topic: "rest api"}, []string{"zz-medium"}},
		{"topic in the name", domain.RankingExtras{Topic: "ZZ-TINY"}, []string{"zz-tiny"}},
		{"topic wildcards are literal text", domain.RankingExtras{Topic: "100%_real"}, []string{"zz-large"}},
		{"a percent sign alone is not a wildcard", domain.RankingExtras{Topic: "%"}, []string{"zz-large"}},
		{"topic nothing matches", domain.RankingExtras{Topic: "no-such-thing"}, nil},
		{"topic with a facts filter", domain.RankingExtras{Topic: "limit", HasCI: &yes}, []string{"zz-edge-low"}},
	}

	for _, tc := range cases {
		if got := names(tc.extra); !equal(got, tc.want...) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

package database

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"rating-service/internal/domain"
)

func TestBuildCountQuery(t *testing.T) {
	query, args := buildCountQuery("")
	if !strings.Contains(query, "WHERE r.visibility = 'public'") || strings.Contains(query, "main_language") {
		t.Errorf("unfiltered count query = %q", query)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want none", args)
	}

	query, args = buildCountQuery("Go")
	if !strings.Contains(query, "AND lower(r.main_language) = lower($1)") {
		t.Errorf("filtered count query = %q", query)
	}
	if !reflect.DeepEqual(args, []any{"Go"}) {
		t.Errorf("args = %v, want [Go]", args)
	}
}

func TestBuildListQueryOrder(t *testing.T) {
	cases := []struct {
		sortBy domain.SortBy
		order  string
	}{
		{domain.SortByScore, "ORDER BY g.score DESC NULLS LAST, r.last_activity_at DESC NULLS LAST, r.org_slug, r.repo_slug"},
		{domain.SortByLastActivity, "ORDER BY r.last_activity_at DESC NULLS LAST, g.score DESC NULLS LAST, r.org_slug, r.repo_slug"},
		{domain.SortByLikes, "ORDER BY r.sourcecraft_rating DESC NULLS LAST, g.score DESC NULLS LAST, r.org_slug, r.repo_slug"},
	}

	for _, tc := range cases {
		t.Run(string(tc.sortBy), func(t *testing.T) {
			query, _, err := buildListQuery(domain.RankingQuery{SortBy: tc.sortBy, Limit: 20})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(query, tc.order) {
				t.Errorf("query missing %q:\n%s", tc.order, query)
			}
		})
	}
}

func TestBuildListQueryPlaceholders(t *testing.T) {
	query, args, err := buildListQuery(domain.RankingQuery{SortBy: domain.SortByScore, Limit: 20, Offset: 40})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(query, "LIMIT $1 OFFSET $2") || strings.Contains(query, "main_language) =") {
		t.Errorf("unfiltered list query:\n%s", query)
	}
	if !reflect.DeepEqual(args, []any{20, 40}) {
		t.Errorf("args = %v, want [20 40]", args)
	}

	query, args, err = buildListQuery(domain.RankingQuery{Language: "Go", SortBy: domain.SortByScore, Limit: 20, Offset: 40})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(query, "lower(r.main_language) = lower($1)") || !strings.Contains(query, "LIMIT $2 OFFSET $3") {
		t.Errorf("filtered list query:\n%s", query)
	}
	if !reflect.DeepEqual(args, []any{"Go", 20, 40}) {
		t.Errorf("args = %v, want [Go 20 40]", args)
	}
}

func TestBuildListQueryPublicLeftJoin(t *testing.T) {
	query, _, err := buildListQuery(domain.RankingQuery{SortBy: domain.SortByScore, Limit: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Ungraded repos must stay in the ranking (NULLs last), so the grades join can't be inner.
	if !strings.Contains(query, "LEFT JOIN repo_grades_public g") {
		t.Errorf("expected LEFT JOIN on repo_grades_public:\n%s", query)
	}
	if !strings.Contains(query, "WHERE r.visibility = 'public'") {
		t.Errorf("expected public-only filter:\n%s", query)
	}
}

func TestBuildListQueryColumns(t *testing.T) {
	query, _, err := buildListQuery(domain.RankingQuery{SortBy: domain.SortByScore, Limit: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Name is repo_slug until repos gets a display name column; nullable text is coalesced to ""
	// so the language struct and web_url are always set.
	for _, column := range []string{
		"r.repo_slug,",
		"COALESCE(r.web_url, '')",
		"g.score,",
		"r.sourcecraft_rating,",
		"COALESCE(r.main_language, '')",
		"COALESCE(r.language_color, '')",
	} {
		if !strings.Contains(query, column) {
			t.Errorf("query missing %q:\n%s", column, query)
		}
	}
}

func TestBuildListQueryUnknownSort(t *testing.T) {
	_, _, err := buildListQuery(domain.RankingQuery{SortBy: "stars; DROP TABLE repos", Limit: 20})
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}

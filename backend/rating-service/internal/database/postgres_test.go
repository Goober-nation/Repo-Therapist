package database

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"rating-service/internal/domain"
)

func TestBuildCountQuery(t *testing.T) {
	query, args := buildCountQuery("", false)
	if !strings.Contains(query, "WHERE r.visibility = 'public'") || strings.Contains(query, "main_language") {
		t.Errorf("unfiltered count query = %q", query)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want none", args)
	}

	query, args = buildCountQuery("Go", false)
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

func TestBuildListQueryGradedOnly(t *testing.T) {
	query, _, err := buildListQuery(domain.RankingQuery{SortBy: domain.SortByScore, Limit: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only repos with a public grade are ranked, so the grades join is inner.
	// (The only LEFT JOIN allowed is the optional low-confidence flag, see the tests below.)
	if !strings.Contains(query, "JOIN repo_grades_public g") || strings.Contains(query, "LEFT JOIN repo_grades_public") {
		t.Errorf("expected inner JOIN on repo_grades_public:\n%s", query)
	}
	if !strings.Contains(query, "WHERE r.visibility = 'public'") {
		t.Errorf("expected public-only filter:\n%s", query)
	}

	// The count must apply the same graded-only filter so total_count matches the entries.
	for name, q := range map[string]string{"list": query, "count": func() string { c, _ := buildCountQuery("", false); return c }()} {
		if !strings.Contains(q, "EXISTS (SELECT 1 FROM repo_grades_public pg") {
			t.Errorf("%s query: expected graded-only filter:\n%s", name, q)
		}
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

func TestHideLowConfidenceClauseHidesOnlyExplicitFlags(t *testing.T) {
	count, _ := buildCountQuery("", true)
	list, _, err := buildListQuery(domain.RankingQuery{SortBy: domain.SortByScore, HideLowConfidence: true, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}

	for name, query := range map[string]string{"count": count, "list": list} {
		if !strings.Contains(query, "NOT EXISTS (SELECT 1 FROM repo_confidence_public c WHERE c.sourcecraft_uuid = r.sourcecraft_uuid AND c.low_confidence)") {
			t.Errorf("%s query does not hide flagged repos:\n%s", name, query)
		}
	}

	// A missing flag row must stay visible: the clause is NOT EXISTS of a *true* flag, never an
	// inner join that would drop repos graded before the flag existed.
	if strings.Contains(list, "JOIN repo_confidence_public c ON") && !strings.Contains(list, "LEFT JOIN repo_confidence_public c ON") {
		t.Errorf("the flag table must be LEFT JOINed:\n%s", list)
	}

	for name, query := range map[string]string{
		"count": func() string { c, _ := buildCountQuery("", false); return c }(),
		"list": func() string {
			l, _, _ := buildListQuery(domain.RankingQuery{SortBy: domain.SortByScore, Limit: 20})
			return l
		}(),
	} {
		if strings.Contains(query, "NOT EXISTS (SELECT 1 FROM repo_confidence_public") {
			t.Errorf("%s query hides flagged repos although the toggle is off:\n%s", name, query)
		}
	}
}

func TestListQueryReturnsTheFlagAndKeepsTheOrder(t *testing.T) {
	query, _, err := buildListQuery(domain.RankingQuery{SortBy: domain.SortByScore, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "c.low_confidence,") || !strings.Contains(query, "c.reasons") ||
		!strings.Contains(query, "LEFT JOIN repo_confidence_public c ON c.sourcecraft_uuid = r.sourcecraft_uuid") {
		t.Errorf("list query does not return the flag through a LEFT JOIN:\n%s", query)
	}
	if strings.Contains(strings.SplitN(query, "ORDER BY", 2)[1], "low_confidence") {
		t.Error("the flag must not take part in the ordering")
	}
}

func TestDecodeConfidence(t *testing.T) {
	if got, err := decodeConfidence(nil, nil); got != nil || err != nil {
		t.Errorf("no flag row: got %+v err %v, want nil (unknown)", got, err)
	}

	yes, no := true, false
	got, err := decodeConfidence(&yes, []byte(`[{"code":"LOW_CODE","text":"t"}]`))
	if err != nil || !got.Low || len(got.Reasons) != 1 || got.Reasons[0].Code != "LOW_CODE" || got.Reasons[0].Text != "t" {
		t.Errorf("flagged: got %+v err %v", got, err)
	}

	got, err = decodeConfidence(&no, nil)
	if err != nil || got == nil || got.Low || got.Reasons == nil || len(got.Reasons) != 0 {
		t.Errorf("not flagged: got %+v err %v, want known-not-low with an empty non-nil list", got, err)
	}

	if _, err := decodeConfidence(&yes, []byte(`{"bad":1}`)); err == nil {
		t.Error("malformed reasons: want an error")
	}
}

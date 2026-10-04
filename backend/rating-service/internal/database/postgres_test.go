package database

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"rating-service/internal/domain"
)

func TestBuildCountQuery(t *testing.T) {
	query, args := buildCountQuery("", false, domain.RankingExtras{})
	if !strings.Contains(query, "WHERE r.visibility = 'public'") || strings.Contains(query, "main_language") {
		t.Errorf("unfiltered count query = %q", query)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want none", args)
	}

	query, args = buildCountQuery("Go", false, domain.RankingExtras{})
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
	query, args, err := buildListQuery(domain.RankingQuery{SortBy: domain.SortByScore, Limit: 20, Offset: 40, DynamicsDays: 7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(query, "$1::int") || !strings.Contains(query, "LIMIT $2 OFFSET $3") || strings.Contains(query, "main_language) =") {
		t.Errorf("unfiltered list query:\n%s", query)
	}
	if !reflect.DeepEqual(args, []any{7, 20, 40}) {
		t.Errorf("args = %v, want [7 20 40]", args)
	}

	query, args, err = buildListQuery(domain.RankingQuery{Language: "Go", SortBy: domain.SortByScore, Limit: 20, Offset: 40, DynamicsDays: 7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(query, "lower(r.main_language) = lower($1)") || !strings.Contains(query, "$2::int") || !strings.Contains(query, "LIMIT $3 OFFSET $4") {
		t.Errorf("filtered list query:\n%s", query)
	}
	if !reflect.DeepEqual(args, []any{"Go", 7, 20, 40}) {
		t.Errorf("args = %v, want [Go 7 20 40]", args)
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
	for name, q := range map[string]string{"list": query, "count": func() string { c, _ := buildCountQuery("", false, domain.RankingExtras{}); return c }()} {
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
	count, _ := buildCountQuery("", true, domain.RankingExtras{})
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
		"count": func() string { c, _ := buildCountQuery("", false, domain.RankingExtras{}); return c }(),
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
	// The query's own ORDER BY is the last one; the dynamics CTEs order by score only.
	if strings.Contains(query[strings.LastIndex(query, "ORDER BY"):], "low_confidence") {
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

func boolPtr(v bool) *bool { return &v }

func TestFactFiltersAreOnlyAddedWhenAsked(t *testing.T) {
	query, args := buildCountQuery("", false, domain.RankingExtras{})
	if strings.Contains(query, "repo_facts_public") || strings.Contains(query, "ILIKE") || len(args) != 0 {
		t.Errorf("no extras must add nothing: %q %v", query, args)
	}
}

func TestSizeBucketsUseTheConfiguredLimits(t *testing.T) {
	extra := func(size string) domain.RankingExtras {
		return domain.RankingExtras{Size: size, SmallLOC: 2000, LargeLOC: 20000}
	}

	small, args := buildCountQuery("", false, extra("small"))
	if !strings.Contains(small, "f.loc < $1") || !reflect.DeepEqual(args, []any{2000}) {
		t.Errorf("small: %q %v", small, args)
	}

	medium, args := buildCountQuery("", false, extra("medium"))
	if !strings.Contains(medium, "f.loc >= $1 AND f.loc < $2") || !reflect.DeepEqual(args, []any{2000, 20000}) {
		t.Errorf("medium: %q %v", medium, args)
	}

	large, args := buildCountQuery("", false, extra("large"))
	if !strings.Contains(large, "f.loc >= $1") || !reflect.DeepEqual(args, []any{20000}) {
		t.Errorf("large: %q %v", large, args)
	}
}

func TestCIAndSecurityFiltersCompareToTheFactSoUnknownNeverMatches(t *testing.T) {
	query, args := buildCountQuery("", false, domain.RankingExtras{HasCI: boolPtr(true), HasSecurityPolicy: boolPtr(false)})

	if !strings.Contains(query, "f.has_ci = $1 AND f.has_security_policy = $2") || !reflect.DeepEqual(args, []any{true, false}) {
		t.Errorf("ci/security: %q %v", query, args)
	}
	if strings.Contains(query, "IS NULL") || strings.Contains(query, "NOT EXISTS (SELECT 1 FROM repo_facts_public") {
		t.Errorf("an unknown (NULL) fact must not be matched by 'lacks': %q", query)
	}
}

func TestTopicIsASafeCaseInsensitiveSearchOverNameAndDescription(t *testing.T) {
	query, args := buildCountQuery("Go", false, domain.RankingExtras{Topic: " 50%_off\\ "})

	if !strings.Contains(query, "ILIKE $2") || !strings.Contains(query, "repo_slug ILIKE") || !strings.Contains(query, "COALESCE(r.description, '')") {
		t.Errorf("topic: %q", query)
	}
	// The user's text never reaches the SQL string: it is one argument, with LIKE wildcards made literal.
	if len(args) != 2 || args[1] != "%50\\%\\_off\\\\%" {
		t.Errorf("args = %q, want the trimmed text with %%, _ and the backslash escaped", args)
	}
	if strings.Contains(query, "50") {
		t.Errorf("the topic text leaked into the SQL: %q", query)
	}
}

func TestExtrasKeepPlaceholdersConsistentWithTheListQuery(t *testing.T) {
	query, args, err := buildListQuery(domain.RankingQuery{
		Language: "Go", SortBy: domain.SortByScore, Limit: 20, Offset: 40, DynamicsDays: 7,
		Extra: domain.RankingExtras{Size: "medium", SmallLOC: 2000, LargeLOC: 20000, HasCI: boolPtr(true), Topic: "api"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// language $1, size $2 $3, ci $4, topic $5, then days, limit, offset.
	if !reflect.DeepEqual(args, []any{"Go", 2000, 20000, true, "%api%", 7, 20, 40}) {
		t.Errorf("args = %v", args)
	}
	if !strings.Contains(query, "$6::int") || !strings.Contains(query, "LIMIT $7 OFFSET $8") {
		t.Errorf("dynamics, limit and offset must follow the filter arguments:\n%s", query)
	}
}

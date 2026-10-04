package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"rating-service/internal/domain"
)

func i16(v int16) *int16 { return &v }
func ip(v int) *int      { return &v }

func TestApplyDynamics(t *testing.T) {
	cases := []struct {
		name                 string
		row                  domain.RankingRow
		place                int
		byScore              bool
		wantScore, wantPlace *int
	}{
		{"improved and moved up", domain.RankingRow{PublicScore: i16(80), PastScore: i16(70), PastPlace: ip(5)}, 2, true, ip(10), ip(3)},
		{"dropped and moved down", domain.RankingRow{PublicScore: i16(60), PastScore: i16(65), PastPlace: ip(1)}, 4, true, ip(-5), ip(-3)},
		{"unchanged", domain.RankingRow{PublicScore: i16(60), PastScore: i16(60), PastPlace: ip(2)}, 2, true, ip(0), ip(0)},
		{"no place change when not sorted by score", domain.RankingRow{PublicScore: i16(80), PastScore: i16(70), PastPlace: ip(5)}, 2, false, ip(10), nil},
		{"graded only recently: new", domain.RankingRow{PublicScore: i16(80)}, 1, true, nil, nil},
		{"no score now", domain.RankingRow{PastScore: i16(70), PastPlace: ip(1)}, 1, true, nil, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := tc.row
			applyDynamics(&row, tc.place, tc.byScore)

			if !equalInt(row.ScoreChange, tc.wantScore) {
				t.Errorf("score change = %v, want %v", deref(row.ScoreChange), deref(tc.wantScore))
			}
			if !equalInt(row.PlaceChange, tc.wantPlace) {
				t.Errorf("place change = %v, want %v", deref(row.PlaceChange), deref(tc.wantPlace))
			}
		})
	}
}

func equalInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}

func deref(v *int) any {
	if v == nil {
		return "nil"
	}

	return *v
}

func TestGetRankingFillsChangesAndWindow(t *testing.T) {
	repo := &fakeRepo{total: 2, rows: []domain.RankingRow{
		{SourcecraftUUID: "a", PublicScore: i16(90), PastScore: i16(80), PastPlace: ip(2)},
		{SourcecraftUUID: "b", PublicScore: i16(85)},
	}}

	page, err := NewRatingService(repo, 14).GetRanking(context.Background(), domain.RankingRequest{SortBy: domain.SortByScore})
	if err != nil {
		t.Fatal(err)
	}

	if repo.listQuery.DynamicsDays != 14 || page.DynamicsDays != 14 {
		t.Errorf("window = %d / %d, want 14", repo.listQuery.DynamicsDays, page.DynamicsDays)
	}
	if first := page.Entries[0]; first.Place != 1 || !equalInt(first.ScoreChange, ip(10)) || !equalInt(first.PlaceChange, ip(1)) {
		t.Errorf("first = place %d score change %v place change %v, want 1, 10, 1", first.Place, deref(first.ScoreChange), deref(first.PlaceChange))
	}
	if second := page.Entries[1]; second.ScoreChange != nil || second.PlaceChange != nil {
		t.Errorf("a repo without a past grade must show no change, got %v / %v", deref(second.ScoreChange), deref(second.PlaceChange))
	}
}

func ranked(org, name string, score int, categories string) domain.RankedRow {
	return domain.RankedRow{OrgSlug: org, Name: name, Score: score, CategoryScores: []byte(categories)}
}

func TestExplainPosition(t *testing.T) {
	rows := []domain.RankedRow{
		ranked("o", "r1", 90, `{"activity":{"score":95},"documentation":{"score":90}}`),
		ranked("o", "r2", 80, `{"activity":{"score":85},"documentation":{"score":75}}`),
		ranked("o", "me", 70, `{"activity":{"score":60},"documentation":{"score":80},"issues":{"no_data":true}}`),
		ranked("o", "r4", 70, `{"activity":{"score":65},"documentation":{"score":50}}`),
		ranked("o", "r5", 40, `{"activity":{"score":30},"documentation":{"score":20}}`),
	}

	got, found, err := explainPosition(rows, "o", "me")
	if err != nil || !found {
		t.Fatalf("found = %v err %v", found, err)
	}

	if got.Place != 3 || got.Total != 5 || got.Score != 70 {
		t.Errorf("place %d of %d score %d, want 3 of 5, 70", got.Place, got.Total, got.Score)
	}
	if got.Above == nil || got.Above.Name != "r2" || got.Above.Place != 2 || got.Above.Points != 10 {
		t.Errorf("above = %+v, want r2 at place 2, 10 points ahead", got.Above)
	}
	if got.Below == nil || got.Below.Name != "r4" || got.Below.Place != 4 || got.Below.Points != 0 {
		t.Errorf("below = %+v, want r4 at place 4, 0 points (tie)", got.Below)
	}

	// Medians over the 5 repos here (all within the top 10): activity 65, documentation 75.
	byID := map[string]domain.CategoryComparison{}
	for _, c := range got.Categories {
		byID[c.ID] = c
	}
	if len(got.Categories) != 2 {
		t.Fatalf("categories = %+v, want activity and documentation only (issues has no data)", got.Categories)
	}
	if a := byID["activity"]; a.Top10Median == nil || *a.Top10Median != 65 || *a.Gap != -5 {
		t.Errorf("activity = %+v, want median 65 gap -5", a)
	}
	if d := byID["documentation"]; d.Top10Median == nil || *d.Top10Median != 75 || *d.Gap != 5 {
		t.Errorf("documentation = %+v, want median 75 gap +5", d)
	}
	if got.WeakestCategory != "activity" {
		t.Errorf("weakest = %q, want activity (the only category below the median)", got.WeakestCategory)
	}
}

func TestExplainPositionEdges(t *testing.T) {
	only := []domain.RankedRow{ranked("o", "solo", 50, `{"activity":{"score":50}}`)}
	got, found, err := explainPosition(only, "o", "solo")
	if err != nil || !found {
		t.Fatal(found, err)
	}
	if got.Above != nil || got.Below != nil || got.Place != 1 || got.Total != 1 {
		t.Errorf("a lone repo has no neighbours: %+v", got)
	}
	if len(got.Categories) != 1 || got.Categories[0].Top10Median != nil || got.Categories[0].Gap != nil || got.WeakestCategory != "" {
		t.Errorf("fewer than 3 values must give no median, gap or weakest: %+v", got)
	}

	if _, found, _ := explainPosition(only, "o", "other"); found {
		t.Error("a repo outside the ranking must not be found")
	}

	// Only the top 10 count towards the median: place 12 does not move it.
	var many []domain.RankedRow
	for i := 0; i < 11; i++ {
		many = append(many, ranked("o", string(rune('a'+i)), 100-i, `{"activity":{"score":80}}`))
	}
	many = append(many, ranked("o", "low", 5, `{"activity":{"score":0}}`))
	got, _, _ = explainPosition(many, "o", "low")
	if got.Categories[0].Top10Median == nil || *got.Categories[0].Top10Median != 80 {
		t.Errorf("median = %v, want 80 over the top 10 only", got.Categories[0].Top10Median)
	}
	if got.WeakestCategory != "activity" {
		t.Errorf("weakest = %q, want activity", got.WeakestCategory)
	}
}

func TestMedianInt(t *testing.T) {
	for _, tc := range []struct {
		in   []int
		want int
	}{{[]int{5}, 5}, {[]int{1, 9, 5}, 5}, {[]int{2, 4}, 3}, {[]int{1, 2}, 2}, {[]int{10, 20, 30, 40}, 25}} {
		if got := medianInt(tc.in); got != tc.want {
			t.Errorf("median(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestGetPositionExplanationNotRankedVsUnknown(t *testing.T) {
	// Public but not graded: not in the ranking, GetPublicScore says it exists.
	repo := &fakeRepo{ranked: []domain.RankedRow{ranked("o", "x", 50, `{}`)}}
	got, err := NewRatingService(repo, 0).GetPositionExplanation(context.Background(), "o", "ungraded")
	if err != nil || got.Ranked {
		t.Errorf("ungraded repo: ranked=%v err=%v, want not ranked and no error", got.Ranked, err)
	}

	// Unknown or non-public: ErrNotFound from the existence check.
	repo.scoreErr = domain.ErrNotFound
	if _, err := NewRatingService(repo, 0).GetPositionExplanation(context.Background(), "o", "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown repo err = %v, want ErrNotFound", err)
	}

	if _, err := NewRatingService(repo, 0).GetPositionExplanation(context.Background(), " ", "r"); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("blank org err = %v, want ErrInvalidArgument", err)
	}
}

func TestGetScoreHistory(t *testing.T) {
	t1 := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(72 * time.Hour)
	low := true
	repo := &fakeRepo{history: []domain.HistoryRow{
		{GradedAt: t1, Score: 60, CategoryScores: []byte(`{"issues":{"no_data":true},"activity":{"score":55.4}}`)},
		{GradedAt: t2, Score: 66, CategoryScores: []byte(`{"activity":{"score":70}}`), LowConfidence: &low},
	}}

	points, err := NewRatingService(repo, 0).GetScoreHistory(context.Background(), " org ", "repo ")
	if err != nil {
		t.Fatal(err)
	}

	if repo.historyFor != "org/repo" || repo.historyMax != MaxHistoryPoints {
		t.Errorf("asked for %q with limit %d, want org/repo and %d", repo.historyFor, repo.historyMax, MaxHistoryPoints)
	}
	if len(points) != 2 || points[0].Score != 60 || points[1].Score != 66 || !points[1].GradedAt.After(points[0].GradedAt) {
		t.Fatalf("points = %+v, want oldest first", points)
	}
	if c := points[0].Categories; len(c) != 2 || c[0].ID != "activity" || *c[0].Score != 55 || c[1].ID != "issues" || c[1].Score != nil {
		t.Errorf("categories = %+v, want sorted, rounded, no-data kept nil", c)
	}
	if points[0].LowConfidence != nil || points[1].LowConfidence == nil || !*points[1].LowConfidence {
		t.Error("the unknown flag must stay nil and a known one must carry through")
	}

	repo.historyErr = domain.ErrNotFound
	if _, err := NewRatingService(repo, 0).GetScoreHistory(context.Background(), "o", "r"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if _, err := NewRatingService(repo, 0).GetScoreHistory(context.Background(), "", "r"); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("blank org err = %v, want ErrInvalidArgument", err)
	}
}

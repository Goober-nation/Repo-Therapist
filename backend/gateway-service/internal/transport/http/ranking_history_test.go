package http

import (
	"fmt"
	stdhttp "net/http"
	"testing"
	"time"

	"gateway-service/internal/domain"
)

func i32(v int32) *int32 { return &v }

func TestHistoryReturnsPointsOldestFirst(t *testing.T) {
	low := true
	t1 := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	svc := &fakeService{historyOut: []domain.HistoryPoint{
		{GradedAt: t1, Score: 60, Categories: []domain.PublicCategory{{ID: "activity", Score: i32(55)}, {ID: "issues"}}},
		{GradedAt: t1.Add(72 * time.Hour), Score: 66, LowConfidence: &low},
	}}

	rec := serve(t, svc, "GET", "/history/org/demo", map[string]string{"X-Real-IP": "203.0.113.9"})

	if rec.Code != stdhttp.StatusOK || svc.rankFor != "org/demo" {
		t.Fatalf("status = %d, repo = %q", rec.Code, svc.rankFor)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" || rec.Header().Get("Cache-Control") != "public, max-age=60" {
		t.Errorf("headers = %v, want public CORS and a 60 s cache", rec.Header())
	}

	body := decode(t, rec)
	points := body["points"].([]any)
	if body["status"] != "FINISHED" || len(points) != 2 || body["org"] != "org" || body["repo"] != "demo" {
		t.Fatalf("body = %v", body)
	}

	first := points[0].(map[string]any)
	if first["score"] != float64(60) || first["graded_at"] != "2026-09-28T10:00:00Z" || first["low_confidence"] != nil {
		t.Errorf("first point = %v", first)
	}
	noData := first["categories"].([]any)[1].(map[string]any)
	if noData["score"] != nil || noData["no_data"] != true {
		t.Errorf("no-data category = %v, want null score and no_data", noData)
	}
	if second := points[1].(map[string]any); second["low_confidence"] != true {
		t.Errorf("second point = %v, want low_confidence true", second)
	}
}

func TestHistoryWithoutPointsIsNoData(t *testing.T) {
	body := decode(t, serve(t, &fakeService{}, "GET", "/history/org/demo", nil))
	if pts, ok := body["points"].([]any); body["status"] != "NO_DATA" || !ok || len(pts) != 0 {
		t.Errorf("body = %v, want NO_DATA and an empty list", body)
	}
}

func TestHistoryAndPositionErrors(t *testing.T) {
	cases := map[string]struct {
		err  error
		want int
	}{
		"unknown":      {fmt.Errorf("rpc: %w", domain.ErrNotFound), stdhttp.StatusNotFound},
		"rate limited": {domain.ErrLimitExceeded, stdhttp.StatusTooManyRequests},
		"unavailable":  {fmt.Errorf("rpc: %w", domain.ErrUnavailable), stdhttp.StatusServiceUnavailable},
	}

	for _, route := range []string{"/history/org/demo", "/position/org/demo"} {
		for name, tc := range cases {
			t.Run(route+" "+name, func(t *testing.T) {
				rec := serve(t, &fakeService{err: tc.err}, "GET", route, nil)
				if rec.Code != tc.want {
					t.Errorf("status = %d, want %d", rec.Code, tc.want)
				}
				if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
					t.Error("errors must stay readable cross-origin too")
				}
			})
		}
	}
}

func TestPositionExplainsPlace(t *testing.T) {
	svc := &fakeService{explanationOut: domain.PositionExplanation{
		Ranked: true, Place: 3, Total: 5, Score: 70,
		Above:           &domain.RankNeighbour{Place: 2, OrgSlug: "o", Name: "r2", Score: 80, Points: 10},
		Categories:      []domain.CategoryComparison{{ID: "activity", Score: 60, Top10Median: i32(65), Gap: i32(-5)}, {ID: "documentation", Score: 80}},
		WeakestCategory: "activity",
	}}

	rec := serve(t, svc, "GET", "/position/org/demo", nil)
	body := decode(t, rec)

	if rec.Code != stdhttp.StatusOK || body["status"] != "FINISHED" || body["place"] != float64(3) || body["total"] != float64(5) || body["score"] != float64(70) {
		t.Fatalf("status %d body = %v", rec.Code, body)
	}
	if above := body["above"].(map[string]any); above["name"] != "r2" || above["points"] != float64(10) || above["place"] != float64(2) || above["org"] != "o" {
		t.Errorf("above = %v", above)
	}
	if body["below"] != nil || body["weakest_category"] != "activity" {
		t.Errorf("below = %v weakest = %v, want null and activity", body["below"], body["weakest_category"])
	}

	cats := body["categories"].([]any)
	if a := cats[0].(map[string]any); a["top10_median"] != float64(65) || a["gap"] != float64(-5) {
		t.Errorf("activity = %v", a)
	}
	if d := cats[1].(map[string]any); d["top10_median"] != nil || d["gap"] != nil {
		t.Errorf("documentation = %v, want null median and gap", d)
	}
}

func TestPositionNotRankedIsNoData(t *testing.T) {
	body := decode(t, serve(t, &fakeService{explanationOut: domain.PositionExplanation{Ranked: false}}, "GET", "/position/org/demo", nil))
	if body["status"] != "NO_DATA" || body["place"] != nil || body["score"] != nil || body["above"] != nil {
		t.Errorf("body = %v, want NO_DATA with no place or score", body)
	}
	if cats, ok := body["categories"].([]any); !ok || len(cats) != 0 {
		t.Errorf("categories = %v, want an empty list", body["categories"])
	}
}

func TestRankingResponseCarriesDynamics(t *testing.T) {
	svc := &fakeService{rankingOut: domain.GetRankingResponse{
		DynamicsDays: 7,
		Entries: []domain.RankingEntry{
			{Place: 1, Name: "up", ScoreChange: i32(4), PlaceChange: i32(2), GradeCount: 3},
			{Place: 2, Name: "new"},
		},
	}}

	body := decode(t, serve(t, svc, "GET", "/ranking", nil))
	entries := body["entries"].([]any)

	if body["dynamics_days"] != float64(7) {
		t.Errorf("dynamics_days = %v, want 7", body["dynamics_days"])
	}
	if up := entries[0].(map[string]any); up["score_change"] != float64(4) || up["place_change"] != float64(2) {
		t.Errorf("first = %v", up)
	}
	if entries[0].(map[string]any)["grade_count"] != float64(3) {
		t.Errorf("grade_count = %v, want 3", entries[0].(map[string]any)["grade_count"])
	}
	if fresh := entries[1].(map[string]any); fresh["score_change"] != nil || fresh["place_change"] != nil {
		t.Errorf("second = %v, want null changes for a repo without a past grade", fresh)
	}
}

func TestRankingFactFilterParameters(t *testing.T) {
	svc := &fakeService{}
	rec := serve(t, svc, "GET", "/ranking?size=medium&ci=true&security=false&topic=%20REST%20API%20", nil)
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	got := svc.ranking
	if got.Size != "medium" || got.Topic != "REST API" || got.HasCI == nil || !*got.HasCI || got.HasSecurityPolicy == nil || *got.HasSecurityPolicy {
		t.Errorf("filters = %+v, want medium, 'REST API' (trimmed), ci=true and security=false", got)
	}

	// Not given means any (nil), never false.
	svc = &fakeService{}
	serve(t, svc, "GET", "/ranking", nil)
	if svc.ranking.Size != "" || svc.ranking.HasCI != nil || svc.ranking.HasSecurityPolicy != nil || svc.ranking.Topic != "" {
		t.Errorf("no parameters must leave every filter off: %+v", svc.ranking)
	}

	for _, value := range []string{"true", "1", "TRUE"} {
		svc = &fakeService{}
		serve(t, svc, "GET", "/ranking?ci="+value, nil)
		if svc.ranking.HasCI == nil || !*svc.ranking.HasCI {
			t.Errorf("ci=%s: %v, want true", value, svc.ranking.HasCI)
		}
	}
}

func TestRankingFactFilterParametersAreValidated(t *testing.T) {
	long := ""
	for i := 0; i < 65; i++ {
		long += "я"
	}

	for name, query := range map[string]string{
		"unknown size":   "size=huge",
		"bad ci":         "ci=maybe",
		"bad security":   "security=2x",
		"topic too long": "topic=" + long,
	} {
		svc := &fakeService{}
		if rec := serve(t, svc, "GET", "/ranking?"+query, nil); rec.Code != stdhttp.StatusBadRequest || svc.called {
			t.Errorf("%s: status %d called %v, want 400 without a lookup", name, rec.Code, svc.called)
		}
	}

	exact := ""
	for i := 0; i < 64; i++ {
		exact += "я"
	}

	svc := &fakeService{}
	if rec := serve(t, svc, "GET", "/ranking?topic="+exact, nil); rec.Code != stdhttp.StatusOK {
		t.Errorf("a topic of exactly 64 characters (not bytes) must be accepted, got %d", rec.Code)
	}
}

func TestRegradeStatusIsPublicUncachedAndNullWhenOff(t *testing.T) {
	rec := serve(t, &fakeService{}, "GET", "/regrade-status", map[string]string{"X-Real-IP": "203.0.113.9"})

	if rec.Code != stdhttp.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "*" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status = %d, headers = %v", rec.Code, rec.Header())
	}

	body := decode(t, rec)
	if body["enabled"] != false || body["next_run_at"] != nil || body["running"] != false || body["total"] != float64(0) {
		t.Errorf("body = %v, want an off schedule with null times", body)
	}
}

func TestRegradeStatusReportsScheduleAndProgress(t *testing.T) {
	next := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	svc := &fakeService{regradeOut: domain.RegradeStatus{
		Enabled: true, IntervalSeconds: 259200, NextRunAt: &next, Running: true, StartedAt: &next, Total: 20, Done: 4,
	}}

	body := decode(t, serve(t, svc, "GET", "/regrade-status", map[string]string{"X-Real-IP": "203.0.113.9"}))

	if !svc.called || body["enabled"] != true || body["interval_seconds"] != float64(259200) ||
		body["next_run_at"] != "2026-10-08T12:00:00Z" || body["running"] != true || body["total"] != float64(20) || body["done"] != float64(4) {
		t.Errorf("body = %v", body)
	}
}

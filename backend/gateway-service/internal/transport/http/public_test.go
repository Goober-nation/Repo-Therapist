package http

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	stdhttp "net/http"
	"strings"
	"testing"
	"time"

	"gateway-service/internal/domain"

	"github.com/google/uuid"
)

func ptr(v int32) *int32 { return &v }

func gradedSummary() domain.PublicSummary {
	gradedAt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	return domain.PublicSummary{
		Name:   "demo",
		WebURL: "https://sourcecraft.dev/org/demo",
		Score:  ptr(71),
		Categories: []domain.PublicCategory{
			{ID: "activity", Score: ptr(92)},
			{ID: "code_health", Score: nil},
			{ID: "documentation", Score: ptr(0)},
			{ID: "issues", Score: ptr(64)},
		},
		MissingData: []string{"code_health"},
		GradedAt:    &gradedAt,
	}
}

func TestScoreReturnsStoredGrade(t *testing.T) {
	svc := &fakeService{summary: gradedSummary()}

	rec := serve(t, svc, "GET", "/score/org/demo", map[string]string{"X-Real-IP": "203.0.113.9"})

	if rec.Code != stdhttp.StatusOK || svc.summaryFor != "org/demo" {
		t.Fatalf("status = %d, repo = %q", rec.Code, svc.summaryFor)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("public score API must allow cross-origin reads")
	}
	if rec.Header().Get("Cache-Control") != "public, max-age=60" {
		t.Errorf("cache control = %q", rec.Header().Get("Cache-Control"))
	}

	body := decode(t, rec)
	if body["status"] != "FINISHED" || body["score"] != float64(71) || body["level"] != "healthy" {
		t.Errorf("body = %v", body)
	}
	if body["graded_at"] != "2026-10-01T12:00:00Z" || body["org"] != "org" || body["repo"] != "demo" {
		t.Errorf("body = %v", body)
	}

	categories := body["categories"].([]any)
	if len(categories) != 4 {
		t.Fatalf("categories = %v", categories)
	}
	noData := categories[1].(map[string]any)
	if noData["score"] != nil || noData["no_data"] != true {
		t.Errorf("no-data category = %v, want null score and no_data", noData)
	}
	zero := categories[2].(map[string]any)
	if zero["score"] != float64(0) || zero["no_data"] != false {
		t.Errorf("real zero = %v, want score 0 and no_data false", zero)
	}
}

func TestScoreUngradedIsNoDataNotAnError(t *testing.T) {
	rec := serve(t, &fakeService{summary: domain.PublicSummary{Name: "demo"}}, "GET", "/score/org/demo", nil)

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := decode(t, rec)
	if body["status"] != "NO_DATA" || body["score"] != nil || body["level"] != "no_data" || body["graded_at"] != nil {
		t.Errorf("body = %v", body)
	}
	if categories, ok := body["categories"].([]any); !ok || len(categories) != 0 {
		t.Errorf("categories = %v, want an empty list", body["categories"])
	}
	if missing, ok := body["missing_data"].([]any); !ok || len(missing) != 0 {
		t.Errorf("missing_data = %v, want an empty list", body["missing_data"])
	}
}

func TestScoreErrors(t *testing.T) {
	cases := map[string]struct {
		err  error
		want int
	}{
		"unknown":      {fmt.Errorf("rpc: %w", domain.ErrNotFound), stdhttp.StatusNotFound},
		"rate limited": {domain.ErrLimitExceeded, stdhttp.StatusTooManyRequests},
		"unavailable":  {fmt.Errorf("rpc: %w", domain.ErrUnavailable), stdhttp.StatusServiceUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := serve(t, &fakeService{err: tc.err}, "GET", "/score/org/demo", nil)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
			if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
				t.Error("errors must stay readable cross-origin too")
			}
		})
	}
}

func TestRankingAllowsCrossOrigin(t *testing.T) {
	rec := serve(t, &fakeService{}, "GET", "/ranking", nil)
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("ranking is public and must allow cross-origin reads")
	}
}

func TestShieldsEndpoint(t *testing.T) {
	rec := serve(t, &fakeService{summary: gradedSummary()}, "GET", "/badge/org/demo.json", nil)

	if rec.Code != stdhttp.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("status = %d, cors = %q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content type = %q", ct)
	}

	var got shieldsEndpoint
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if got != (shieldsEndpoint{SchemaVersion: 1, Label: "repo health", Message: "71", Color: "green"}) {
		t.Errorf("shields body = %+v", got)
	}
}

func TestShieldsEndpointFallsBackToNoData(t *testing.T) {
	for name, svc := range map[string]*fakeService{
		"ungraded":    {summary: domain.PublicSummary{}},
		"unknown":     {err: fmt.Errorf("rpc: %w", domain.ErrNotFound)},
		"unavailable": {err: fmt.Errorf("rpc: %w", domain.ErrUnavailable)},
	} {
		t.Run(name, func(t *testing.T) {
			rec := serve(t, svc, "GET", "/badge/org/demo.json", nil)
			var got shieldsEndpoint
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != stdhttp.StatusOK {
				t.Fatalf("status = %d err = %v body = %s", rec.Code, err, rec.Body.String())
			}
			if got.Message != "n/a" || got.Color != "lightgrey" {
				t.Errorf("shields body = %+v, want grey n/a", got)
			}
		})
	}
}

func TestCategoryBadge(t *testing.T) {
	rec := serve(t, &fakeService{summary: gradedSummary()}, "GET", "/badge/org/demo/activity.svg", nil)

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/svg+xml") {
		t.Errorf("content type = %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, ">92<") || !strings.Contains(body, "repo health: activity") || !strings.Contains(body, badgeColor(92)) {
		t.Errorf("body = %s", body)
	}
}

func TestCategoryBadgeNoDataVersusRealZero(t *testing.T) {
	svc := &fakeService{summary: gradedSummary()}

	noData := serve(t, svc, "GET", "/badge/org/demo/code_health.svg", nil).Body.String()
	if !strings.Contains(noData, ">n/a<") || !strings.Contains(noData, badgeNoDataColor) {
		t.Errorf("no-data category = %s, want grey n/a", noData)
	}

	zero := serve(t, svc, "GET", "/badge/org/demo/documentation.svg", nil).Body.String()
	if !strings.Contains(zero, ">0<") || !strings.Contains(zero, badgeColor(0)) {
		t.Errorf("zero category = %s, want a red 0, not n/a", zero)
	}
}

func TestCategoryBadgeRejectsUnknownCategories(t *testing.T) {
	for _, target := range []string{
		"/badge/org/demo/security.svg", // private-only
		"/badge/org/demo/ci_cd.svg",    // private-only
		"/badge/org/demo/%3Cscript%3E.svg",
		"/badge/org/demo/activity", // no suffix
		"/badge/org/demo/activity.png",
	} {
		svc := &fakeService{summary: gradedSummary()}
		rec := serve(t, svc, "GET", target, nil)
		if rec.Code != stdhttp.StatusNotFound || svc.called {
			t.Errorf("%s: status = %d called = %v, want 404 without a lookup", target, rec.Code, svc.called)
		}
	}
}

func TestCategoryBadgeFallsBackToNoData(t *testing.T) {
	for name, svc := range map[string]*fakeService{
		"unknown":     {err: fmt.Errorf("rpc: %w", domain.ErrNotFound)},
		"unavailable": {err: fmt.Errorf("rpc: %w", domain.ErrUnavailable)},
	} {
		t.Run(name, func(t *testing.T) {
			rec := serve(t, svc, "GET", "/badge/org/demo/issues.svg", nil)
			if rec.Code != stdhttp.StatusOK || !strings.Contains(rec.Body.String(), ">n/a<") {
				t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestWidgetRendersCard(t *testing.T) {
	rec := serve(t, &fakeService{summary: gradedSummary()}, "GET", "/widget/org/demo.svg", nil)

	if rec.Code != stdhttp.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "image/svg+xml") {
		t.Fatalf("status = %d content type = %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Cross-Origin-Resource-Policy") != "cross-origin" || rec.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Errorf("headers = %v", rec.Header())
	}

	body := rec.Body.String()
	for _, want := range []string{"org/demo", ">71<", "Здоровый", "Активность", "Документация", "Обновлено 2026-10-01", ">92<", ">n/a<", ">0<"} {
		if !strings.Contains(body, want) {
			t.Errorf("widget lacks %q", want)
		}
	}
	assertWellFormedXML(t, body)
}

func TestWidgetWithoutDataIsGreyNotRed(t *testing.T) {
	for name, svc := range map[string]*fakeService{
		"ungraded": {summary: domain.PublicSummary{}},
		"unknown":  {err: fmt.Errorf("rpc: %w", domain.ErrNotFound)},
	} {
		t.Run(name, func(t *testing.T) {
			rec := serve(t, svc, "GET", "/widget/org/demo.svg", nil)
			body := rec.Body.String()
			if rec.Code != stdhttp.StatusOK || !strings.Contains(body, "Нет данных") || !strings.Contains(body, ">n/a<") {
				t.Fatalf("status = %d body = %s", rec.Code, body)
			}
			if strings.Contains(body, badgeColor(0)) {
				t.Error("a repo without data must not be drawn in the bad-score colour")
			}
			assertWellFormedXML(t, body)
		})
	}
}

func TestWidgetEscapesAndTruncatesName(t *testing.T) {
	svc := &fakeService{err: fmt.Errorf("rpc: %w", domain.ErrNotFound)}
	hostile := `a"><script>alert(1)</script>`

	rec := serve(t, svc, "GET", "/widget/org/"+strings.NewReplacer("<", "%3C", ">", "%3E", `"`, "%22").Replace(hostile)+".svg", nil)

	body := rec.Body.String()
	if strings.Contains(body, "<script>") || strings.Contains(body, `"><`) {
		t.Errorf("widget did not escape the repo name: %s", body)
	}
	assertWellFormedXML(t, body)

	long := renderWidget("organization/"+strings.Repeat("x", 80), domain.PublicSummary{})
	if !strings.Contains(long, "…") || strings.Contains(long, strings.Repeat("x", 40)) {
		t.Error("long names must be truncated")
	}
	assertWellFormedXML(t, long)
}

func TestWidgetNeedsSvgSuffix(t *testing.T) {
	svc := &fakeService{}
	rec := serve(t, svc, "GET", "/widget/org/demo", nil)
	if rec.Code != stdhttp.StatusNotFound || svc.called {
		t.Fatalf("status = %d called = %v, want 404 without a lookup", rec.Code, svc.called)
	}
}

func TestHealthLevelThresholdsMatchBadgeColors(t *testing.T) {
	cases := []struct {
		score *int32
		key   string
	}{
		{nil, "no_data"}, {ptr(100), "excellent"}, {ptr(80), "excellent"}, {ptr(79), "healthy"},
		{ptr(70), "healthy"}, {ptr(69), "attention"}, {ptr(40), "attention"}, {ptr(39), "at_risk"}, {ptr(0), "at_risk"},
	}
	for _, tc := range cases {
		if key, _ := healthLevel(tc.score); key != tc.key {
			t.Errorf("healthLevel(%v) = %q, want %q", tc.score, key, tc.key)
		}
	}

	for _, score := range []int32{0, 59, 60, 74, 75, 89, 90, 100} {
		if got := shieldsColor(score); (got == "red") != (badgeColor(score) == "#e05d44") {
			t.Errorf("score %d: shields colour %q disagrees with badge colour %q", score, got, badgeColor(score))
		}
	}
}

func assertWellFormedXML(t *testing.T, svg string) {
	t.Helper()

	decoder := xml.NewDecoder(strings.NewReader(svg))
	for {
		if _, err := decoder.Token(); err != nil {
			if err.Error() == "EOF" {
				return
			}
			t.Fatalf("SVG is not well-formed XML: %v\n%s", err, svg)
		}
	}
}

func confidenceEntry(id string, confidence *domain.Confidence) domain.RankingEntry {
	return domain.RankingEntry{Place: 1, SourcecraftUUID: uuid.MustParse(id), Name: id, Confidence: confidence}
}

func TestRankingEntriesCarryTheFlagUnknownAsNull(t *testing.T) {
	svc := &fakeService{rankingOut: domain.GetRankingResponse{Entries: []domain.RankingEntry{
		confidenceEntry("11111111-1111-1111-1111-111111111111", &domain.Confidence{Low: true, Reasons: []domain.ConfidenceReason{{Code: "LOW_CODE", Text: "мало кода"}}}),
		confidenceEntry("22222222-2222-2222-2222-222222222222", &domain.Confidence{Low: false}),
		confidenceEntry("33333333-3333-3333-3333-333333333333", nil),
	}}}

	entries := decode(t, serve(t, svc, "GET", "/ranking", nil))["entries"].([]any)

	flagged, clear, unknown := entries[0].(map[string]any), entries[1].(map[string]any), entries[2].(map[string]any)
	if flagged["low_confidence"] != true || len(flagged["confidence_reasons"].([]any)) != 1 {
		t.Errorf("flagged = %v", flagged)
	}
	reason := flagged["confidence_reasons"].([]any)[0].(map[string]any)
	if reason["code"] != "LOW_CODE" || reason["text"] != "мало кода" {
		t.Errorf("reason = %v", reason)
	}
	if clear["low_confidence"] != false || len(clear["confidence_reasons"].([]any)) != 0 {
		t.Errorf("known-not-low = %v, want false and an empty list", clear)
	}
	if v, present := unknown["low_confidence"]; !present || v != nil {
		t.Errorf("unknown = %v, want an explicit null (not an omitted key, not false)", unknown)
	}
	if reasons, ok := unknown["confidence_reasons"].([]any); !ok || len(reasons) != 0 {
		t.Errorf("unknown reasons = %v, want an empty list", unknown["confidence_reasons"])
	}
}

func TestRankingHideLowConfidenceParameter(t *testing.T) {
	cases := map[string]bool{"": false, "false": false, "0": false, "true": true, "1": true, "TRUE": true}
	for value, want := range cases {
		svc := &fakeService{}
		rec := serve(t, svc, "GET", "/ranking?hide_low_confidence="+value, nil)
		if rec.Code != stdhttp.StatusOK || svc.ranking.HideLowConfidence != want {
			t.Errorf("hide_low_confidence=%q: status %d hide %v, want hide %v", value, rec.Code, svc.ranking.HideLowConfidence, want)
		}
	}

	svc := &fakeService{}
	if rec := serve(t, svc, "GET", "/ranking?hide_low_confidence=maybe", nil); rec.Code != stdhttp.StatusBadRequest || svc.called {
		t.Errorf("invalid value: status %d called %v, want 400 without a lookup", rec.Code, svc.called)
	}

	svc = &fakeService{}
	serve(t, svc, "GET", "/ranking", nil)
	if svc.ranking.HideLowConfidence {
		t.Error("the toggle must default to off")
	}
}

func TestScoreCarriesTheFlagWithoutTouchingTheScore(t *testing.T) {
	summary := gradedSummary()
	summary.Confidence = &domain.Confidence{Low: true, Reasons: []domain.ConfidenceReason{{Code: "LOW_HISTORY", Text: "мало истории"}}}

	body := decode(t, serve(t, &fakeService{summary: summary}, "GET", "/score/org/demo", nil))
	if body["low_confidence"] != true || body["score"] != float64(71) || body["status"] != "FINISHED" || body["level"] != "healthy" {
		t.Errorf("body = %v: the flag must not change score, status or level", body)
	}
	if reasons := body["confidence_reasons"].([]any); len(reasons) != 1 || reasons[0].(map[string]any)["code"] != "LOW_HISTORY" {
		t.Errorf("reasons = %v", body["confidence_reasons"])
	}

	body = decode(t, serve(t, &fakeService{summary: gradedSummary()}, "GET", "/score/org/demo", nil))
	if v, present := body["low_confidence"]; !present || v != nil {
		t.Errorf("unknown flag = %v, want an explicit null", body["low_confidence"])
	}
}

func TestAnalyticsReturnsTheStoredObject(t *testing.T) {
	gradedAt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	svc := &fakeService{analyticsOut: domain.RepoAnalytics{
		Analytics: []byte(`{"version":1,"bus_factor":{"status":"ok","value":2}}`),
		GradedAt:  &gradedAt,
	}}

	rec := serve(t, svc, "GET", "/analytics/org/demo", map[string]string{"PAT": "owner-pat"})

	if rec.Code != stdhttp.StatusOK || svc.analyticsFor != "org/demo" || svc.pat != "owner-pat" {
		t.Fatalf("status = %d for = %q pat = %q", rec.Code, svc.analyticsFor, svc.pat)
	}
	body := decode(t, rec)
	analytics := body["analytics"].(map[string]any)
	if body["status"] != "FINISHED" || analytics["version"] != float64(1) || body["graded_at"] != "2026-10-01T12:00:00Z" {
		t.Errorf("body = %v", body)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("the analytics depend on the caller's PAT and must not be readable cross-origin")
	}
}

func TestAnalyticsWithoutDataIsNoDataNotAnError(t *testing.T) {
	for name, stored := range map[string][]byte{"none": nil, "json null": []byte("null"), "broken": []byte("{not json")} {
		t.Run(name, func(t *testing.T) {
			rec := serve(t, &fakeService{analyticsOut: domain.RepoAnalytics{Analytics: stored}}, "GET", "/analytics/org/demo", nil)
			if rec.Code != stdhttp.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			body := decode(t, rec)
			if body["status"] != "NO_DATA" || body["analytics"] != nil || body["graded_at"] != nil {
				t.Errorf("body = %v, want NO_DATA with null analytics", body)
			}
		})
	}
}

func TestAnalyticsErrorsAreMapped(t *testing.T) {
	cases := map[string]struct {
		err  error
		want int
	}{
		"no access":    {fmt.Errorf("rpc: %w", domain.ErrForbidden), stdhttp.StatusForbidden},
		"unknown repo": {fmt.Errorf("rpc: %w", domain.ErrNotFound), stdhttp.StatusNotFound},
		"rate limited": {domain.ErrLimitExceeded, stdhttp.StatusTooManyRequests},
		"unavailable":  {fmt.Errorf("rpc: %w", domain.ErrUnavailable), stdhttp.StatusServiceUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if rec := serve(t, &fakeService{err: tc.err}, "GET", "/analytics/org/demo", nil); rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

package http

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gateway-service/internal/domain"
)

type statsFake struct {
	GatewayService

	stats domain.PlatformStats
	pages []domain.ExportPage
	// failAfter makes the export fail after that many emitted pages when >= 0.
	failAfter int
	err       error
}

func (f *statsFake) GetPlatformStats(ctx context.Context, clientIP string) (domain.PlatformStats, error) {
	return f.stats, f.err
}

func (f *statsFake) ExportPublicGrades(ctx context.Context, clientIP string, emit func(domain.ExportPage) error) error {
	if f.err != nil {
		return f.err
	}

	for i, page := range f.pages {
		if f.failAfter >= 0 && i == f.failAfter {
			return errors.New("rating down")
		}

		if err := emit(page); err != nil {
			return err
		}
	}

	return nil
}

func serveStats(t *testing.T, svc GatewayService, target string) *httptest.ResponseRecorder {
	t.Helper()

	mux := stdhttp.NewServeMux()
	NewHandler(svc).Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))

	return rec
}

func TestStatsResponseShape(t *testing.T) {
	week := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	rec := serveStats(t, &statsFake{failAfter: -1, stats: domain.PlatformStats{
		GradedRepos:  28,
		AvgScore:     51.2,
		MedianScore:  47,
		Distribution: []domain.ScoreBucket{{Min: 40, Max: 49, Repos: 12}},
		Languages:    []domain.LanguageStat{{Name: "Go", Color: "#00ADD8", Repos: 9, AvgScore: 50.6}},
		TopAdvice:    []domain.AdviceStat{{CategoryID: "activity", MetricID: "commits_per_week", Priority: "low", Repos: 21}},
		Trend:        []domain.TrendPoint{{WeekStart: &week, AvgScore: 51.5, Repos: 26}},
		MostImproved: []domain.ImprovedRepo{{OrgSlug: "acme", Name: "tool", Score: 80, PastScore: 60, Change: 20}},
		ImprovedDays: 7,
		MinLanguage:  3,
	}}, "/stats")

	if rec.Code != 200 || rec.Header().Get("Access-Control-Allow-Origin") != "*" || !strings.Contains(rec.Header().Get("Cache-Control"), "max-age") {
		t.Fatalf("status %d headers %v", rec.Code, rec.Header())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("not JSON: %v", err)
	}

	for _, key := range []string{"graded_repos", "avg_score", "median_score", "distribution", "levels", "languages",
		"categories", "top_advice", "trend", "most_improved", "improved_days", "min_language_repos", "generated_at"} {
		if _, ok := body[key]; !ok {
			t.Errorf("missing key %q", key)
		}
	}

	if levels, ok := body["levels"].([]any); !ok || len(levels) != 0 {
		t.Errorf("levels = %v, want [] and not null", body["levels"])
	}

	improved := body["most_improved"].([]any)[0].(map[string]any)
	if improved["org"] != "acme" || improved["change"] != float64(20) || improved["past_score"] != float64(60) {
		t.Errorf("improved = %v", improved)
	}
}

func TestStatsLimitExceeded(t *testing.T) {
	rec := serveStats(t, &statsFake{failAfter: -1, err: domain.ErrLimitExceeded}, "/stats")
	if rec.Code != stdhttp.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rec.Code)
	}
}

func score(v int32) *int32 { return &v }

func exportFixture() []domain.ExportPage {
	likes := 4.5
	low := false
	graded := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

	return []domain.ExportPage{
		{Total: 3, Grades: []domain.ExportedGrade{
			{
				OrgSlug: "acme", Repo: "tool", WebURL: "https://sourcecraft.dev/acme/tool", Score: 82, Language: "Go",
				Likes: &likes, GradedAt: &graded, LowConfidence: &low,
				Categories: []domain.PublicCategory{{ID: "activity", Score: score(70)}, {ID: "issues"}, {ID: "documentation", Score: score(0)}},
			},
			{OrgSlug: "acme", Repo: "=cmd|calc", Score: 40, Language: "+evil"},
		}},
		{Total: 3, Grades: []domain.ExportedGrade{{OrgSlug: "zed", Repo: "last", Score: 95}}},
	}
}

func TestExportCSV(t *testing.T) {
	rec := serveStats(t, &statsFake{failAfter: -1, pages: exportFixture()}, "/export/grades.csv")

	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("content type = %q", ct)
	}

	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.HasSuffix(cd, `.csv"`) {
		t.Errorf("content disposition = %q", cd)
	}

	rows, err := csv.NewReader(rec.Body).ReadAll()
	if err != nil {
		t.Fatalf("not valid CSV: %v", err)
	}

	if len(rows) != 4 {
		t.Fatalf("rows = %d, want header + 3", len(rows))
	}

	header := strings.Join(rows[0], ",")
	if header != "org,repo,url,score,level,language,likes,last_activity_at,graded_at,low_confidence,activity,issues,documentation,code_health" {
		t.Errorf("header = %s", header)
	}

	first := rows[1]
	if first[0] != "acme" || first[3] != "82" || first[4] != "healthy" || first[6] != "4.5" || first[9] != "false" {
		t.Errorf("first = %v", first)
	}

	// activity 70, issues no data (blank, not 0), documentation a real 0, code_health absent (blank).
	if first[10] != "70" || first[11] != "" || first[12] != "0" || first[13] != "" {
		t.Errorf("category cells = %v (no data must be blank, a real zero must stay 0)", first[10:])
	}

	if rows[2][1] != "'=cmd|calc" || rows[2][5] != "'+evil" {
		t.Errorf("formula cells = %q / %q, want a leading apostrophe", rows[2][1], rows[2][5])
	}

	if rows[3][0] != "zed" {
		t.Errorf("second page row = %v", rows[3])
	}
}

func TestExportJSON(t *testing.T) {
	rec := serveStats(t, &statsFake{failAfter: -1, pages: exportFixture()}, "/export/grades.json")

	var body struct {
		GeneratedAt string `json:"generated_at"`
		Total       int    `json:"total"`
		Grades      []struct {
			Org        string `json:"org"`
			Repo       string `json:"repo"`
			Level      string `json:"level"`
			Categories []struct {
				ID     string `json:"id"`
				Score  *int   `json:"score"`
				NoData bool   `json:"no_data"`
			} `json:"categories"`
		} `json:"grades"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, rec.Body.String())
	}

	if body.Total != 3 || len(body.Grades) != 3 || body.GeneratedAt == "" {
		t.Fatalf("body = %+v", body)
	}

	if body.Grades[0].Level != "healthy" || body.Grades[2].Org != "zed" || body.Grades[2].Level != "excellent" {
		t.Errorf("grades = %+v", body.Grades)
	}

	cats := body.Grades[0].Categories
	if cats[1].ID != "issues" || cats[1].Score != nil || !cats[1].NoData || cats[2].Score == nil || *cats[2].Score != 0 {
		t.Errorf("categories = %+v", cats)
	}
}

func TestExportEmptyIsStillValid(t *testing.T) {
	rec := serveStats(t, &statsFake{failAfter: -1, pages: []domain.ExportPage{{Total: 0}}}, "/export/grades.json")

	var body struct {
		Grades []any `json:"grades"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Grades == nil || len(body.Grades) != 0 {
		t.Fatalf("empty export = %q, err %v", rec.Body.String(), err)
	}
}

func TestExportLimitedBeforeAnyBytesIs429(t *testing.T) {
	rec := serveStats(t, &statsFake{failAfter: -1, err: domain.ErrLimitExceeded}, "/export/grades.csv")
	if rec.Code != stdhttp.StatusTooManyRequests || strings.Contains(rec.Header().Get("Content-Type"), "csv") {
		t.Errorf("status %d, content type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestExportFailureMidStreamAbortsInsteadOfLookingComplete(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != stdhttp.ErrAbortHandler {
			t.Fatalf("recovered %v, want http.ErrAbortHandler", recovered)
		}
	}()

	serveStats(t, &statsFake{failAfter: 1, pages: exportFixture()}, "/export/grades.json")
	t.Fatal("a mid-stream failure must abort the response")
}

package http

import (
	"context"
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gateway-service/internal/domain"

	"github.com/google/uuid"
)

type fakeService struct {
	err error

	pat      string
	grading  domain.GetGradingDTO
	ranking  domain.GetRankingDTO
	oauth    string
	called   bool
	gradeOut domain.GetGradingResponse
}

func (f *fakeService) GetMyRepos(ctx context.Context, clientIP, pat string) (domain.GetMyReposDTO, error) {
	f.called, f.pat = true, pat
	return domain.GetMyReposDTO{}, f.err
}

func (f *fakeService) GetGrading(ctx context.Context, clientIP, pat string, request domain.GetGradingDTO) (domain.GetGradingResponse, error) {
	f.called, f.pat, f.grading = true, pat, request
	return f.gradeOut, f.err
}

func (f *fakeService) RequestLLMRecommendations(ctx context.Context, clientIP, pat string, request domain.RequestLLMRecommendationsDTO) (domain.RequestLLMRecommendationsResponse, error) {
	f.called, f.pat = true, pat
	return domain.RequestLLMRecommendationsResponse{RawResponse: []byte(`{"summary":"ok"}`)}, f.err
}

func (f *fakeService) GetRanking(ctx context.Context, clientIP string, request domain.GetRankingDTO) (domain.GetRankingResponse, error) {
	f.called, f.ranking = true, request
	return domain.GetRankingResponse{}, f.err
}

func (f *fakeService) LoginWithYandex(ctx context.Context, clientIP string, request domain.YandexLoginDTO) (domain.LoginResponse, error) {
	f.called, f.oauth = true, request.OAuthToken
	return domain.LoginResponse{SessionID: "sid"}, f.err
}

func serve(t *testing.T, svc *fakeService, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	mux := stdhttp.NewServeMux()
	NewHandler(svc).Register(mux)

	req := httptest.NewRequest(method, target, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %q", rec.Body.String())
	}
	return body
}

func TestGradingRouteForwardsSlugsAndPAT(t *testing.T) {
	score := int32(74)
	svc := &fakeService{gradeOut: domain.GetGradingResponse{Grade: domain.RepoGrade{
		SourcecraftUUID: uuid.New(),
		Score:           &score,
		CategoryScores:  []byte(`{"activity":{"score":88}}`),
		Advice:          []byte(`[{"text":"t"}]`),
		GradedAt:        time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC),
	}}}

	rec := serve(t, svc, "GET", "/grading/my-org/my-repo", map[string]string{"PAT": "Bearer secret"})

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if svc.grading.OrgSlug != "my-org" || svc.grading.RepoSlug != "my-repo" || svc.pat != "secret" {
		t.Errorf("service got %+v pat=%q", svc.grading, svc.pat)
	}

	body := decode(t, rec)
	if body["status"] != "FINISHED" {
		t.Errorf("status field = %v, want FINISHED", body["status"])
	}
	// Stored JSON is passed through as JSON, not base64 text.
	grade := body["grade"].(map[string]any)
	if _, ok := grade["category_scores"].(map[string]any); !ok {
		t.Errorf("category_scores = %#v, want a JSON object", grade["category_scores"])
	}
	if _, ok := grade["advice"].([]any); !ok {
		t.Errorf("advice = %#v, want a JSON array", grade["advice"])
	}
}

func TestGradingWithoutPATPassesEmptyToken(t *testing.T) {
	svc := &fakeService{}
	serve(t, svc, "GET", "/grading/o/r", nil)

	if !svc.called || svc.pat != "" {
		t.Errorf("called=%v pat=%q, want a public request", svc.called, svc.pat)
	}
}

func TestPendingIsAccepted(t *testing.T) {
	for _, target := range []string{"/grading/o/r", "/recommendations/o/r"} {
		t.Run(target, func(t *testing.T) {
			svc := &fakeService{err: fmt.Errorf("get grading rpc: %w", domain.ErrPending)}
			rec := serve(t, svc, "GET", target, nil)

			if rec.Code != stdhttp.StatusAccepted {
				t.Fatalf("status = %d, want 202", rec.Code)
			}
			if body := decode(t, rec); body["status"] != "PENDING" || len(body) != 1 {
				t.Errorf("body = %v, want {status: PENDING}", body)
			}
		})
	}
}

func TestRecommendationsFinishedResponse(t *testing.T) {
	rec := serve(t, &fakeService{}, "GET", "/recommendations/o/r", nil)

	body := decode(t, rec)
	if rec.Code != stdhttp.StatusOK || body["status"] != "FINISHED" {
		t.Fatalf("status = %d, body = %v", rec.Code, body)
	}
	if raw, ok := body["raw_response"].(map[string]any); !ok || raw["summary"] != "ok" {
		t.Errorf("raw_response = %#v, want the stored JSON object", body["raw_response"])
	}
}

func TestReposRequiresPAT(t *testing.T) {
	svc := &fakeService{}
	rec := serve(t, svc, "GET", "/repos", nil)

	if rec.Code != stdhttp.StatusUnauthorized || svc.called {
		t.Errorf("status = %d called = %v, want 401 without calling the service", rec.Code, svc.called)
	}

	svc = &fakeService{}
	rec = serve(t, svc, "GET", "/repos", map[string]string{"PAT": "tok"})
	if rec.Code != stdhttp.StatusOK || svc.pat != "tok" {
		t.Errorf("status = %d pat = %q", rec.Code, svc.pat)
	}
}

func TestRankingQueryParameters(t *testing.T) {
	svc := &fakeService{}
	rec := serve(t, svc, "GET", "/ranking?sortby=likes&filter=%20Go%20&page=2&page_size=15", nil)

	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	want := domain.GetRankingDTO{Page: 2, PageSize: 15, Language: "Go", SortBy: domain.RankingSortByLikes}
	if svc.ranking != want {
		t.Errorf("dto = %+v, want %+v", svc.ranking, want)
	}

	svc = &fakeService{}
	serve(t, svc, "GET", "/ranking", nil)
	if svc.ranking != (domain.GetRankingDTO{SortBy: domain.RankingSortByScore}) {
		t.Errorf("defaults = %+v, want score sort and zero paging", svc.ranking)
	}
}

func TestRankingInvalidQueryIsBadRequest(t *testing.T) {
	for _, target := range []string{"/ranking?sortby=stars", "/ranking?page=-1", "/ranking?page_size=abc"} {
		t.Run(target, func(t *testing.T) {
			svc := &fakeService{}
			rec := serve(t, svc, "GET", target, nil)
			if rec.Code != stdhttp.StatusBadRequest || svc.called {
				t.Errorf("status = %d called = %v, want 400 without calling the service", rec.Code, svc.called)
			}
		})
	}
}

func TestLoginAcceptsBothHeaderForms(t *testing.T) {
	for name, headers := range map[string]map[string]string{
		"OAuth header":         {"OAuth": "ya-token"},
		"Authorization header": {"Authorization": "OAuth ya-token"},
	} {
		t.Run(name, func(t *testing.T) {
			svc := &fakeService{}
			rec := serve(t, svc, "POST", "/auth/yandex", headers)

			if rec.Code != stdhttp.StatusOK || svc.oauth != "ya-token" {
				t.Errorf("status = %d token = %q", rec.Code, svc.oauth)
			}
			if !strings.Contains(rec.Header().Get("Set-Cookie"), "session_id=sid") {
				t.Errorf("session cookie missing: %q", rec.Header().Get("Set-Cookie"))
			}
		})
	}

	rec := serve(t, &fakeService{}, "POST", "/auth/yandex", nil)
	if rec.Code != stdhttp.StatusUnauthorized {
		t.Errorf("status without token = %d, want 401", rec.Code)
	}
}

func TestUpstreamErrorStatuses(t *testing.T) {
	cases := map[error]int{
		domain.ErrInvalidRequest:  stdhttp.StatusBadRequest,
		domain.ErrUnauthenticated: stdhttp.StatusUnauthorized,
		domain.ErrForbidden:       stdhttp.StatusForbidden,
		domain.ErrNotFound:        stdhttp.StatusNotFound,
		domain.ErrConflict:        stdhttp.StatusConflict,
		domain.ErrLimitExceeded:   stdhttp.StatusTooManyRequests,
		domain.ErrUnavailable:     stdhttp.StatusServiceUnavailable,
		domain.ErrTimeout:         stdhttp.StatusGatewayTimeout,
		domain.ErrRequestFailed:   stdhttp.StatusBadGateway,
	}
	for err, want := range cases {
		t.Run(err.Error(), func(t *testing.T) {
			rec := serve(t, &fakeService{err: fmt.Errorf("get grading rpc: %w", err)}, "GET", "/grading/o/r", nil)
			if rec.Code != want {
				t.Errorf("status = %d, want %d", rec.Code, want)
			}
		})
	}
}

func TestOldPostRoutesAreGone(t *testing.T) {
	for _, target := range []string{"/grading", "/ranking", "/repos"} {
		rec := serve(t, &fakeService{}, "POST", target, nil)
		if rec.Code == stdhttp.StatusOK {
			t.Errorf("POST %s still answers 200", target)
		}
	}
}

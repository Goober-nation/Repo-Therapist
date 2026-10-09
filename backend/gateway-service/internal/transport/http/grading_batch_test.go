package http

import (
	"context"
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gateway-service/internal/domain"
)

type batchFake struct {
	adminFake

	getErr error

	startActor  string
	startFilter domain.BatchFilter
	controlID   int64
	controlAct  string
	startErr    error
}

func (b *batchFake) StartGradingBatch(_ context.Context, actor string, f domain.BatchFilter) (domain.GradingBatch, error) {
	b.startActor, b.startFilter = actor, f
	return domain.GradingBatch{ID: 5, Status: "running", Total: 2, Pending: 2}, b.startErr
}

func (b *batchFake) ControlGradingBatch(_ context.Context, _ string, id int64, action string) (domain.GradingBatch, error) {
	b.controlID, b.controlAct = id, action
	return domain.GradingBatch{ID: id, Status: "paused"}, nil
}

func (b *batchFake) PreviewGradingBatch(context.Context, domain.BatchFilter) (domain.BatchPreview, error) {
	return domain.BatchPreview{Count: 3}, nil
}

func (b *batchFake) GetGradingBatch(_ context.Context, id int64) (domain.GradingBatchDetail, error) {
	b.controlID = id
	if b.getErr != nil {
		return domain.GradingBatchDetail{}, b.getErr
	}

	return domain.GradingBatchDetail{Batch: domain.GradingBatch{ID: 5}, Failures: []domain.GradingBatchFailure{{OrgSlug: "o", RepoSlug: "r", Error: "timeout"}}}, nil
}

func (b *batchFake) ListGradingBatches(context.Context, int32) (domain.GradingBatchList, error) {
	return domain.GradingBatchList{MaxLimit: 250}, nil
}

func postAdmin(svc GatewayService, target, body string, header bool) *httptest.ResponseRecorder {
	mux := stdhttp.NewServeMux()
	NewHandler(svc).Register(mux)

	req := httptest.NewRequest(stdhttp.MethodPost, target, strings.NewReader(body))
	if header {
		req.Header.Set(adminRequestHeader, adminRequestValue)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	return rec
}

func TestRegradeRoutesNeedTheCSRFHeaderAndAdmin(t *testing.T) {
	f := &batchFake{}

	if rec := postAdmin(f, "/admin/grading/batches", `{"limit":10}`, false); rec.Code != stdhttp.StatusForbidden {
		t.Fatalf("no header: %d", rec.Code)
	}

	f.authErr = domain.ErrForbidden
	if rec := postAdmin(f, "/admin/grading/batches", `{"limit":10}`, true); rec.Code != stdhttp.StatusForbidden || f.startActor != "" {
		t.Fatalf("not admin: %d, started by %q", rec.Code, f.startActor)
	}
}

func TestStartGradingBatchReadsFilterAndUsesTheAdminHash(t *testing.T) {
	f := &batchFake{}

	rec := postAdmin(f, "/admin/grading/batches", `{"languages":["go"],"min_score":10,"repos":["o/r"],"limit":300,"only_regrade":true}`, true)
	if rec.Code != stdhttp.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	if f.startActor != "abcd" || f.startFilter.MinScore == nil || *f.startFilter.MinScore != 10 || f.startFilter.MaxScore != nil ||
		f.startFilter.Limit != 300 || !f.startFilter.OnlyRegrade {
		t.Fatalf("actor %q filter %+v", f.startActor, f.startFilter)
	}

	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)

	if body["id"] != float64(5) || body["total"] != float64(2) {
		t.Fatalf("body = %v", body)
	}

	if _, leaks := body["actor_hash"]; leaks {
		t.Fatal("unexpected field")
	}
}

func TestStartGradingBatchRejectsBadBodies(t *testing.T) {
	f := &batchFake{}

	for _, body := range []string{``, `not json`, `{"limit":10,"bogus":1}`, `{"languages":"go"}`} {
		if rec := postAdmin(f, "/admin/grading/batches", body, true); rec.Code != stdhttp.StatusBadRequest {
			t.Errorf("%q: status %d", body, rec.Code)
		}
	}

	if f.startActor != "" {
		t.Fatal("a bad body must not start a batch")
	}
}

func TestRegradeErrorsCarryTheReasonCode(t *testing.T) {
	f := &batchFake{startErr: domain.ReasonError{Reason: "batch_active", Err: fmt.Errorf("x: %w", domain.ErrConflict)}}

	rec := postAdmin(f, "/admin/grading/batches", `{"limit":10}`, true)
	if rec.Code != stdhttp.StatusConflict {
		t.Fatalf("status %d", rec.Code)
	}

	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)

	if body["code"] != "batch_active" || body["error"] != "conflict" {
		t.Fatalf("body = %v", body)
	}
}

func TestRegradeControlAndStatusRoutes(t *testing.T) {
	f := &batchFake{}

	if rec := postAdmin(f, "/admin/grading/batches/7/pause", ``, true); rec.Code != stdhttp.StatusOK || f.controlID != 7 || f.controlAct != "pause" {
		t.Fatalf("pause: %d id=%d act=%q", rec.Code, f.controlID, f.controlAct)
	}

	for _, target := range []string{"/admin/grading/batches/0/pause", "/admin/grading/batches/abc/pause", "/admin/grading/batches/current/pause"} {
		if rec := postAdmin(f, target, ``, true); rec.Code != stdhttp.StatusBadRequest {
			t.Errorf("%s: %d", target, rec.Code)
		}
	}

	mux := stdhttp.NewServeMux()
	NewHandler(f).Register(mux)

	req := httptest.NewRequest(stdhttp.MethodGet, "/admin/grading/batches/current", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusOK || f.controlID != 0 || !strings.Contains(rec.Body.String(), `"repo":"o/r"`) || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("current: %d id=%d %s", rec.Code, f.controlID, rec.Body)
	}

	if rec := postAdmin(f, "/admin/grading/preview", `{"limit":10}`, true); rec.Code != stdhttp.StatusOK || !strings.Contains(rec.Body.String(), `"sample":[]`) ||
		!strings.Contains(rec.Body.String(), `"matched":0`) {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body)
	}
}

func TestCurrentBatchBeforeTheFirstOneIsAnEmptyAnswerNotAnError(t *testing.T) {
	f := &batchFake{getErr: domain.ReasonError{Reason: "batch_not_found", Err: fmt.Errorf("x: %w", domain.ErrNotFound)}}

	mux := stdhttp.NewServeMux()
	NewHandler(f).Register(mux)

	get := func(target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, target, nil))

		return rec
	}

	if rec := get("/admin/grading/batches/current"); rec.Code != stdhttp.StatusOK || !strings.Contains(rec.Body.String(), `"batch":null`) {
		t.Fatalf("current: %d %s", rec.Code, rec.Body)
	}

	// A specific id that does not exist is still a 404 with its reason.
	if rec := get("/admin/grading/batches/7"); rec.Code != stdhttp.StatusNotFound || !strings.Contains(rec.Body.String(), "batch_not_found") {
		t.Fatalf("unknown id: %d %s", rec.Code, rec.Body)
	}
}

func TestGradingBatchListCarriesTheStandMaximum(t *testing.T) {
	mux := stdhttp.NewServeMux()
	NewHandler(&batchFake{}).Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, "/admin/grading/batches", nil))

	if rec.Code != stdhttp.StatusOK || !strings.Contains(rec.Body.String(), `"max_limit":250`) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
}

func TestLimitAboveTheMaximumCarriesItsReason(t *testing.T) {
	f := &batchFake{startErr: domain.ReasonError{Reason: "limit_too_high", Err: fmt.Errorf("x: %w", domain.ErrInvalidRequest)}}

	rec := postAdmin(f, "/admin/grading/batches", `{"limit":9999}`, true)
	if rec.Code != stdhttp.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":"limit_too_high"`) {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
}

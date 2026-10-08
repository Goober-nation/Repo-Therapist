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

func (b *batchFake) StartRegradeBatch(_ context.Context, actor string, f domain.BatchFilter) (domain.RegradeBatch, error) {
	b.startActor, b.startFilter = actor, f
	return domain.RegradeBatch{ID: 5, Status: "running", Total: 2, Pending: 2}, b.startErr
}

func (b *batchFake) ControlRegradeBatch(_ context.Context, _ string, id int64, action string) (domain.RegradeBatch, error) {
	b.controlID, b.controlAct = id, action
	return domain.RegradeBatch{ID: id, Status: "paused"}, nil
}

func (b *batchFake) PreviewRegradeBatch(context.Context, domain.BatchFilter) (domain.BatchPreview, error) {
	return domain.BatchPreview{Count: 3}, nil
}

func (b *batchFake) GetRegradeBatch(_ context.Context, id int64) (domain.RegradeBatchDetail, error) {
	b.controlID = id
	if b.getErr != nil {
		return domain.RegradeBatchDetail{}, b.getErr
	}

	return domain.RegradeBatchDetail{Batch: domain.RegradeBatch{ID: 5}, Failures: []domain.RegradeBatchFailure{{OrgSlug: "o", RepoSlug: "r", Error: "timeout"}}}, nil
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

	if rec := postAdmin(f, "/admin/regrade/batches", `{"all":true}`, false); rec.Code != stdhttp.StatusForbidden {
		t.Fatalf("no header: %d", rec.Code)
	}

	f.authErr = domain.ErrForbidden
	if rec := postAdmin(f, "/admin/regrade/batches", `{"all":true}`, true); rec.Code != stdhttp.StatusForbidden || f.startActor != "" {
		t.Fatalf("not admin: %d, started by %q", rec.Code, f.startActor)
	}
}

func TestStartRegradeBatchReadsFilterAndUsesTheAdminHash(t *testing.T) {
	f := &batchFake{}

	rec := postAdmin(f, "/admin/regrade/batches", `{"languages":["go"],"min_score":10,"repos":["o/r"]}`, true)
	if rec.Code != stdhttp.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	if f.startActor != "abcd" || f.startFilter.MinScore == nil || *f.startFilter.MinScore != 10 || f.startFilter.MaxScore != nil || f.startFilter.All {
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

func TestStartRegradeBatchRejectsBadBodies(t *testing.T) {
	f := &batchFake{}

	for _, body := range []string{``, `not json`, `{"all":true,"bogus":1}`, `{"languages":"go"}`} {
		if rec := postAdmin(f, "/admin/regrade/batches", body, true); rec.Code != stdhttp.StatusBadRequest {
			t.Errorf("%q: status %d", body, rec.Code)
		}
	}

	if f.startActor != "" {
		t.Fatal("a bad body must not start a batch")
	}
}

func TestRegradeErrorsCarryTheReasonCode(t *testing.T) {
	f := &batchFake{startErr: domain.ReasonError{Reason: "batch_active", Err: fmt.Errorf("x: %w", domain.ErrConflict)}}

	rec := postAdmin(f, "/admin/regrade/batches", `{"all":true}`, true)
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

	if rec := postAdmin(f, "/admin/regrade/batches/7/pause", ``, true); rec.Code != stdhttp.StatusOK || f.controlID != 7 || f.controlAct != "pause" {
		t.Fatalf("pause: %d id=%d act=%q", rec.Code, f.controlID, f.controlAct)
	}

	for _, target := range []string{"/admin/regrade/batches/0/pause", "/admin/regrade/batches/abc/pause", "/admin/regrade/batches/current/pause"} {
		if rec := postAdmin(f, target, ``, true); rec.Code != stdhttp.StatusBadRequest {
			t.Errorf("%s: %d", target, rec.Code)
		}
	}

	mux := stdhttp.NewServeMux()
	NewHandler(f).Register(mux)

	req := httptest.NewRequest(stdhttp.MethodGet, "/admin/regrade/batches/current", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != stdhttp.StatusOK || f.controlID != 0 || !strings.Contains(rec.Body.String(), `"repo":"o/r"`) || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("current: %d id=%d %s", rec.Code, f.controlID, rec.Body)
	}

	if rec := postAdmin(f, "/admin/regrade/preview", `{"all":true}`, true); rec.Code != stdhttp.StatusOK || !strings.Contains(rec.Body.String(), `"sample":[]`) {
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

	if rec := get("/admin/regrade/batches/current"); rec.Code != stdhttp.StatusOK || !strings.Contains(rec.Body.String(), `"batch":null`) {
		t.Fatalf("current: %d %s", rec.Code, rec.Body)
	}

	// A specific id that does not exist is still a 404 with its reason.
	if rec := get("/admin/regrade/batches/7"); rec.Code != stdhttp.StatusNotFound || !strings.Contains(rec.Body.String(), "batch_not_found") {
		t.Fatalf("unknown id: %d %s", rec.Code, rec.Body)
	}
}

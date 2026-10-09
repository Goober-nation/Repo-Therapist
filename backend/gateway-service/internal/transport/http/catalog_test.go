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
)

type catalogFake struct {
	adminFake

	refreshActor string
	cancelActor  string
	refreshErr   error
}

func (c *catalogFake) GetCatalogStatus(context.Context) (domain.CatalogStatus, error) {
	updated := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

	return domain.CatalogStatus{
		Size: 28496, UpdatedAt: &updated,
		Current: &domain.CatalogRefresh{ID: 2, Status: "running", Trigger: "admin", Pages: 10, Repos: 1000},
	}, nil
}

func (c *catalogFake) RefreshCatalog(_ context.Context, actor string) (domain.CatalogRefresh, error) {
	c.refreshActor = actor
	return domain.CatalogRefresh{ID: 3, Status: "running", Trigger: "admin", ActorHash: actor}, c.refreshErr
}

func (c *catalogFake) CancelCatalogRefresh(_ context.Context, actor string) (domain.CatalogRefresh, error) {
	c.cancelActor = actor
	return domain.CatalogRefresh{ID: 3, Status: "cancelled"}, nil
}

func TestCatalogRoutesNeedTheCSRFHeaderAndAdmin(t *testing.T) {
	f := &catalogFake{}

	if rec := postAdmin(f, "/admin/catalog/refresh", ``, false); rec.Code != stdhttp.StatusForbidden || f.refreshActor != "" {
		t.Fatalf("no header: %d", rec.Code)
	}

	f.authErr = domain.ErrForbidden
	for _, target := range []string{"/admin/catalog/refresh", "/admin/catalog/refresh/cancel"} {
		if rec := postAdmin(f, target, ``, true); rec.Code != stdhttp.StatusForbidden {
			t.Errorf("%s not admin: %d", target, rec.Code)
		}
	}

	mux := stdhttp.NewServeMux()
	NewHandler(f).Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, "/admin/catalog", nil))

	if rec.Code != stdhttp.StatusForbidden {
		t.Fatalf("status not admin: %d", rec.Code)
	}
	if f.refreshActor != "" || f.cancelActor != "" {
		t.Fatal("a refused request must not reach the service")
	}
}

func TestCatalogRoutes(t *testing.T) {
	f := &catalogFake{}

	rec := postAdmin(f, "/admin/catalog/refresh", ``, true)
	if rec.Code != stdhttp.StatusAccepted || f.refreshActor == "" || !strings.Contains(rec.Body.String(), `"status":"running"`) {
		t.Fatalf("refresh: %d actor=%q %s", rec.Code, f.refreshActor, rec.Body)
	}

	if rec := postAdmin(f, "/admin/catalog/refresh/cancel", ``, true); rec.Code != stdhttp.StatusOK || f.cancelActor != f.refreshActor {
		t.Fatalf("cancel: %d actor=%q", rec.Code, f.cancelActor)
	}

	mux := stdhttp.NewServeMux()
	NewHandler(f).Register(mux)

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, "/admin/catalog", nil))

	var body struct {
		Size          int                     `json:"size"`
		UpdatedAt     string                  `json:"updated_at"`
		Current       *catalogRefreshResponse `json:"current"`
		LastCompleted *catalogRefreshResponse `json:"last_completed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	if rec.Code != stdhttp.StatusOK || body.Size != 28496 || body.Current == nil || body.Current.Repos != 1000 || body.LastCompleted != nil {
		t.Fatalf("status: %d %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", rec.Header().Get("Cache-Control"))
	}
}

func TestCatalogRefreshWhileRunningCarriesTheReason(t *testing.T) {
	f := &catalogFake{refreshErr: domain.ReasonError{Reason: "refresh_active", Err: fmt.Errorf("x: %w", domain.ErrConflict)}}

	rec := postAdmin(f, "/admin/catalog/refresh", ``, true)

	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)

	if rec.Code != stdhttp.StatusConflict || body["code"] != "refresh_active" {
		t.Fatalf("status %d body %v", rec.Code, body)
	}
}

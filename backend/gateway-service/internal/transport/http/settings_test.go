package http

import (
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gateway-service/internal/domain"
)

func doSettings(svc GatewayService, method, target, body string, header bool) *httptest.ResponseRecorder {
	mux := stdhttp.NewServeMux()
	NewHandler(svc).Register(mux)

	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if header {
		req.Header.Set(adminRequestHeader, adminRequestValue)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	return rec
}

func TestSettingsRoutes(t *testing.T) {
	f := &fakeService{}

	if rec := doSettings(f, stdhttp.MethodPut, "/admin/settings/gateway.rate_limit", `{"value":"90"}`, false); rec.Code != stdhttp.StatusForbidden {
		t.Fatalf("PUT without the CSRF header: %d", rec.Code)
	}
	if rec := doSettings(f, stdhttp.MethodDelete, "/admin/settings/gateway.rate_limit", ``, false); rec.Code != stdhttp.StatusForbidden {
		t.Fatalf("DELETE without the CSRF header: %d", rec.Code)
	}

	rec := doSettings(f, stdhttp.MethodPut, "/admin/settings/gateway.rate_limit", `{"value":"90"}`, true)
	if rec.Code != stdhttp.StatusOK || f.settingSet[1] != "gateway.rate_limit" || f.settingSet[2] != "90" {
		t.Fatalf("PUT: %d %v", rec.Code, f.settingSet)
	}

	for _, bad := range []string{``, `{}`, `{"value":1}`, `{"value":"1","extra":true}`} {
		if rec := doSettings(f, stdhttp.MethodPut, "/admin/settings/gateway.rate_limit", bad, true); rec.Code != stdhttp.StatusBadRequest {
			t.Errorf("PUT %q: %d", bad, rec.Code)
		}
	}

	rec = doSettings(f, stdhttp.MethodGet, "/admin/settings", ``, false)
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("GET: %d", rec.Code)
	}

	var page struct {
		Settings []settingResponse `json:"settings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || len(page.Settings) != 2 {
		t.Fatalf("GET body %s: %v", rec.Body, err)
	}
	if s := page.Settings[0]; s.EffectiveFrom != "file" || s.FileValue == nil || *s.FileValue != "60" || s.Override != nil {
		t.Errorf("file-backed setting %+v", s)
	}
	// An env lock wins over an override the table still holds.
	if s := page.Settings[1]; s.EffectiveFrom != "env" || s.Override == nil || len(s.EnvLockedBy) != 1 {
		t.Errorf("env-locked setting %+v", s)
	}
}

func TestSettingReasonsReachTheClient(t *testing.T) {
	f := &fakeService{err: domain.ReasonError{Reason: "setting_env_locked", Err: domain.ErrInvalidRequest}}

	rec := doSettings(f, stdhttp.MethodPut, "/admin/settings/gateway.rate_limit", `{"value":"90"}`, true)
	if !strings.Contains(rec.Body.String(), "setting_env_locked") {
		t.Errorf("reason not passed on: %d %s", rec.Code, rec.Body)
	}
}

func TestGetConfigIsPublic(t *testing.T) {
	f := &fakeService{publicConfig: domain.PublicConfig{PublicURL: "https://x", YandexClientID: "id", PollTimeoutMS: 1000}}

	rec := doSettings(f, stdhttp.MethodGet, "/config", ``, false)
	if rec.Code != stdhttp.StatusOK || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("GET /config: %d %v", rec.Code, rec.Header())
	}

	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["public_url"] != "https://x" || body["yandex_client_id"] != "id" || body["poll_timeout_ms"] != float64(1000) || body["dev_auth_bypass"] != false {
		t.Errorf("body %v", body)
	}
}

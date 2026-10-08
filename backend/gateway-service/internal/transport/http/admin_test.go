package http

import (
	"context"
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gateway-service/internal/domain"
)

type adminFake struct {
	GatewayService

	status     domain.AdminStatus
	statusErr  error
	unlockErr  error
	unlocked   []string
	lockErr    error
	locked     int
	authErr    error
	authCalls  []string
	cookieSeen string
	routeSeen  string
	page       domain.AuditPage
	filter     domain.AuditFilter
	listCalls  int
	listErr    error
}

func (a *adminFake) AdminStatus(_ context.Context, sessionID string) (domain.AdminStatus, error) {
	a.cookieSeen = sessionID
	return a.status, a.statusErr
}

func (a *adminFake) UnlockAdmin(_ context.Context, clientIP, sessionID, password string) error {
	a.cookieSeen = sessionID
	a.unlocked = append(a.unlocked, clientIP+"|"+password)

	return a.unlockErr
}

func (a *adminFake) LockAdmin(_ context.Context, sessionID string) error {
	a.cookieSeen = sessionID
	a.locked++

	return a.lockErr
}

func (a *adminFake) AuthenticateAdmin(_ context.Context, clientIP, sessionID, route string) (domain.AdminIdentity, error) {
	a.authCalls = append(a.authCalls, clientIP)
	a.cookieSeen, a.routeSeen = sessionID, route

	return domain.AdminIdentity{ActorHash: "abcd"}, a.authErr
}

func (a *adminFake) ListAudit(_ context.Context, filter domain.AuditFilter) (domain.AuditPage, error) {
	a.listCalls++
	a.filter = filter

	return a.page, a.listErr
}

func doAdmin(t *testing.T, svc GatewayService, method, target string, headers map[string]string, cookie string) *httptest.ResponseRecorder {
	t.Helper()

	mux := stdhttp.NewServeMux()
	handler := NewHandler(svc)
	handler.Register(mux)
	// A state-changing admin route does not exist yet; this stands in to test the guard itself.
	mux.HandleFunc("POST /admin/probe", handler.requireAdmin(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, _ domain.AdminIdentity) {
		w.WriteHeader(stdhttp.StatusNoContent)
	}))

	req := httptest.NewRequest(method, target, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	if cookie != "" {
		req.AddCookie(&stdhttp.Cookie{Name: sessionCookieName, Value: cookie})
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	return rec
}

func TestAdminMeAnswers200ForEveryone(t *testing.T) {
	for name, tc := range map[string]struct {
		fake *adminFake
		want bool
	}{
		"admin":   {&adminFake{status: domain.AdminStatus{IsAdmin: true}}, true},
		"visitor": {&adminFake{}, false},
	} {
		rec := doAdmin(t, tc.fake, "GET", "/admin/me", nil, "sess-1")

		var body adminMeResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != 200 || body.IsAdmin != tc.want {
			t.Errorf("%s: status %d body %s", name, rec.Code, rec.Body.String())
		}

		if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s: headers = %v (admin answers are never cached and never CORS-open)", name, rec.Header())
		}

		if tc.fake.cookieSeen != "sess-1" {
			t.Errorf("%s: the session cookie was not read, got %q", name, tc.fake.cookieSeen)
		}
	}

	rec := doAdmin(t, &adminFake{}, "GET", "/admin/me", nil, "")
	if rec.Code != 200 {
		t.Errorf("a visitor without a cookie got %d, want 200", rec.Code)
	}

	if rec := doAdmin(t, &adminFake{statusErr: errors.New("redis down")}, "GET", "/admin/me", nil, "x"); rec.Code != stdhttp.StatusInternalServerError {
		t.Errorf("an outage got %d, want 500", rec.Code)
	}
}

func TestGuardedRouteMapsSessionAndRoleErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"no or unknown session": {domain.ErrSessionNotFound, stdhttp.StatusUnauthorized},
		"not an admin":          {domain.ErrForbidden, stdhttp.StatusForbidden},
		"outage":                {errors.New("boom"), stdhttp.StatusInternalServerError},
	} {
		svc := &adminFake{authErr: tc.err}
		rec := doAdmin(t, svc, "GET", "/admin/audit", nil, "sess-1")

		if rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d", name, rec.Code, tc.want)
		}

		if svc.listCalls != 0 {
			t.Errorf("%s: the log was read although the guard refused", name)
		}

		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: a refusal must not be cached", name)
		}
	}
}

func TestGuardPassesRouteAndCookieButNotTheQuery(t *testing.T) {
	svc := &adminFake{}
	doAdmin(t, svc, "GET", "/admin/audit?action=admin.&limit=3", nil, "sess-42")

	if svc.routeSeen != "/admin/audit" || svc.cookieSeen != "sess-42" {
		t.Errorf("route %q cookie %q: the audit route must be the bare path", svc.routeSeen, svc.cookieSeen)
	}
}

func TestStateChangingAdminRoutesNeedTheCSRFHeader(t *testing.T) {
	svc := &adminFake{}

	rec := doAdmin(t, svc, "POST", "/admin/probe", nil, "sess-1")
	if rec.Code != stdhttp.StatusForbidden || len(svc.authCalls) != 0 {
		t.Fatalf("without the header: status %d, auth calls %d (must be refused before any session work)", rec.Code, len(svc.authCalls))
	}

	rec = doAdmin(t, svc, "POST", "/admin/probe", map[string]string{adminRequestHeader: "0"}, "sess-1")
	if rec.Code != stdhttp.StatusForbidden {
		t.Errorf("a wrong header value got %d, want 403", rec.Code)
	}

	rec = doAdmin(t, svc, "POST", "/admin/probe", map[string]string{adminRequestHeader: adminRequestValue}, "sess-1")
	if rec.Code != stdhttp.StatusNoContent || len(svc.authCalls) != 1 {
		t.Errorf("with the header: status %d, auth calls %d", rec.Code, len(svc.authCalls))
	}

	// Reads need no header: the same-origin UI and curl can both read.
	if rec := doAdmin(t, svc, "GET", "/admin/audit", nil, "sess-1"); rec.Code != 200 {
		t.Errorf("a GET without the header got %d, want 200", rec.Code)
	}
}

func TestAuditPageShapeAndParameters(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	svc := &adminFake{page: domain.AuditPage{
		NextBeforeID: 41,
		Entries: []domain.AuditEntry{
			{ID: 42, OccurredAt: at, AuditEvent: domain.AuditEvent{
				ActorKind: "user", ActorHash: "abcd", Action: "admin.denied", TargetKind: "route", Target: "/admin/audit",
				Outcome: "denied", Details: json.RawMessage(`{"reason":"not_admin"}`),
			}},
			{ID: 41, OccurredAt: at, AuditEvent: domain.AuditEvent{ActorKind: "system", Action: "audit.purge", Outcome: "ok"}},
		},
	}}

	rec := doAdmin(t, svc, "GET", "/admin/audit?limit=25&before_id=100&action=admin.&outcome=denied", nil, "s")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	if svc.filter != (domain.AuditFilter{Limit: 25, BeforeID: 100, Action: "admin.", Outcome: "denied"}) {
		t.Errorf("filter = %+v", svc.filter)
	}

	var body struct {
		NextBeforeID int64 `json:"next_before_id"`
		Entries      []struct {
			ID      int64           `json:"id"`
			Actor   string          `json:"actor"`
			Action  string          `json:"action"`
			Details json.RawMessage `json:"details"`
		} `json:"entries"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("not JSON: %v", err)
	}

	if body.NextBeforeID != 41 || len(body.Entries) != 2 || body.Entries[0].Actor != "abcd" || string(body.Entries[0].Details) != `{"reason":"not_admin"}` {
		t.Errorf("body = %s", rec.Body.String())
	}

	if string(body.Entries[1].Details) != `{}` {
		t.Errorf("missing details must encode as {}, got %s", body.Entries[1].Details)
	}

	if strings.Contains(rec.Body.String(), "actor_hash") {
		t.Error("the response exposes the field under its internal name")
	}
}

func TestAuditParametersAreValidatedBeforeTheLogIsRead(t *testing.T) {
	for _, target := range []string{
		"/admin/audit?limit=abc",
		"/admin/audit?limit=-1",
		"/admin/audit?limit=99999999999",
		"/admin/audit?before_id=x",
		"/admin/audit?before_id=-3",
	} {
		svc := &adminFake{}

		if rec := doAdmin(t, svc, "GET", target, nil, "s"); rec.Code != stdhttp.StatusBadRequest || svc.listCalls != 0 {
			t.Errorf("%s: status %d, log reads %d, want 400 and none", target, rec.Code, svc.listCalls)
		}
	}
}

func TestAuditEmptyPageIsAnEmptyList(t *testing.T) {
	rec := doAdmin(t, &adminFake{}, "GET", "/admin/audit", nil, "s")

	if !strings.Contains(rec.Body.String(), `"entries":[]`) {
		t.Errorf("body = %s, want an empty list and not null", rec.Body.String())
	}
}

func post(t *testing.T, svc GatewayService, target, body string, withHeader bool, cookie string) *httptest.ResponseRecorder {
	t.Helper()

	mux := stdhttp.NewServeMux()
	NewHandler(svc).Register(mux)

	req := httptest.NewRequest("POST", target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	if withHeader {
		req.Header.Set(adminRequestHeader, adminRequestValue)
	}

	if cookie != "" {
		req.AddCookie(&stdhttp.Cookie{Name: sessionCookieName, Value: cookie})
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	return rec
}

func TestAdminMeCarriesTheWholeStatus(t *testing.T) {
	svc := &adminFake{status: domain.AdminStatus{SignedIn: true, IsAdmin: true, UnlockAvailable: true, ViaPassword: true}}
	rec := doAdmin(t, svc, "GET", "/admin/me", nil, "s")

	var body adminMeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	if !body.IsAdmin || !body.SignedIn || !body.UnlockAvailable || !body.ViaPassword {
		t.Errorf("body = %+v", body)
	}
}

func TestUnlockNeedsTheCSRFHeaderBeforeAnythingElse(t *testing.T) {
	svc := &adminFake{}

	rec := post(t, svc, "/admin/unlock", `{"password":"x"}`, false, "sess-1")
	if rec.Code != stdhttp.StatusForbidden || len(svc.unlocked) != 0 {
		t.Fatalf("status %d, unlock calls %d: a request without the header must not reach the password check", rec.Code, len(svc.unlocked))
	}

	rec = post(t, svc, "/admin/lock", ``, false, "sess-1")
	if rec.Code != stdhttp.StatusForbidden || svc.locked != 0 {
		t.Errorf("lock without the header: status %d, calls %d", rec.Code, svc.locked)
	}
}

func TestUnlockPassesCookieIPAndPasswordAndAnswersTheStatus(t *testing.T) {
	svc := &adminFake{status: domain.AdminStatus{SignedIn: true, IsAdmin: true, UnlockAvailable: true, ViaPassword: true}}

	rec := post(t, svc, "/admin/unlock", `{"password":"s3cret pass"}`, true, "sess-9")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	if len(svc.unlocked) != 1 || !strings.HasSuffix(svc.unlocked[0], "|s3cret pass") || svc.cookieSeen != "sess-9" {
		t.Errorf("unlock calls = %v, cookie %q", svc.unlocked, svc.cookieSeen)
	}

	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Error("the response must never echo the password")
	}

	var body adminMeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || !body.IsAdmin || !body.ViaPassword {
		t.Errorf("body = %s", rec.Body.String())
	}

	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("headers = %v", rec.Header())
	}
}

func TestUnlockErrorMapping(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"no session":     {domain.ErrSessionNotFound, stdhttp.StatusUnauthorized},
		"wrong password": {domain.ErrForbidden, stdhttp.StatusForbidden},
		"bad password":   {domain.ErrInvalidRequest, stdhttp.StatusBadRequest},
		"outage":         {errors.New("redis down"), stdhttp.StatusInternalServerError},
	} {
		svc := &adminFake{unlockErr: tc.err}

		rec := post(t, svc, "/admin/unlock", `{"password":"x"}`, true, "s")
		if rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d", name, rec.Code, tc.want)
		}

		if strings.Contains(rec.Body.String(), "redis") {
			t.Errorf("%s: an internal error text leaked: %s", name, rec.Body.String())
		}
	}
}

func TestUnlockLockedAnswers429WithRetryAfter(t *testing.T) {
	svc := &adminFake{unlockErr: domain.AdminLockedError{RetryAfter: 14*time.Minute + 30*time.Second}}

	rec := post(t, svc, "/admin/unlock", `{"password":"x"}`, true, "s")
	if rec.Code != stdhttp.StatusTooManyRequests || rec.Header().Get("Retry-After") != "870" {
		t.Errorf("status %d, Retry-After %q, want 429 and 870", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestUnlockRejectsBrokenOrOversizedBodiesBeforeTheService(t *testing.T) {
	for name, body := range map[string]string{
		"not json":  `password=x`,
		"empty":     ``,
		"too large": `{"password":"` + strings.Repeat("a", 5000) + `"}`,
	} {
		svc := &adminFake{}

		rec := post(t, svc, "/admin/unlock", body, true, "s")
		if rec.Code != stdhttp.StatusBadRequest || len(svc.unlocked) != 0 {
			t.Errorf("%s: status %d, unlock calls %d, want 400 and none", name, rec.Code, len(svc.unlocked))
		}
	}
}

func TestPasswordInTheQueryStringIsIgnored(t *testing.T) {
	svc := &adminFake{}

	rec := post(t, svc, "/admin/unlock?password=leaked", `{"password":""}`, true, "s")
	if rec.Code != 200 || len(svc.unlocked) != 1 || strings.HasSuffix(svc.unlocked[0], "|leaked") {
		t.Errorf("status %d, calls %v: the password must come from the body only", rec.Code, svc.unlocked)
	}
}

func TestLockAnswersTheStatusAndMapsErrors(t *testing.T) {
	svc := &adminFake{status: domain.AdminStatus{SignedIn: true, UnlockAvailable: true}}

	rec := post(t, svc, "/admin/lock", ``, true, "sess-3")
	if rec.Code != 200 || svc.locked != 1 || svc.cookieSeen != "sess-3" {
		t.Fatalf("status %d, lock calls %d", rec.Code, svc.locked)
	}

	var body adminMeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.IsAdmin {
		t.Errorf("body = %s", rec.Body.String())
	}

	if rec := post(t, &adminFake{lockErr: domain.ErrSessionNotFound}, "/admin/lock", ``, true, ""); rec.Code != stdhttp.StatusUnauthorized {
		t.Errorf("lock without a session: status %d, want 401", rec.Code)
	}
}

type logoutFake struct {
	GatewayService

	err      error
	sessions []string
	ips      []string
}

func (l *logoutFake) Logout(_ context.Context, clientIP, sessionID string) error {
	l.ips = append(l.ips, clientIP)
	l.sessions = append(l.sessions, sessionID)

	return l.err
}

func TestLogoutNeedsTheHeaderAndClearsTheCookie(t *testing.T) {
	svc := &logoutFake{}

	rec := post(t, svc, "/auth/logout", ``, false, "sess-1")
	if rec.Code != stdhttp.StatusForbidden || len(svc.sessions) != 0 {
		t.Fatalf("without the header: status %d, calls %d (another site must not be able to sign a user out)", rec.Code, len(svc.sessions))
	}

	rec = post(t, svc, "/auth/logout", ``, true, "sess-1")
	if rec.Code != 200 || len(svc.sessions) != 1 || svc.sessions[0] != "sess-1" {
		t.Fatalf("status %d, sessions %v", rec.Code, svc.sessions)
	}

	var cleared *stdhttp.Cookie

	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			cleared = cookie
		}
	}

	if cleared == nil || cleared.Value != "" || cleared.MaxAge >= 0 || !cleared.HttpOnly || !cleared.Secure || cleared.Path != "/" {
		t.Errorf("cookie = %+v, want an expired, empty, HttpOnly, Secure cookie on /", cleared)
	}

	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("headers = %v", rec.Header())
	}
}

func TestLogoutWithoutACookieStillAnswers200(t *testing.T) {
	svc := &logoutFake{}

	rec := post(t, svc, "/auth/logout", ``, true, "")
	if rec.Code != 200 || len(svc.sessions) != 1 || svc.sessions[0] != "" {
		t.Errorf("status %d, sessions %v: a logout with no session is a quiet success", rec.Code, svc.sessions)
	}
}

func TestLogoutErrorsKeepTheCookie(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"limit":  {domain.ErrLimitExceeded, stdhttp.StatusTooManyRequests},
		"outage": {errors.New("redis down"), stdhttp.StatusInternalServerError},
	} {
		rec := post(t, &logoutFake{err: tc.err}, "/auth/logout", ``, true, "sess-1")
		if rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d", name, rec.Code, tc.want)
		}

		for _, cookie := range rec.Result().Cookies() {
			if cookie.Name == sessionCookieName {
				t.Errorf("%s: the cookie must not be cleared when the session could not be ended", name)
			}
		}
	}
}

func TestGradingAndRecommendationsReportTheUserAction(t *testing.T) {
	for route, action := range map[string]string{
		"/grading/my-org/my-repo":         domain.AuditActionGrading,
		"/recommendations/my-org/my-repo": domain.AuditActionRecommendations,
	} {
		svc := &fakeService{}

		serve(t, svc, "GET", route+"?x=1", map[string]string{"Cookie": "session_id=abc", "PAT": "Bearer tok", "X-Forwarded-For": "198.51.100.7"})

		if len(svc.userActions) != 1 {
			t.Fatalf("%s: %d actions reported, want 1", route, len(svc.userActions))
		}

		got := svc.userActions[0]
		if got.Action != action || got.SessionID != "abc" || !got.HasToken || got.OrgSlug != "my-org" || got.RepoSlug != "my-repo" || got.Err != nil {
			t.Errorf("%s: reported %+v", route, got)
		}
	}
}

func TestAFailedRequestIsReportedWithItsErrorAndNoToken(t *testing.T) {
	svc := &fakeService{err: domain.ErrNotFound}

	serve(t, svc, "GET", "/grading/o/r", nil)

	if len(svc.userActions) != 1 || !errors.Is(svc.userActions[0].Err, domain.ErrNotFound) || svc.userActions[0].HasToken || svc.userActions[0].SessionID != "" {
		t.Errorf("reported %+v", svc.userActions)
	}
}

func TestAuditRouteForwardsAValidScope(t *testing.T) {
	filter, err := mapAuditRequest(httptest.NewRequest("GET", "/admin/audit?scope=users&limit=10", nil))
	if err != nil || filter.Scope != "users" || filter.Limit != 10 {
		t.Errorf("filter = %+v, err %v", filter, err)
	}
}

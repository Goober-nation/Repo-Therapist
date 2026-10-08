package http

import (
	"encoding/json"
	"fmt"
	stdhttp "net/http"
	"strconv"
	"time"

	"gateway-service/internal/domain"
)

// adminRequestHeader must be present on every state-changing admin request. A page on another site
// cannot add a custom header to a cross-site request without a CORS preflight, which the admin routes
// never grant, so the cookie alone cannot be used to forge an admin action.
const (
	adminRequestHeader = "X-Admin-Request"
	adminRequestValue  = "1"
)

type adminHandlerFunc func(w stdhttp.ResponseWriter, r *stdhttp.Request, admin domain.AdminIdentity)

// requireAdmin guards an admin route: it checks the CSRF header on state-changing methods, then the
// session. Admin responses are never cached and carry no CORS headers.
func (h *Handler) requireAdmin(next adminHandlerFunc) stdhttp.HandlerFunc {
	return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Cache-Control", "no-store")

		if !isSafeMethod(r.Method) && r.Header.Get(adminRequestHeader) != adminRequestValue {
			writeJSON(w, stdhttp.StatusForbidden, errorResponse{Error: "access denied"})
			return
		}

		admin, err := h.service.AuthenticateAdmin(r.Context(), clientIP(r), sessionCookie(r), r.URL.Path)
		if err != nil {
			writeError(w, err)
			return
		}

		next(w, r, admin)
	}
}

func isSafeMethod(method string) bool {
	return method == stdhttp.MethodGet || method == stdhttp.MethodHead || method == stdhttp.MethodOptions
}

func sessionCookie(r *stdhttp.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}

	return cookie.Value
}

type adminMeResponse struct {
	IsAdmin bool `json:"is_admin"`
	// SignedIn: the session cookie belongs to a signed-in account.
	SignedIn bool `json:"signed_in"`
	// UnlockAvailable: a password unlock is configured.
	UnlockAvailable bool `json:"unlock_available"`
	// ViaPassword: the admin rights come from a password unlock of this session.
	ViaPassword bool `json:"via_password"`
}

func adminMe(status domain.AdminStatus) adminMeResponse {
	return adminMeResponse{
		IsAdmin:         status.IsAdmin,
		SignedIn:        status.SignedIn,
		UnlockAvailable: status.UnlockAvailable,
		ViaPassword:     status.ViaPassword,
	}
}

// GetAdminMe serves GET /admin/me: the caller's admin rights. Always 200 for a healthy gateway,
// including for visitors without a session, so the UI can ask on every page load without errors.
func (h *Handler) GetAdminMe(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	w.Header().Set("Cache-Control", "no-store")

	status, err := h.service.AdminStatus(r.Context(), sessionCookie(r))
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, stdhttp.StatusOK, adminMe(status))
}

// maxUnlockBodyBytes bounds the unlock request body; a password is at most 256 bytes.
const maxUnlockBodyBytes = 1024

type unlockRequest struct {
	Password string `json:"password"`
}

// checkAdminHeader enforces the CSRF header on a state-changing admin request and answers 403 when it is
// missing; it reports whether the request may go on.
func checkAdminHeader(w stdhttp.ResponseWriter, r *stdhttp.Request) bool {
	if r.Header.Get(adminRequestHeader) == adminRequestValue {
		return true
	}

	writeJSON(w, stdhttp.StatusForbidden, errorResponse{Error: "access denied"})

	return false
}

// PostAdminUnlock serves POST /admin/unlock {password}: a signed-in account that sends the admin password
// is an admin for the rest of its session. 401 without a session, 403 for a wrong password, 429 with
// Retry-After while locked after too many wrong ones. The password is read from the body only, never
// from the URL, and is never logged or echoed.
func (h *Handler) PostAdminUnlock(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	w.Header().Set("Cache-Control", "no-store")

	if !checkAdminHeader(w, r) {
		return
	}

	var body unlockRequest

	decoder := json.NewDecoder(stdhttp.MaxBytesReader(w, r.Body, maxUnlockBodyBytes))
	if err := decoder.Decode(&body); err != nil {
		writeError(w, httpError{status: stdhttp.StatusBadRequest, err: fmt.Errorf("%w: body", domain.ErrInvalidRequest)})
		return
	}

	sessionID := sessionCookie(r)

	if err := h.service.UnlockAdmin(r.Context(), clientIP(r), sessionID, body.Password); err != nil {
		writeError(w, err)
		return
	}

	h.writeAdminStatus(w, r)
}

// PostAdminLock serves POST /admin/lock: takes the password unlock off the session.
func (h *Handler) PostAdminLock(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	w.Header().Set("Cache-Control", "no-store")

	if !checkAdminHeader(w, r) {
		return
	}

	if err := h.service.LockAdmin(r.Context(), sessionCookie(r)); err != nil {
		writeError(w, err)
		return
	}

	h.writeAdminStatus(w, r)
}

func (h *Handler) writeAdminStatus(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	status, err := h.service.AdminStatus(r.Context(), sessionCookie(r))
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, stdhttp.StatusOK, adminMe(status))
}

type auditEntryResponse struct {
	ID         int64           `json:"id"`
	At         time.Time       `json:"at"`
	ActorKind  string          `json:"actor_kind"`
	Actor      string          `json:"actor"`
	Action     string          `json:"action"`
	TargetKind string          `json:"target_kind"`
	Target     string          `json:"target"`
	Outcome    string          `json:"outcome"`
	Details    json.RawMessage `json:"details"`
}

type auditResponse struct {
	Entries      []auditEntryResponse `json:"entries"`
	NextBeforeID int64                `json:"next_before_id"`
}

// GetAudit serves GET /admin/audit?limit=&before_id=&action=&outcome=: one page of the audit log,
// newest first. before_id is the cursor from the previous page's next_before_id.
func (h *Handler) GetAudit(w stdhttp.ResponseWriter, r *stdhttp.Request, _ domain.AdminIdentity) {
	filter, err := mapAuditRequest(r)
	if err != nil {
		writeError(w, err)
		return
	}

	page, err := h.service.ListAudit(r.Context(), filter)
	if err != nil {
		writeError(w, err)
		return
	}

	response := auditResponse{Entries: make([]auditEntryResponse, 0, len(page.Entries)), NextBeforeID: page.NextBeforeID}

	for _, entry := range page.Entries {
		details := entry.Details
		if len(details) == 0 {
			details = json.RawMessage("{}")
		}

		response.Entries = append(response.Entries, auditEntryResponse{
			ID: entry.ID, At: entry.OccurredAt, ActorKind: entry.ActorKind, Actor: entry.ActorHash,
			Action: entry.Action, TargetKind: entry.TargetKind, Target: entry.Target,
			Outcome: entry.Outcome, Details: details,
		})
	}

	writeJSON(w, stdhttp.StatusOK, response)
}

func mapAuditRequest(r *stdhttp.Request) (domain.AuditFilter, error) {
	query := r.URL.Query()
	filter := domain.AuditFilter{Action: query.Get("action"), Outcome: query.Get("outcome")}

	if value := query.Get("limit"); value != "" {
		limit, err := strconv.ParseInt(value, 10, 32)
		if err != nil || limit < 0 {
			return domain.AuditFilter{}, invalidAuditParam("limit")
		}

		filter.Limit = int32(limit)
	}

	if value := query.Get("before_id"); value != "" {
		before, err := strconv.ParseInt(value, 10, 64)
		if err != nil || before < 0 {
			return domain.AuditFilter{}, invalidAuditParam("before_id")
		}

		filter.BeforeID = before
	}

	return filter, nil
}

func invalidAuditParam(name string) error {
	return httpError{status: stdhttp.StatusBadRequest, err: fmt.Errorf("%w: %s", domain.ErrInvalidRequest, name)}
}

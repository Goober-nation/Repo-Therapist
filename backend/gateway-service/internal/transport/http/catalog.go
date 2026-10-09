package http

import (
	stdhttp "net/http"
	"time"

	"gateway-service/internal/domain"
)

type catalogRefreshResponse struct {
	ID         int64      `json:"id"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Status     string     `json:"status"`
	Trigger    string     `json:"trigger"`
	Actor      string     `json:"actor"`
	Pages      int32      `json:"pages"`
	Repos      int32      `json:"repos"`
	Error      string     `json:"error"`
}

func catalogRefreshBody(r *domain.CatalogRefresh) *catalogRefreshResponse {
	if r == nil {
		return nil
	}

	return &catalogRefreshResponse{ID: r.ID, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, Status: r.Status,
		Trigger: r.Trigger, Actor: r.ActorHash, Pages: r.Pages, Repos: r.Repos, Error: r.Error}
}

// GetCatalog serves GET /admin/catalog: the public repo catalog's size, age and refreshes.
func (h *Handler) GetCatalog(w stdhttp.ResponseWriter, r *stdhttp.Request, _ domain.AdminIdentity) {
	status, err := h.service.GetCatalogStatus(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, stdhttp.StatusOK, map[string]any{
		"size": status.Size, "updated_at": status.UpdatedAt,
		"current": catalogRefreshBody(status.Current), "last_completed": catalogRefreshBody(status.LastCompleted),
	})
}

// PostCatalogRefresh serves POST /admin/catalog/refresh: starts a refresh (409 refresh_active while one runs).
func (h *Handler) PostCatalogRefresh(w stdhttp.ResponseWriter, r *stdhttp.Request, admin domain.AdminIdentity) {
	refresh, err := h.service.RefreshCatalog(r.Context(), admin.ActorHash)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, stdhttp.StatusAccepted, catalogRefreshBody(&refresh))
}

// PostCatalogCancel serves POST /admin/catalog/refresh/cancel: stops the running refresh
// (409 refresh_not_running when none runs).
func (h *Handler) PostCatalogCancel(w stdhttp.ResponseWriter, r *stdhttp.Request, admin domain.AdminIdentity) {
	refresh, err := h.service.CancelCatalogRefresh(r.Context(), admin.ActorHash)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, stdhttp.StatusOK, catalogRefreshBody(&refresh))
}

package http

import (
	stdhttp "net/http"

	"gateway-service/internal/domain"
)

// GetPublicSeries serves GET /stats/series/{name}: a graph of the Analytics tab. Only series built from
// public gradings exist here (an admin series is a 404); the range is fixed by the series. Read-only,
// public and CORS-open; answers are reused for a minute.
func (h *Handler) GetPublicSeries(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	setPublicCORS(w)

	result, err := h.service.PublicSeries(r.Context(), clientIP(r), r.PathValue("name"))
	if err != nil {
		writeError(w, err)
		return
	}

	w.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(w, stdhttp.StatusOK, result)
}

// GetAdminSeries serves GET /admin/metrics/{name}?range=1h|6h|24h|7d: a graph of the admin tab.
func (h *Handler) GetAdminSeries(w stdhttp.ResponseWriter, r *stdhttp.Request, _ domain.AdminIdentity) {
	result, err := h.service.AdminSeries(r.Context(), r.PathValue("name"), r.URL.Query().Get("range"))
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, stdhttp.StatusOK, result)
}

package http

import (
	stdhttp "net/http"

	"gateway-service/internal/domain"
)

type organizationResponse struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	Personal    bool   `json:"personal"`
}

type getMyOrgsResponse struct {
	Organizations  []organizationResponse `json:"organizations"`
	DefaultOrgSlug string                 `json:"default_org_slug"`
}

// GetMyOrgs serves GET /orgs: the organizations the PAT owner belongs to and the one to open
// first (the personal org when known, else the first listed).
func (h *Handler) GetMyOrgs(
	w stdhttp.ResponseWriter,
	r *stdhttp.Request,
) {
	pat := patFromRequest(r)
	if pat == "" {
		writeError(w, httpError{
			status: stdhttp.StatusUnauthorized,
			err:    domain.ErrUnauthenticated,
		})
		return
	}

	result, err := h.service.GetMyOrgs(r.Context(), clientIP(r), pat)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, stdhttp.StatusOK, mapGetMyOrgsResponse(result))
}

func mapGetMyOrgsResponse(result domain.GetMyOrgsDTO) getMyOrgsResponse {
	orgs := make([]organizationResponse, 0, len(result.Organizations))
	for _, org := range result.Organizations {
		orgs = append(orgs, organizationResponse{
			Slug:        org.Slug,
			DisplayName: org.DisplayName,
			Personal:    org.Personal,
		})
	}

	return getMyOrgsResponse{
		Organizations:  orgs,
		DefaultOrgSlug: result.DefaultOrgSlug,
	}
}

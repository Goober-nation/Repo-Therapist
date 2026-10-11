package http

import (
	"context"
	stdhttp "net/http"
	"time"

	"gateway-service/internal/domain"
)

// SearchService is the catalog search part of GatewayService.
type SearchService interface {
	SuggestCatalog(ctx context.Context, clientIP, query string) (domain.CatalogSuggestions, error)
	SearchCatalogRepos(ctx context.Context, clientIP string, request domain.CatalogSearchDTO) (domain.CatalogReposPage, error)
	SearchCatalogOrgs(ctx context.Context, clientIP string, request domain.CatalogSearchDTO) (domain.CatalogOrgsPage, error)
}

type catalogRepoResponse struct {
	SourcecraftUUID string           `json:"sourcecraft_uuid"`
	OrgSlug         string           `json:"org_slug"`
	RepoSlug        string           `json:"repo_slug"`
	Description     string           `json:"description"`
	WebURL          string           `json:"web_url"`
	Language        languageResponse `json:"language"`
	Likes           float64          `json:"likes"`
	LastActivityAt  *string          `json:"last_activity_at"`
	PublicScore     *int32           `json:"public_score"`
}

type catalogOrgResponse struct {
	Slug      string `json:"slug"`
	RepoCount int32  `json:"repo_count"`
}

type suggestResponse struct {
	Repos []catalogRepoResponse `json:"repos"`
	Orgs  []catalogOrgResponse  `json:"orgs"`
}

type searchReposResponse struct {
	Repos      []catalogRepoResponse `json:"repos"`
	TotalCount int32                 `json:"total_count"`
	Page       int32                 `json:"page"`
	PageSize   int32                 `json:"page_size"`
}

type searchOrgsResponse struct {
	Orgs       []catalogOrgResponse `json:"orgs"`
	TotalCount int32                `json:"total_count"`
	Page       int32                `json:"page"`
	PageSize   int32                `json:"page_size"`
}

// GetSearchSuggest serves GET /search/suggest?q=: the top 3 repos and top org for the search field.
func (h *Handler) GetSearchSuggest(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	setPublicCORS(w)

	result, err := h.service.SuggestCatalog(r.Context(), clientIP(r), r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, stdhttp.StatusOK, suggestResponse{
		Repos: mapCatalogRepoResponses(result.Repos),
		Orgs:  mapCatalogOrgResponses(result.Orgs),
	})
}

// GetSearchRepos serves GET /search/repos?q=&org=&page=&page_size=.
func (h *Handler) GetSearchRepos(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	setPublicCORS(w)

	request, err := mapSearchRequest(r)
	if err != nil {
		writeError(w, err)
		return
	}

	result, err := h.service.SearchCatalogRepos(r.Context(), clientIP(r), request)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, stdhttp.StatusOK, searchReposResponse{
		Repos:      mapCatalogRepoResponses(result.Repos),
		TotalCount: result.TotalCount,
		Page:       result.Page,
		PageSize:   result.PageSize,
	})
}

// GetSearchOrgs serves GET /search/orgs?q=&page=&page_size=.
func (h *Handler) GetSearchOrgs(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	setPublicCORS(w)

	request, err := mapSearchRequest(r)
	if err != nil {
		writeError(w, err)
		return
	}

	result, err := h.service.SearchCatalogOrgs(r.Context(), clientIP(r), request)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, stdhttp.StatusOK, searchOrgsResponse{
		Orgs:       mapCatalogOrgResponses(result.Orgs),
		TotalCount: result.TotalCount,
		Page:       result.Page,
		PageSize:   result.PageSize,
	})
}

func mapSearchRequest(r *stdhttp.Request) (domain.CatalogSearchDTO, error) {
	query := r.URL.Query()

	page, err := queryInt32(query.Get("page"), "page")
	if err != nil {
		return domain.CatalogSearchDTO{}, err
	}

	pageSize, err := queryInt32(query.Get("page_size"), "page_size")
	if err != nil {
		return domain.CatalogSearchDTO{}, err
	}

	return domain.CatalogSearchDTO{
		Query:    query.Get("q"),
		Org:      query.Get("org"),
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func mapCatalogRepoResponses(repos []domain.CatalogRepo) []catalogRepoResponse {
	result := make([]catalogRepoResponse, 0, len(repos))

	for _, repo := range repos {
		entry := catalogRepoResponse{
			SourcecraftUUID: repo.SourcecraftUUID,
			OrgSlug:         repo.OrgSlug,
			RepoSlug:        repo.RepoSlug,
			Description:     repo.Description,
			WebURL:          repo.WebURL,
			Language:        languageResponse{Name: repo.Language.Name, Color: repo.Language.Color},
			Likes:           repo.Likes,
			PublicScore:     repo.PublicScore,
		}

		if repo.LastActivityAt != nil {
			value := repo.LastActivityAt.Format(time.RFC3339Nano)
			entry.LastActivityAt = &value
		}

		result = append(result, entry)
	}

	return result
}

func mapCatalogOrgResponses(orgs []domain.CatalogOrg) []catalogOrgResponse {
	result := make([]catalogOrgResponse, 0, len(orgs))

	for _, org := range orgs {
		result = append(result, catalogOrgResponse{Slug: org.Slug, RepoCount: org.RepoCount})
	}

	return result
}

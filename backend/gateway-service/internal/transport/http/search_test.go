package http

import (
	"context"
	stdhttp "net/http"
	"testing"
	"time"

	"gateway-service/internal/domain"
)

func (f *fakeService) SuggestCatalog(ctx context.Context, clientIP, query string) (domain.CatalogSuggestions, error) {
	f.called, f.suggestFor = true, query
	return domain.CatalogSuggestions{Orgs: []domain.CatalogOrg{{Slug: "yandex", RepoCount: 4}}}, f.err
}

func (f *fakeService) SearchCatalogRepos(ctx context.Context, clientIP string, request domain.CatalogSearchDTO) (domain.CatalogReposPage, error) {
	f.called, f.search = true, request
	return f.reposOut, f.err
}

func (f *fakeService) SearchCatalogOrgs(ctx context.Context, clientIP string, request domain.CatalogSearchDTO) (domain.CatalogOrgsPage, error) {
	f.called, f.search = true, request
	return domain.CatalogOrgsPage{}, f.err
}

func TestSearchReposRouteForwardsQueryAndMapsEntries(t *testing.T) {
	score := int32(81)
	active := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	svc := &fakeService{reposOut: domain.CatalogReposPage{
		Repos: []domain.CatalogRepo{
			{OrgSlug: "org", RepoSlug: "graded", PublicScore: &score, LastActivityAt: &active},
			{OrgSlug: "org", RepoSlug: "fresh"},
		},
		TotalCount: 2, Page: 2, PageSize: 10,
	}}

	rec := serve(t, svc, stdhttp.MethodGet, "/search/repos?q=gr&org=org&page=2&page_size=10", nil)
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}

	if svc.search != (domain.CatalogSearchDTO{Query: "gr", Org: "org", Page: 2, PageSize: 10}) {
		t.Errorf("request = %+v", svc.search)
	}

	body := decode(t, rec)
	repos := body["repos"].([]any)
	if len(repos) != 2 || body["total_count"].(float64) != 2 {
		t.Fatalf("body = %v", body)
	}
	if first := repos[0].(map[string]any); first["public_score"].(float64) != 81 || first["last_activity_at"] == nil {
		t.Errorf("first = %v", first)
	}
	if second := repos[1].(map[string]any); second["public_score"] != nil {
		t.Errorf("ungraded repo has score %v, want null", second["public_score"])
	}
}

func TestSearchRejectsBadPage(t *testing.T) {
	svc := &fakeService{}

	rec := serve(t, svc, stdhttp.MethodGet, "/search/orgs?q=x&page=abc", nil)
	if rec.Code != stdhttp.StatusBadRequest || svc.called {
		t.Errorf("status = %d called = %v, want 400 without a service call", rec.Code, svc.called)
	}
}

func TestSuggestRoute(t *testing.T) {
	svc := &fakeService{}

	rec := serve(t, svc, stdhttp.MethodGet, "/search/suggest?q=yan", nil)
	if rec.Code != stdhttp.StatusOK || svc.suggestFor != "yan" {
		t.Fatalf("status = %d query = %q", rec.Code, svc.suggestFor)
	}

	body := decode(t, rec)
	if repos := body["repos"].([]any); len(repos) != 0 {
		t.Errorf("repos = %v, want an empty array", repos)
	}
	if orgs := body["orgs"].([]any); len(orgs) != 1 {
		t.Errorf("orgs = %v", orgs)
	}
}

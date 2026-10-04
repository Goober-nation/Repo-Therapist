package http

import (
	stdhttp "net/http"
	"testing"

	"gateway-service/internal/domain"

	"github.com/google/uuid"
)

func TestOrgsRouteReturnsOrgsAndDefault(t *testing.T) {
	svc := &fakeService{orgsOut: domain.GetMyOrgsDTO{
		Organizations: []domain.UserOrganization{
			{Slug: "alice", DisplayName: "alice Personal", Personal: true},
			{Slug: "team", DisplayName: "Team"},
		},
		DefaultOrgSlug: "alice",
	}}

	rec := serve(t, svc, stdhttp.MethodGet, "/orgs", map[string]string{"PAT": "Bearer user-pat"})
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
	body := decode(t, rec)
	if body["default_org_slug"] != "alice" || svc.pat != "user-pat" {
		t.Fatalf("body = %v pat = %q", body, svc.pat)
	}
	orgs := body["organizations"].([]any)
	first := orgs[0].(map[string]any)
	if len(orgs) != 2 || first["slug"] != "alice" || first["personal"] != true || first["display_name"] != "alice Personal" {
		t.Fatalf("organizations = %v", orgs)
	}
}

func TestOrgsRouteRequiresPAT(t *testing.T) {
	svc := &fakeService{}
	rec := serve(t, svc, stdhttp.MethodGet, "/orgs", nil)
	if rec.Code != stdhttp.StatusUnauthorized || svc.called {
		t.Fatalf("status = %d called = %v", rec.Code, svc.called)
	}
}

func TestReposRouteWithOrgUsesOrgListing(t *testing.T) {
	svc := &fakeService{orgReposOut: domain.GetMyReposDTO{
		Repositories:  []domain.Repo{{SourcecraftUUID: uuid.New(), OrgSlug: "team", RepoSlug: "r", Visibility: "private"}},
		NextPageToken: "more",
	}}

	rec := serve(t, svc, stdhttp.MethodGet, "/repos?org=team&page_token=abc", map[string]string{"PAT": "user-pat"})
	if rec.Code != stdhttp.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body)
	}
	if svc.orgReposFor != "team|abc" {
		t.Fatalf("org repos called with %q", svc.orgReposFor)
	}
	body := decode(t, rec)
	if body["next_page_token"] != "more" || len(body["repositories"].([]any)) != 1 {
		t.Fatalf("body = %v", body)
	}
}

func TestReposRouteWithoutOrgKeepsLegacyListing(t *testing.T) {
	svc := &fakeService{}
	rec := serve(t, svc, stdhttp.MethodGet, "/repos", map[string]string{"PAT": "user-pat"})
	if rec.Code != stdhttp.StatusOK || svc.orgReposFor != "" || !svc.called {
		t.Fatalf("status = %d orgReposFor = %q", rec.Code, svc.orgReposFor)
	}
}

package rpc

import (
	"context"
	"testing"
	"time"

	"rating-service/internal/domain"
	pb "rating-service/internal/transport/gen"
)

func (f *fakeService) SuggestCatalog(ctx context.Context, query string) (domain.Suggestions, error) {
	return f.suggestions, f.err
}

func (f *fakeService) SearchCatalogRepos(ctx context.Context, query, org string, page, pageSize int) (domain.CatalogReposPage, error) {
	f.searchReq = []any{query, org, page, pageSize}
	return f.reposPage, f.err
}

func (f *fakeService) SearchCatalogOrgs(ctx context.Context, query string, page, pageSize int) (domain.CatalogOrgsPage, error) {
	f.searchReq = []any{query, page, pageSize}
	return f.orgsPage, f.err
}

func TestSearchCatalogReposMapsEntries(t *testing.T) {
	score := int16(77)
	active := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	svc := &fakeService{reposPage: domain.CatalogReposPage{
		Repos: []domain.CatalogRepo{
			{SourcecraftUUID: "u1", OrgSlug: "org", RepoSlug: "graded", Language: domain.Language{Name: "Go"}, Likes: 3, LastActivityAt: &active, PublicScore: &score},
			{SourcecraftUUID: "u2", OrgSlug: "org", RepoSlug: "fresh"},
		},
		Total: 2, Page: 1, PageSize: 20,
	}}

	resp, err := NewServer(svc).SearchCatalogRepos(context.Background(), &pb.SearchCatalogReposRequest{Query: "gr", Org: "org", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}

	if got := svc.searchReq; got[0] != "gr" || got[1] != "org" || got[2] != 1 || got[3] != 20 {
		t.Errorf("request = %v", got)
	}
	if resp.GetTotalCount() != 2 || len(resp.GetRepos()) != 2 {
		t.Fatalf("response = %v", resp)
	}
	if first := resp.GetRepos()[0]; first.GetPublicScore() != 77 || first.GetLanguage().GetName() != "Go" || first.GetLastActivityAt() == nil {
		t.Errorf("first = %v", first)
	}
	if second := resp.GetRepos()[1]; second.PublicScore != nil || second.GetLanguage() == nil {
		t.Errorf("ungraded repo = %v, want no score and an empty language", second)
	}
}

func TestSuggestCatalogMapsOrgs(t *testing.T) {
	svc := &fakeService{suggestions: domain.Suggestions{Orgs: []domain.CatalogOrg{{Slug: "yandex", RepoCount: 12}}}}

	resp, err := NewServer(svc).SuggestCatalog(context.Background(), &pb.SuggestCatalogRequest{Query: "yan"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetOrgs()) != 1 || resp.GetOrgs()[0].GetRepoCount() != 12 || len(resp.GetRepos()) != 0 {
		t.Errorf("response = %v", resp)
	}
}

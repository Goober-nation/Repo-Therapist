package service

import (
	"errors"
	"testing"

	"aggregator-service/internal/domain"
	"aggregator-service/internal/transport/rpc/gen/client"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGetMyOrgsUsesUserToken(t *testing.T) {
	f := newFixture("public")
	f.wrapper.myOrgs = &client.GetMyOrgsResponse{
		Organizations: []*client.UserOrganization{
			{Slug: "alice", DisplayName: "alice Personal", Personal: true},
			{Slug: "team", DisplayName: "Team"},
		},
		DefaultOrgSlug: "alice",
	}

	got, err := f.svc.GetMyOrgs(userCtx("owner-pat"))
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultOrgSlug != "alice" || len(got.Organizations) != 2 || !got.Organizations[0].Personal {
		t.Fatalf("got %+v", got)
	}
	if len(f.wrapper.authSeen) != 1 || f.wrapper.authSeen[0] != "Bearer owner-pat" {
		t.Fatalf("auth seen = %v", f.wrapper.authSeen)
	}
}

func TestGetMyOrgsRequiresUserToken(t *testing.T) {
	f := newFixture("public")
	if _, err := f.svc.GetMyOrgs(userCtx("")); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("err = %v", err)
	}
	if len(f.wrapper.authSeen) != 0 {
		t.Fatal("wrapper must not be called with the service token")
	}
}

func TestGetOrgReposMapsRepositories(t *testing.T) {
	f := newFixture("public")
	f.wrapper.orgRepos = &client.GetOrgReposResponse{
		Repositories: []*client.Repository{{
			Id:           testRepoID.String(),
			Slug:         "repo",
			Organization: &client.Organization{Slug: "team"},
			Visibility:   "private",
		}},
		NextPageToken: "next",
	}

	got, err := f.svc.GetOrgRepos(userCtx("owner-pat"), "team", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repositories) != 1 || got.Repositories[0].OrgSlug != "team" || got.NextPageToken != "next" {
		t.Fatalf("got %+v", got)
	}
	seen := f.wrapper.orgReposSeen
	if len(seen) != 1 || seen[0].GetOrgSlug() != "team" || seen[0].GetPageToken() != "tok" {
		t.Fatalf("request = %v", seen)
	}
}

func TestGetOrgReposMapsWrapperErrors(t *testing.T) {
	f := newFixture("public")
	f.wrapper.orgReposErr = status.Error(codes.NotFound, "nope")
	if _, err := f.svc.GetOrgRepos(userCtx("owner-pat"), "team", ""); !errors.Is(err, domain.ErrRepositoryNotFound) {
		t.Fatalf("err = %v", err)
	}

	if _, err := f.svc.GetOrgRepos(userCtx(""), "team", ""); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("err = %v", err)
	}
}

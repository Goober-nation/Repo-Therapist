package service

import (
	"context"
	"errors"
	"testing"

	"gateway-service/internal/domain"
	"gateway-service/internal/transport/rpc/gen/agg"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGetMyOrgsForwardsPATAndMaps(t *testing.T) {
	aggregator := &fakeAggregator{myOrgs: &agg.GetMyOrgsResponse{
		Organizations:  []*agg.UserOrganization{{Slug: "alice", DisplayName: "A", Personal: true}, {Slug: "team"}},
		DefaultOrgSlug: "alice",
	}}

	got, err := newService(aggregator).GetMyOrgs(context.Background(), "ip", "user-pat")
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultOrgSlug != "alice" || len(got.Organizations) != 2 || !got.Organizations[0].Personal {
		t.Fatalf("got %+v", got)
	}
	if len(aggregator.authMD) != 1 || aggregator.authMD[0] != "Bearer user-pat" {
		t.Fatalf("authorization = %v", aggregator.authMD)
	}
}

func TestGetMyOrgsRequiresPAT(t *testing.T) {
	aggregator := &fakeAggregator{}
	if _, err := newService(aggregator).GetMyOrgs(context.Background(), "ip", ""); !errors.Is(err, domain.ErrUnauthenticated) || aggregator.calls != 0 {
		t.Fatalf("err = %v calls = %d", err, aggregator.calls)
	}
}

func TestGetOrgReposForwardsSlugAndToken(t *testing.T) {
	aggregator := &fakeAggregator{orgRepos: &agg.GetOrgReposResponse{
		Repositories:  []*agg.Repository{{Id: uuid.NewString(), Slug: "r", Organization: &agg.Organization{Slug: "team"}}},
		NextPageToken: "next",
	}}

	got, err := newService(aggregator).GetOrgRepos(context.Background(), "ip", "user-pat", " team ", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if aggregator.orgReposIn.GetOrgSlug() != "team" || aggregator.orgReposIn.GetPageToken() != "tok" {
		t.Fatalf("request = %v", aggregator.orgReposIn)
	}
	if len(got.Repositories) != 1 || got.Repositories[0].OrgSlug != "team" || got.NextPageToken != "next" {
		t.Fatalf("got %+v", got)
	}
}

func TestGetOrgReposValidatesInput(t *testing.T) {
	aggregator := &fakeAggregator{}
	svc := newService(aggregator)
	if _, err := svc.GetOrgRepos(context.Background(), "ip", "", "team", ""); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("no PAT: err = %v", err)
	}
	if _, err := svc.GetOrgRepos(context.Background(), "ip", "pat", " ", ""); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("no org: err = %v", err)
	}
	if aggregator.calls != 0 {
		t.Fatalf("calls = %d", aggregator.calls)
	}
}

func TestGetOrgReposMapsDeadlineToTimeout(t *testing.T) {
	aggregator := &fakeAggregator{err: status.Error(codes.DeadlineExceeded, "slow")}
	if _, err := newService(aggregator).GetOrgRepos(context.Background(), "ip", "pat", "team", ""); !errors.Is(err, domain.ErrTimeout) {
		t.Fatalf("err = %v", err)
	}
}

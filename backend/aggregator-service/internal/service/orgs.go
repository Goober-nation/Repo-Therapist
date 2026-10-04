package service

import (
	"context"

	"aggregator-service/internal/domain"
	"aggregator-service/internal/transport/rpc/gen/client"
)

// GetMyOrgs lists the caller's organizations with the caller's own token; the service token
// is never used here.
func (as *AggregatorService) GetMyOrgs(ctx context.Context) (domain.MyOrganizations, error) {
	token := tokenFromContext(ctx)
	if token == "" {
		return domain.MyOrganizations{}, domain.ErrUnauthenticated
	}

	cont, cancel := context.WithTimeout(withToken(ctx, token), as.RPCTimeout)
	defer cancel()

	response, err := as.clientRPC.GetMyOrgs(cont, &client.GetMyOrgsRequest{})
	if err != nil {
		return domain.MyOrganizations{}, wrapperError("get my organizations", err)
	}

	orgs := make([]domain.UserOrganization, 0, len(response.GetOrganizations()))
	for _, org := range response.GetOrganizations() {
		orgs = append(orgs, domain.UserOrganization{
			Slug:        org.GetSlug(),
			DisplayName: org.GetDisplayName(),
			Personal:    org.GetPersonal(),
		})
	}

	return domain.MyOrganizations{
		Organizations:  orgs,
		DefaultOrgSlug: response.GetDefaultOrgSlug(),
	}, nil
}

// GetOrgRepos returns one batch of an organization's repos visible to the caller's own token.
func (as *AggregatorService) GetOrgRepos(
	ctx context.Context,
	orgSlug string,
	pageToken string,
) (domain.MyRepositoriesResponse, error) {
	token := tokenFromContext(ctx)
	if token == "" {
		return domain.MyRepositoriesResponse{}, domain.ErrUnauthenticated
	}

	cont, cancel := context.WithTimeout(withToken(ctx, token), as.RPCTimeout)
	defer cancel()

	response, err := as.clientRPC.GetOrgRepos(cont, &client.GetOrgReposRequest{
		OrgSlug:   orgSlug,
		PageToken: pageToken,
	})
	if err != nil {
		return domain.MyRepositoriesResponse{}, wrapperError("get organization repositories", err)
	}

	repositories := make([]domain.Repo, 0, len(response.GetRepositories()))
	for _, pbRepo := range response.GetRepositories() {
		repo, err := repoFromWrapper(pbRepo)
		if err != nil {
			return domain.MyRepositoriesResponse{}, err
		}
		repositories = append(repositories, repo)
	}

	return domain.MyRepositoriesResponse{
		Repositories:  repositories,
		NextPageToken: response.GetNextPageToken(),
	}, nil
}

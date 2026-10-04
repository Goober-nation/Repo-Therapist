package service

import (
	"context"
	"strings"

	"gateway-service/internal/domain"
	"gateway-service/internal/transport/rpc/gen/agg"
)

// GetMyOrgs lists the organizations the PAT owner belongs to and the one to open first.
func (s *GatewayService) GetMyOrgs(
	ctx context.Context,
	clientIP string,
	pat string,
) (domain.GetMyOrgsDTO, error) {
	if err := s.checkRateLimit(ctx, clientIP); err != nil {
		return domain.GetMyOrgsDTO{}, err
	}
	if pat == "" {
		return domain.GetMyOrgsDTO{}, domain.ErrUnauthenticated
	}

	rpcCtx, cancel := context.WithTimeout(withPAT(ctx, pat), s.aggregatorTimeout)
	defer cancel()

	resp, err := s.aggregatorClient.GetMyOrgs(rpcCtx, &agg.GetMyOrgsRequest{})
	if err != nil {
		return domain.GetMyOrgsDTO{}, rpcError("get my orgs", err)
	}

	orgs := make([]domain.UserOrganization, 0, len(resp.GetOrganizations()))
	for _, org := range resp.GetOrganizations() {
		orgs = append(orgs, domain.UserOrganization{
			Slug:        org.GetSlug(),
			DisplayName: org.GetDisplayName(),
			Personal:    org.GetPersonal(),
		})
	}

	return domain.GetMyOrgsDTO{
		Organizations:  orgs,
		DefaultOrgSlug: resp.GetDefaultOrgSlug(),
	}, nil
}

// GetOrgRepos returns one batch of an organization's repos visible to the PAT owner.
func (s *GatewayService) GetOrgRepos(
	ctx context.Context,
	clientIP string,
	pat string,
	orgSlug string,
	pageToken string,
) (domain.GetMyReposDTO, error) {
	if err := s.checkRateLimit(ctx, clientIP); err != nil {
		return domain.GetMyReposDTO{}, err
	}
	if pat == "" {
		return domain.GetMyReposDTO{}, domain.ErrUnauthenticated
	}
	orgSlug = strings.TrimSpace(orgSlug)
	if orgSlug == "" {
		return domain.GetMyReposDTO{}, domain.ErrInvalidRequest
	}

	rpcCtx, cancel := context.WithTimeout(withPAT(ctx, pat), s.aggregatorTimeout)
	defer cancel()

	resp, err := s.aggregatorClient.GetOrgRepos(rpcCtx, &agg.GetOrgReposRequest{
		OrgSlug:   orgSlug,
		PageToken: pageToken,
	})
	if err != nil {
		return domain.GetMyReposDTO{}, rpcError("get org repos", err)
	}

	repositories := make([]domain.Repo, 0, len(resp.GetRepositories()))
	for _, repo := range resp.GetRepositories() {
		mapped, err := mapRepository(repo)
		if err != nil {
			return domain.GetMyReposDTO{}, err
		}
		repositories = append(repositories, mapped)
	}

	return domain.GetMyReposDTO{
		Repositories:  repositories,
		NextPageToken: resp.GetNextPageToken(),
	}, nil
}

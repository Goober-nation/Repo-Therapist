package service

import (
	"context"

	"gateway-service/internal/domain"
	"gateway-service/internal/transport/rpc/gen/rank"
)

// SuggestCatalog returns the few best public repo and org matches of query for the search field.
func (s *GatewayService) SuggestCatalog(ctx context.Context, clientIP, query string) (domain.CatalogSuggestions, error) {
	if err := s.checkRateLimit(ctx, clientIP); err != nil {
		return domain.CatalogSuggestions{}, err
	}

	rpcCtx, cancel := context.WithTimeout(ctx, s.ratingRPCTimeout())
	defer cancel()

	resp, err := s.ratingClient.SuggestCatalog(rpcCtx, &rank.SuggestCatalogRequest{Query: query})
	if err != nil {
		return domain.CatalogSuggestions{}, rpcError("suggest catalog", err)
	}

	return domain.CatalogSuggestions{
		Repos: mapCatalogRepos(resp.GetRepos()),
		Orgs:  mapCatalogOrgs(resp.GetOrgs()),
	}, nil
}

// SearchCatalogRepos returns one page of the public repos matching the request.
func (s *GatewayService) SearchCatalogRepos(ctx context.Context, clientIP string, request domain.CatalogSearchDTO) (domain.CatalogReposPage, error) {
	if err := s.checkRateLimit(ctx, clientIP); err != nil {
		return domain.CatalogReposPage{}, err
	}

	rpcCtx, cancel := context.WithTimeout(ctx, s.ratingRPCTimeout())
	defer cancel()

	resp, err := s.ratingClient.SearchCatalogRepos(rpcCtx, &rank.SearchCatalogReposRequest{
		Query:    request.Query,
		Org:      request.Org,
		Page:     request.Page,
		PageSize: request.PageSize,
	})
	if err != nil {
		return domain.CatalogReposPage{}, rpcError("search catalog repos", err)
	}

	return domain.CatalogReposPage{
		Repos:      mapCatalogRepos(resp.GetRepos()),
		TotalCount: resp.GetTotalCount(),
		Page:       resp.GetPage(),
		PageSize:   resp.GetPageSize(),
	}, nil
}

// SearchCatalogOrgs returns one page of the organizations matching the request.
func (s *GatewayService) SearchCatalogOrgs(ctx context.Context, clientIP string, request domain.CatalogSearchDTO) (domain.CatalogOrgsPage, error) {
	if err := s.checkRateLimit(ctx, clientIP); err != nil {
		return domain.CatalogOrgsPage{}, err
	}

	rpcCtx, cancel := context.WithTimeout(ctx, s.ratingRPCTimeout())
	defer cancel()

	resp, err := s.ratingClient.SearchCatalogOrgs(rpcCtx, &rank.SearchCatalogOrgsRequest{
		Query:    request.Query,
		Page:     request.Page,
		PageSize: request.PageSize,
	})
	if err != nil {
		return domain.CatalogOrgsPage{}, rpcError("search catalog orgs", err)
	}

	return domain.CatalogOrgsPage{
		Orgs:       mapCatalogOrgs(resp.GetOrgs()),
		TotalCount: resp.GetTotalCount(),
		Page:       resp.GetPage(),
		PageSize:   resp.GetPageSize(),
	}, nil
}

func mapCatalogRepos(repos []*rank.CatalogRepo) []domain.CatalogRepo {
	result := make([]domain.CatalogRepo, 0, len(repos))

	for _, repo := range repos {
		entry := domain.CatalogRepo{
			SourcecraftUUID: repo.GetSourcecraftUuid(),
			OrgSlug:         repo.GetOrgSlug(),
			RepoSlug:        repo.GetRepoSlug(),
			Description:     repo.GetDescription(),
			WebURL:          repo.GetWebUrl(),
			Language: domain.PrimaryLanguage{
				Name:  repo.GetLanguage().GetName(),
				Color: repo.GetLanguage().GetColor(),
			},
			Likes:       repo.GetLikes(),
			PublicScore: repo.PublicScore,
		}

		if repo.GetLastActivityAt() != nil {
			at := repo.GetLastActivityAt().AsTime()
			entry.LastActivityAt = &at
		}

		result = append(result, entry)
	}

	return result
}

func mapCatalogOrgs(orgs []*rank.CatalogOrg) []domain.CatalogOrg {
	result := make([]domain.CatalogOrg, 0, len(orgs))

	for _, org := range orgs {
		result = append(result, domain.CatalogOrg{Slug: org.GetSlug(), RepoCount: org.GetRepoCount()})
	}

	return result
}

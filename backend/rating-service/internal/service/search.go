package service

import (
	"context"
	"fmt"
	"strings"

	"rating-service/internal/domain"
)

const (
	// DefaultSearchPageSize and MaxSearchPageSize bound one page of search results.
	DefaultSearchPageSize = 20
	MaxSearchPageSize     = 100
	// MaxSearchQueryLength cuts longer queries; no repo or org name is that long.
	MaxSearchQueryLength = 200
	// suggestRepos and suggestOrgs are how many matches the search field suggests.
	suggestRepos = 3
	suggestOrgs  = 1
)

// SetSearch turns on the catalog search; without it the search calls fail.
func (s *RatingService) SetSearch(repo domain.SearchRepository) {
	s.search = repo
}

func normalizeQuery(query string) string {
	query = strings.ToLower(strings.TrimSpace(query))
	if len(query) > MaxSearchQueryLength {
		query = query[:MaxSearchQueryLength]
		// Do not cut a multi-byte character in half.
		query = strings.ToValidUTF8(query, "")
	}

	return query
}

func searchPage(page, pageSize int) (int, int, error) {
	if page < 0 || pageSize < 0 {
		return 0, 0, fmt.Errorf("%w: page and page_size must not be negative", domain.ErrInvalidArgument)
	}

	if page == 0 {
		page = 1
	}

	if pageSize == 0 {
		pageSize = DefaultSearchPageSize
	}

	if pageSize > MaxSearchPageSize {
		pageSize = MaxSearchPageSize
	}

	return page, pageSize, nil
}

func (s *RatingService) requireSearch() error {
	if s.search == nil {
		return fmt.Errorf("catalog search is not configured")
	}

	return nil
}

// SuggestCatalog returns the best few repo and org matches of query for the search field. An
// empty query suggests nothing.
func (s *RatingService) SuggestCatalog(ctx context.Context, query string) (domain.Suggestions, error) {
	if err := s.requireSearch(); err != nil {
		return domain.Suggestions{}, err
	}

	query = normalizeQuery(query)
	if query == "" {
		return domain.Suggestions{Repos: []domain.CatalogRepo{}, Orgs: []domain.CatalogOrg{}}, nil
	}

	repos, _, err := s.search.SearchRepos(ctx, query, "", 0, suggestRepos)
	if err != nil {
		return domain.Suggestions{}, err
	}

	orgs, _, err := s.search.SearchOrgs(ctx, query, 0, suggestOrgs)
	if err != nil {
		return domain.Suggestions{}, err
	}

	return domain.Suggestions{Repos: repos, Orgs: orgs}, nil
}

// SearchCatalogRepos returns one page of the catalog repos matching query; org, when set, keeps
// only that organization's repos (with an empty query: all of them).
func (s *RatingService) SearchCatalogRepos(ctx context.Context, query, org string, page, pageSize int) (domain.CatalogReposPage, error) {
	if err := s.requireSearch(); err != nil {
		return domain.CatalogReposPage{}, err
	}

	page, pageSize, err := searchPage(page, pageSize)
	if err != nil {
		return domain.CatalogReposPage{}, err
	}

	query, org = normalizeQuery(query), strings.TrimSpace(org)
	if query == "" && org == "" {
		return domain.CatalogReposPage{}, fmt.Errorf("%w: query or org is required", domain.ErrInvalidArgument)
	}

	repos, total, err := s.search.SearchRepos(ctx, query, org, (page-1)*pageSize, pageSize)
	if err != nil {
		return domain.CatalogReposPage{}, err
	}

	return domain.CatalogReposPage{Repos: repos, Total: total, Page: page, PageSize: pageSize}, nil
}

// SearchCatalogOrgs returns one page of the catalog organizations matching query.
func (s *RatingService) SearchCatalogOrgs(ctx context.Context, query string, page, pageSize int) (domain.CatalogOrgsPage, error) {
	if err := s.requireSearch(); err != nil {
		return domain.CatalogOrgsPage{}, err
	}

	page, pageSize, err := searchPage(page, pageSize)
	if err != nil {
		return domain.CatalogOrgsPage{}, err
	}

	query = normalizeQuery(query)
	if query == "" {
		return domain.CatalogOrgsPage{}, fmt.Errorf("%w: query is required", domain.ErrInvalidArgument)
	}

	orgs, total, err := s.search.SearchOrgs(ctx, query, (page-1)*pageSize, pageSize)
	if err != nil {
		return domain.CatalogOrgsPage{}, err
	}

	return domain.CatalogOrgsPage{Orgs: orgs, Total: total, Page: page, PageSize: pageSize}, nil
}

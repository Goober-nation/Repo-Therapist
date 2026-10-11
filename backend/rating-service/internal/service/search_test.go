package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"rating-service/internal/domain"
)

type fakeSearch struct {
	repoCalls [][]any
	orgCalls  [][]any
}

func (f *fakeSearch) SearchRepos(ctx context.Context, query, org string, offset, limit int) ([]domain.CatalogRepo, int, error) {
	f.repoCalls = append(f.repoCalls, []any{query, org, offset, limit})
	return []domain.CatalogRepo{{RepoSlug: "r"}}, 41, nil
}

func (f *fakeSearch) SearchOrgs(ctx context.Context, query string, offset, limit int) ([]domain.CatalogOrg, int, error) {
	f.orgCalls = append(f.orgCalls, []any{query, offset, limit})
	return []domain.CatalogOrg{{Slug: "o"}}, 5, nil
}

func newSearchService() (*RatingService, *fakeSearch) {
	search := &fakeSearch{}
	s := NewRatingService(&fakeRepo{}, 0)
	s.SetSearch(search)
	return s, search
}

func TestSuggestCatalogTakesThreeReposAndOneOrg(t *testing.T) {
	s, search := newSearchService()

	if _, err := s.SuggestCatalog(context.Background(), "  YanDex/Repo "); err != nil {
		t.Fatal(err)
	}

	if got := search.repoCalls[0]; got[0] != "yandex/repo" || got[2] != 0 || got[3] != 3 {
		t.Errorf("repo call = %v, want normalized query, offset 0, limit 3", got)
	}
	if got := search.orgCalls[0]; got[0] != "yandex/repo" || got[2] != 1 {
		t.Errorf("org call = %v, want limit 1", got)
	}
}

func TestSuggestCatalogEmptyQuerySuggestsNothing(t *testing.T) {
	s, search := newSearchService()

	got, err := s.SuggestCatalog(context.Background(), "   ")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Repos) != 0 || len(got.Orgs) != 0 || len(search.repoCalls) != 0 {
		t.Errorf("suggestions = %+v calls = %d, want nothing", got, len(search.repoCalls))
	}
}

func TestSearchCatalogReposPaging(t *testing.T) {
	s, search := newSearchService()

	page, err := s.SearchCatalogRepos(context.Background(), "", "Yandex", 3, 500)
	if err != nil {
		t.Fatal(err)
	}

	if got := search.repoCalls[0]; got[1] != "Yandex" || got[2] != 200 || got[3] != MaxSearchPageSize {
		t.Errorf("call = %v, want org filter, offset 200, capped limit", got)
	}
	if page.Total != 41 || page.Page != 3 || page.PageSize != MaxSearchPageSize {
		t.Errorf("page = %+v", page)
	}
}

func TestSearchCatalogRejectsBadInput(t *testing.T) {
	s, _ := newSearchService()

	if _, err := s.SearchCatalogRepos(context.Background(), " ", "", 1, 20); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("no query and no org: err = %v", err)
	}
	if _, err := s.SearchCatalogOrgs(context.Background(), "", 1, 20); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("empty org query: err = %v", err)
	}
	if _, err := s.SearchCatalogOrgs(context.Background(), "x", -1, 20); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Errorf("negative page: err = %v", err)
	}
}

func TestNormalizeQueryCutsLongInputOnRuneBoundary(t *testing.T) {
	q := normalizeQuery(strings.Repeat("я", MaxSearchQueryLength))
	if len(q) > MaxSearchQueryLength || !strings.HasPrefix(strings.Repeat("я", 200), q) {
		t.Errorf("len = %d", len(q))
	}
}

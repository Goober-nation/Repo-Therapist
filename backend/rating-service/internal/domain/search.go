package domain

import (
	"context"
	"time"
)

// CatalogRepo is one public repository of the catalog matched by a search, with its public score
// when it has been graded.
type CatalogRepo struct {
	SourcecraftUUID string
	OrgSlug         string
	RepoSlug        string
	Description     string
	WebURL          string
	Language        Language
	// Likes is the SourceCraft rating value.
	Likes          float64
	LastActivityAt *time.Time
	// PublicScore is nil when the repo has not been graded yet.
	PublicScore *int16
}

// CatalogOrg is one organization of the catalog with how many public repos it has.
type CatalogOrg struct {
	Slug      string
	RepoCount int
}

// CatalogReposPage is one page of a repo search.
type CatalogReposPage struct {
	Repos    []CatalogRepo
	Total    int
	Page     int
	PageSize int
}

// CatalogOrgsPage is one page of an org search.
type CatalogOrgsPage struct {
	Orgs     []CatalogOrg
	Total    int
	Page     int
	PageSize int
}

// Suggestions are the few best matches shown under the search field.
type Suggestions struct {
	Repos []CatalogRepo
	Orgs  []CatalogOrg
}

// SearchRepository searches the public repo catalog by name. query is lowercased and trimmed;
// an empty query matches everything. org, when set, keeps only that org's repos (exact,
// case-insensitive).
type SearchRepository interface {
	SearchRepos(ctx context.Context, query, org string, offset, limit int) ([]CatalogRepo, int, error)
	SearchOrgs(ctx context.Context, query string, offset, limit int) ([]CatalogOrg, int, error)
}

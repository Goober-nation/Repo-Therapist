package domain

import "time"

// CatalogRepo is a public repository of the catalog matched by a search.
type CatalogRepo struct {
	SourcecraftUUID string
	OrgSlug         string
	RepoSlug        string
	Description     string
	WebURL          string
	Language        PrimaryLanguage
	Likes           float64
	LastActivityAt  *time.Time
	// PublicScore is nil when the repo has not been graded yet.
	PublicScore *int32
}

// CatalogOrg is an organization of the catalog with its number of public repos.
type CatalogOrg struct {
	Slug      string
	RepoCount int32
}

type CatalogSuggestions struct {
	Repos []CatalogRepo
	Orgs  []CatalogOrg
}

type CatalogReposPage struct {
	Repos      []CatalogRepo
	TotalCount int32
	Page       int32
	PageSize   int32
}

type CatalogOrgsPage struct {
	Orgs       []CatalogOrg
	TotalCount int32
	Page       int32
	PageSize   int32
}

// CatalogSearchDTO is one search request: Org keeps only that organization's repos.
type CatalogSearchDTO struct {
	Query    string
	Org      string
	Page     int32
	PageSize int32
}

package domain

type RepositoryID struct {
	RepoId string `json:"id"`
}

type RepositoriesID struct {
	Repositories []RepositoryID `json:"repositories"`
}

type MyRepositoriesResponse struct {
	Repositories  []Repository `json:"repositories"`
	NextPageToken string       `json:"next_page_token"`
}

type Repository struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Organization Organization `json:"organization"`
	Slug         string       `json:"slug"`
	Description  string       `json:"description"`
	Logo         Logo         `json:"logo"`
	WebURL       string       `json:"web_url"`
}

type Logo struct {
	URL string `json:"url"`
}

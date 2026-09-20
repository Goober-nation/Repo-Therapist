package domain

type RepositoryID struct {
	RepoId string `json:"id"`
}

type RepositoriesID struct {
	Repositories []RepositoryID `json:"repositories"`
}

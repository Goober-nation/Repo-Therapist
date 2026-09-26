package domain

type MyRepositoriesResponse struct {
	Repositories  []Repo `json:"repositories"`
	NextPageToken string `json:"next_page_token"`
}
type ReactionCount struct {
	Type  string `json:"type"`
	Count string `json:"count"`
}

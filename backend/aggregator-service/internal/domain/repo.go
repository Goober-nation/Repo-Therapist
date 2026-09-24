package domain

type MyRepositoriesResponse struct {
	Repositories  []Repo `json:"repositories"`
	NextPageToken string `json:"next_page_token"`
}

type Logo struct {
	URL string `json:"url"`
}
type Organization struct {
	ID   string `json:"id"`
	Slug string `json:"slug"`
}

type Rating struct {
	Value          float64         `json:"value"`
	Percentile     float64         `json:"percentile"`
	ReactionCounts []ReactionCount `json:"reaction_counts"`
}

type ReactionCount struct {
	Type  string `json:"type"`
	Count string `json:"count"`
}

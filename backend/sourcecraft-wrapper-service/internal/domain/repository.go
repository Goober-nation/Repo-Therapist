package domain

import "time"

type MyRepositoriesResponse struct {
	Repositories  []Repository `json:"repositories"`
	NextPageToken string       `json:"next_page_token"`
}

type Repository struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Organization Organization    `json:"organization"`
	Slug         string          `json:"slug"`
	Description  string          `json:"description"`
	Logo         Logo            `json:"logo"`
	WebURL       string          `json:"web_url"`
	Visibility   string          `json:"visibility"`
	LastActivity *time.Time      `json:"last_updated"`
	Rating       Rating          `json:"rating"`
	Language     PrimaryLanguage `json:"language"`
}

type PrimaryLanguage struct {
	Name  string `json:"name"`
	Color string `json:"color"`
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
type Logo struct {
	URL string `json:"url"`
}

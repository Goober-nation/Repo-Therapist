package domain

import "time"

type RankingSortBy int32

const (
	RankingSortByScore RankingSortBy = iota
	RankingSortByLastActivity
	RankingSortByLikes
)

type GetRankingDTO struct {
	Page     int32
	PageSize int32
	Language string
	SortBy   RankingSortBy
	// HideLowConfidence leaves out repos whose public grade is flagged low-confidence.
	HideLowConfidence bool
	// Size is "", "small", "medium" or "large" (by lines of code); HasCI and HasSecurityPolicy filter
	// on a CI config file and a SECURITY.md (nil = any); Topic is searched in the name and description.
	Size                     string
	HasCI, HasSecurityPolicy *bool
	Topic                    string
}
type GetMyReposDTO struct {
	Repositories  []Repo `json:"repositories"`
	NextPageToken string `json:"nextPageToken"`
}

type GetGradingDTO struct {
	OrgSlug  string `json:"org_slug"`
	RepoSlug string `json:"repo_slug"`
}

type RequestLLMRecommendationsDTO struct {
	OrgSlug  string `json:"org_slug"`
	RepoSlug string `json:"repo_slug"`
}

type RequestLLMRecommendationsResponse struct {
	RawResponse []byte    `json:"raw_response"`
	ModelUsed   string    `json:"model_used"`
	EvaluatedAt time.Time `json:"evaluated_at"`
}

type GetRankingResponse struct {
	Entries    []RankingEntry
	TotalCount int32
	Page       int32
	PageSize   int32
	// DynamicsDays is the window of the entries' score and place change.
	DynamicsDays int32
}

type LoginWithYandexRequest struct {
	OAuthToken string `json:"oauth_token"`
}

type YandexLoginDTO struct {
	OAuthToken string
}

type LoginResponse struct {
	SessionID string `json:"session_id"`
}

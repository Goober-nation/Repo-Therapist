// Package sourcecraft is a minimal client for the SourceCraft public REST API.
// See agents_memory/topics/sourcecraft-api.md for the research behind these choices
// (bearer PAT required on every call, no anonymous tier).
package sourcecraft

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const DefaultBaseURL = "https://api.sourcecraft.tech"

// maxPages bounds pagination loops so a pathologically large repo (thousands of PRs/releases)
// can't make a single fetch run unbounded — matches the TZ's "must handle large repos" concern.
const maxPages = 50

type Client struct {
	baseURL    string
	pat        string
	httpClient *http.Client
}

func NewClient(baseURL, pat string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL:    baseURL,
		pat:        pat,
		httpClient: http.DefaultClient,
	}
}

// Repo is the subset of the SourceCraft Repository schema this service needs.
// Full schema has many more fields — see docs/api_docs.md.
type Repo struct {
	ID            string `json:"id"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
	Visibility    string `json:"visibility"`
	IsEmpty       bool   `json:"is_empty"`
	Organization  struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	} `json:"organization"`
	CloneURL struct {
		HTTPS string `json:"https"`
		SSH   string `json:"ssh"`
	} `json:"clone_url"`
}

// GetRepo fetches repo metadata by org+repo slug. Requires a bearer PAT —
// SourceCraft has no anonymous read tier, even for public repos.
func (c *Client) GetRepo(ctx context.Context, orgSlug, repoSlug string) (*Repo, error) {
	var repo Repo
	if err := c.getJSON(ctx, fmt.Sprintf("/repos/%s/%s", orgSlug, repoSlug), nil, &repo); err != nil {
		return nil, err
	}
	return &repo, nil
}

// PullRequest is the subset of SourceCraft's pull_request schema needed for Activity's MR-rate
// metric. Verified against the real API (2026-09-19, examples/vulnerable-repo).
type PullRequest struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Status    string `json:"status"` // "merged", "open", "closed", ...
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type listPullsResponse struct {
	PullRequests  []PullRequest `json:"pull_requests"`
	NextPageToken string        `json:"next_page_token"`
}

// ListPulls fetches every PR for a repo, paginated, capped at maxPages.
func (c *Client) ListPulls(ctx context.Context, orgSlug, repoSlug string) ([]PullRequest, error) {
	var all []PullRequest
	pageToken := ""
	for page := 0; page < maxPages; page++ {
		params := url.Values{"page_size": {"100"}}
		if pageToken != "" {
			params.Set("page_token", pageToken)
		}
		var resp listPullsResponse
		if err := c.getJSON(ctx, fmt.Sprintf("/repos/%s/%s/pulls", orgSlug, repoSlug), params, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.PullRequests...)
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return all, nil
}

// Release is the subset of SourceCraft's Release schema (per the swagger spec's `Release`
// definition, 2026-09-19) needed for Activity's release-cadence metrics. Unverified against a
// populated example — no repo found with actual release data during this session; releases
// appear to be a largely-unused feature on the platform distinct from git tags.
type Release struct {
	ID         string `json:"id"`
	Tag        string `json:"tag"`
	IsLatest   bool   `json:"is_latest"`
	CreatedAt  string `json:"created_at"`
	ReleasedAt string `json:"released_at"`
}

type listReleasesResponse struct {
	Releases      []Release `json:"releases"`
	NextPageToken string    `json:"next_page_token"`
}

// ListReleases fetches every release for a repo, paginated, capped at maxPages.
func (c *Client) ListReleases(ctx context.Context, orgSlug, repoSlug string) ([]Release, error) {
	var all []Release
	pageToken := ""
	for page := 0; page < maxPages; page++ {
		params := url.Values{"page_size": {"100"}}
		if pageToken != "" {
			params.Set("page_token", pageToken)
		}
		var resp listReleasesResponse
		if err := c.getJSON(ctx, fmt.Sprintf("/repos/%s/%s/releases", orgSlug, repoSlug), params, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Releases...)
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return all, nil
}

func (c *Client) getJSON(ctx context.Context, path string, params url.Values, out any) error {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.pat)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling %s: %w", u, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: unexpected status %d", u, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding response from %s: %w", u, err)
	}
	return nil
}

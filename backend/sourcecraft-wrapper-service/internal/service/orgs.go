package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	url2 "net/url"
	"strings"
	"time"

	"sourcecraft-wrapper-service/internal/domain"
)

const (
	// DefaultOrgReposBatch is how many repos GetOrgRepos tries to collect before returning.
	DefaultOrgReposBatch = 100
	// DefaultOrgReposBudget bounds how long GetOrgRepos keeps starting new pages. SourceCraft
	// often returns pages with only a few items, so a large org needs many round trips; the
	// rest is returned as a page token instead of running past the caller's deadline.
	DefaultOrgReposBudget = 5 * time.Second
)

// GetMyOrgs lists the caller's organizations (GET /me/orgs) and picks the default one: the
// personal org (slug equal to the GET /user username), otherwise the first listed org.
func (scw *SCWrapper) GetMyOrgs(ctx context.Context) (domain.MyOrganizations, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return domain.MyOrganizations{}, fmt.Errorf("%w: %w", domain.ErrUnauthenticated, err)
	}

	// The username only picks the default org, so fetch it in parallel and ignore failures.
	usernameCh := make(chan string, 1)
	go func() {
		var user struct {
			Username string `json:"username"`
		}
		if err := scw.getJSON(ctx, token, "/user", nil, &user); err != nil {
			usernameCh <- ""
			return
		}
		usernameCh <- user.Username
	}()

	var orgs []domain.UserOrganization
	pageToken := ""
	for page := 0; page < scw.MaxPages; page++ {
		var resp struct {
			Organizations []domain.UserOrganization `json:"organizations"`
			NextPageToken string                    `json:"next_page_token"`
		}
		params := url2.Values{}
		if pageToken != "" {
			params.Set("page_token", pageToken)
		}
		if err := scw.getJSON(ctx, token, "/me/orgs", params, &resp); err != nil {
			<-usernameCh
			return domain.MyOrganizations{}, err
		}
		orgs = append(orgs, resp.Organizations...)
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return pickDefaultOrg(orgs, <-usernameCh), nil
}

// pickDefaultOrg marks the personal org and chooses the default slug.
func pickDefaultOrg(orgs []domain.UserOrganization, username string) domain.MyOrganizations {
	result := domain.MyOrganizations{Organizations: orgs}
	for i := range orgs {
		if username != "" && strings.EqualFold(orgs[i].Slug, username) {
			orgs[i].Personal = true
			if result.DefaultOrgSlug == "" {
				result.DefaultOrgSlug = orgs[i].Slug
			}
		}
	}
	if result.DefaultOrgSlug == "" && len(orgs) > 0 {
		result.DefaultOrgSlug = orgs[0].Slug
	}
	return result
}

// GetOrgRepos returns one batch of GET /orgs/{orgSlug}/repos starting at pageToken. It stops
// after OrgReposBatch repos, when the list ends, or once OrgReposBudget has elapsed, and
// returns the token of the first page it did not read. A later page that fails returns what
// was collected so far with that page's token, so the client can retry from there.
func (scw *SCWrapper) GetOrgRepos(
	ctx context.Context,
	orgSlug string,
	pageToken string,
) (domain.MyRepositoriesResponse, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return domain.MyRepositoriesResponse{}, fmt.Errorf("%w: %w", domain.ErrUnauthenticated, err)
	}
	orgSlug = strings.TrimSpace(orgSlug)
	if orgSlug == "" {
		return domain.MyRepositoriesResponse{}, domain.ErrRepositoryNotFound
	}

	batch := scw.OrgReposBatch
	if batch <= 0 {
		batch = DefaultOrgReposBatch
	}
	budget := scw.OrgReposBudget
	if budget <= 0 {
		budget = DefaultOrgReposBudget
	}
	deadline := time.Now().Add(budget)
	path := "/orgs/" + url2.PathEscape(orgSlug) + "/repos"

	var result domain.MyRepositoriesResponse
	for page := 0; page < scw.MaxPages; page++ {
		pageCtx, cancel := ctx, context.CancelFunc(func() {})
		if page > 0 {
			// The first page is bounded only by the caller; later ones must fit the budget.
			pageCtx, cancel = context.WithDeadline(ctx, deadline)
		}

		var resp domain.MyRepositoriesResponse
		params := url2.Values{"page_size": {"100"}}
		if pageToken != "" {
			params.Set("page_token", pageToken)
		}
		err := scw.getJSON(pageCtx, token, path, params, &resp)
		cancel()
		if err != nil {
			if page == 0 {
				return domain.MyRepositoriesResponse{}, err
			}
			result.NextPageToken = pageToken
			return result, nil
		}

		result.Repositories = append(result.Repositories, resp.Repositories...)
		pageToken = resp.NextPageToken
		if pageToken == "" || len(result.Repositories) >= batch || !time.Now().Before(deadline) {
			break
		}
	}

	result.NextPageToken = pageToken
	return result, nil
}

// getJSON GETs path with params and decodes a 200 body into out. 404 maps to
// ErrRepositoryNotFound and 403 to ErrNoAccess.
func (scw *SCWrapper) getJSON(ctx context.Context, token, path string, params url2.Values, out any) error {
	u := scw.apiURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := scw.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrExternalAPI, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return domain.ErrRepositoryNotFound
	case http.StatusForbidden:
		return domain.ErrNoAccess
	case http.StatusUnauthorized:
		return domain.ErrUnauthenticated
	default:
		return fmt.Errorf("%w: external API returned status %s", domain.ErrExternalAPI, resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	return nil
}

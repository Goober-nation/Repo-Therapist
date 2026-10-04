package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	url2 "net/url"

	"sourcecraft-wrapper-service/internal/domain"
)

// MaxPullRequestsLimit is the SourceCraft page-size cap; one page is all GetPullRequests reads.
const MaxPullRequestsLimit = 100

// GetPullRequests returns the newest pull requests of a repository (any status), newest first.
// It reads a single page, so limit is capped at MaxPullRequestsLimit.
func (scw *SCWrapper) GetPullRequests(ctx context.Context, repoID string, limit int) (domain.PullRequests, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return domain.PullRequests{}, fmt.Errorf("%w:%w", domain.ErrUnauthenticated, err)
	}

	if limit <= 0 || limit > MaxPullRequestsLimit {
		limit = MaxPullRequestsLimit
	}

	path := fmt.Sprintf("/repos/id:%s/pulls", repoID)
	query := url2.Values{"page_size": {fmt.Sprint(limit)}, "sort_by": {"-created_at"}}

	var resp domain.PullRequests
	if err := scw.fetchJSON(ctx, token, path+"?"+query.Encode(), &resp); err != nil {
		return domain.PullRequests{}, err
	}

	if len(resp.PullRequests) > limit {
		resp.PullRequests = resp.PullRequests[:limit]
	}

	return resp, nil
}

// GetPullRequestActivity returns the reviewers and the comments (first page of each) of one pull
// request. Only metadata is kept: decisions, authors, timestamps and flags, never comment text.
func (scw *SCWrapper) GetPullRequestActivity(ctx context.Context, pullRequestID string) (domain.PullRequestActivity, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return domain.PullRequestActivity{}, fmt.Errorf("%w:%w", domain.ErrUnauthenticated, err)
	}

	base := fmt.Sprintf("/pulls/id:%s", pullRequestID)

	var reviewers struct {
		Reviewers []domain.PullRequestReviewer `json:"reviewers"`
	}
	if err := scw.fetchJSON(ctx, token, base+"/reviewers", &reviewers); err != nil {
		return domain.PullRequestActivity{}, err
	}

	var comments struct {
		Comments []domain.PullRequestComment `json:"pull_request_comments"`
	}
	if err := scw.fetchJSON(ctx, token, base+"/comments?page_size=100", &comments); err != nil {
		return domain.PullRequestActivity{}, err
	}

	return domain.PullRequestActivity{Reviewers: reviewers.Reviewers, Comments: comments.Comments}, nil
}

// fetchJSON performs one authorized GET of path (with its query) and decodes the JSON body into out.
func (scw *SCWrapper) fetchJSON(ctx context.Context, token, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scw.apiURL+path, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := scw.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrExternalAPI, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return domain.ErrRepositoryNotFound
		}

		return fmt.Errorf("%w: external API returned status %s", domain.ErrExternalAPI, resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}

	return nil
}

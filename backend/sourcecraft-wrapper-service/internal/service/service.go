package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	url2 "net/url"
	"sourcecraft-wrapper-service/internal/domain"
	"strings"

	"google.golang.org/grpc/metadata"
)

// maxPages bounds pagination loops so a pathologically large repo (thousands of issues/
// milestones) can't make a single fetch run unbounded.
const maxPages = 50

type SCService interface {
	GetRepositoryBySlugs(ctx context.Context, repoSlug string, orgSlug string) (domain.Repository, error)
	GetGitUrl(ctx context.Context, repoID string) (domain.CloneURL, error)
	GetCiCdRuns(ctx context.Context, repoID string) (domain.Runs, error)
	GetIssues(ctx context.Context, repoID string) (domain.Issues, error)
	GetMilestones(ctx context.Context, repoID string) (domain.Milestones, error)
	ListAllPublicRepos(ctx context.Context) ([]domain.Repository, error)
	GetMyRepos(ctx context.Context) (domain.MyRepositoriesResponse, error)
}

type SCWrapper struct {
	client http.Client
	apiURL string
}

func NewSCWrapperService(client http.Client, apiURL string) *SCWrapper {
	return &SCWrapper{
		client: client,
		apiURL: apiURL,
	}
}

func (scw *SCWrapper) GetRepositoryBySlugs(ctx context.Context, repoSlug string, orgSlug string) (domain.Repository, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return domain.Repository{}, fmt.Errorf(
			"%w: %w",
			domain.ErrUnauthenticated,
			err,
		)
	}

	url := fmt.Sprintf(
		"%s/repos/%s/%s",
		scw.apiURL,
		orgSlug,
		repoSlug,
	)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		url,
		nil,
	)
	if err != nil {
		return domain.Repository{}, fmt.Errorf(
			"%w: %w",
			domain.ErrInternalError,
			err,
		)
	}

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := scw.client.Do(req)
	if err != nil {
		return domain.Repository{}, fmt.Errorf(
			"%w: %w",
			domain.ErrExternalAPI,
			err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusNotFound:
			return domain.Repository{}, domain.ErrRepositoryNotFound

		case http.StatusForbidden:
			return domain.Repository{}, domain.ErrNoAccess

		default:
			return domain.Repository{}, fmt.Errorf(
				"%w: external API returned status %s",
				domain.ErrExternalAPI,
				resp.Status,
			)
		}
	}

	var repo domain.Repository
	if err := json.NewDecoder(resp.Body).Decode(&repo); err != nil {
		return domain.Repository{}, fmt.Errorf(
			"%w: %w",
			domain.ErrInternalError,
			err,
		)
	}

	return repo, nil
}

func (scw *SCWrapper) GetGitUrl(ctx context.Context, repoID string) (domain.CloneURL, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return domain.CloneURL{}, fmt.Errorf("%w:%w", domain.ErrUnauthenticated, err)
	}
	url := fmt.Sprintf("%s/repos/id:%s", scw.apiURL, repoID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return domain.CloneURL{}, fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := scw.client.Do(req)
	if err != nil {
		return domain.CloneURL{}, fmt.Errorf("%w: %w", domain.ErrExternalAPI, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return domain.CloneURL{}, domain.ErrRepositoryNotFound
		}
		return domain.CloneURL{}, fmt.Errorf(
			"%w: external API returned status %s",
			domain.ErrExternalAPI,
			resp.Status,
		)
	}
	// GET /repos/id:{id} returns the full repo object; the clone URLs are nested under
	// its "clone_url" key, not top-level.
	var repoResponse struct {
		CloneURL domain.CloneURL `json:"clone_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&repoResponse); err != nil {
		return domain.CloneURL{}, fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	return repoResponse.CloneURL, nil
}

func (scw *SCWrapper) GetCiCdRuns(ctx context.Context, repoID string) (domain.Runs, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return domain.Runs{}, fmt.Errorf("%w:%w", domain.ErrUnauthenticated, err)
	}
	url := fmt.Sprintf("%s/repos/id:%s/cicd/runs", scw.apiURL, repoID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return domain.Runs{}, fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := scw.client.Do(req)
	if err != nil {
		return domain.Runs{}, fmt.Errorf("%w: %w", domain.ErrExternalAPI, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return domain.Runs{}, domain.ErrRepositoryNotFound
		}
		return domain.Runs{}, fmt.Errorf(
			"%w: external API returned status %s",
			domain.ErrExternalAPI,
			resp.Status,
		)
	}

	var runs domain.Runs

	if err := json.NewDecoder(resp.Body).Decode(&runs); err != nil {
		return domain.Runs{}, fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	return runs, nil
}

func (scw *SCWrapper) GetIssues(ctx context.Context, repoID string) (domain.Issues, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return domain.Issues{}, fmt.Errorf("%w:%w", domain.ErrUnauthenticated, err)
	}
	basePath := fmt.Sprintf("/repos/id:%s/issues", repoID)

	var all domain.Issues
	pageToken := ""
	for page := 0; page < maxPages; page++ {
		var resp domain.Issues
		next, err := scw.getPage(ctx, token, basePath, pageToken, &resp, func() string { return resp.NextPageToken })
		if err != nil {
			return domain.Issues{}, err
		}
		all.Issues = append(all.Issues, resp.Issues...)
		if next == "" {
			break
		}
		pageToken = next
	}
	return all, nil
}

func (scw *SCWrapper) GetMilestones(ctx context.Context, repoID string) (domain.Milestones, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return domain.Milestones{}, fmt.Errorf("%w:%w", domain.ErrUnauthenticated, err)
	}
	basePath := fmt.Sprintf("/repos/id:%s/milestones", repoID)

	var all domain.Milestones
	pageToken := ""
	for page := 0; page < maxPages; page++ {
		var resp domain.Milestones
		next, err := scw.getPage(ctx, token, basePath, pageToken, &resp, func() string { return resp.NextPageToken })
		if err != nil {
			return domain.Milestones{}, err
		}
		all.Items = append(all.Items, resp.Items...)
		if next == "" {
			break
		}
		pageToken = next
	}
	return all, nil
}

// getPage fetches one page of a paginated GET endpoint into out, returning the next page's token
// ("" if this was the last page).
func (scw *SCWrapper) getPage(ctx context.Context, token, path, pageToken string, out any, nextToken func() string) (string, error) {
	u := scw.apiURL + path
	params := url2.Values{"page_size": {"100"}}
	if pageToken != "" {
		params.Set("page_token", pageToken)
	}
	u += "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := scw.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %w", domain.ErrExternalAPI, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return "", domain.ErrRepositoryNotFound
		}
		return "", fmt.Errorf("%w: external API returned status %s", domain.ErrExternalAPI, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return "", fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	return nextToken(), nil
}

func (scw *SCWrapper) ListAllPublicRepos(
	ctx context.Context,
) ([]domain.Repository, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: %w",
			domain.ErrUnauthenticated,
			err,
		)
	}

	var repositories []domain.Repository
	pageToken := ""

	for page := 0; page < maxPages; page++ {
		url := fmt.Sprintf("%s/repos", scw.apiURL)

		if pageToken != "" {
			url += "?page_token=" + url2.QueryEscape(pageToken)
		}

		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			url,
			nil,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: %w",
				domain.ErrInternalError,
				err,
			)
		}

		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := scw.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: %w",
				domain.ErrExternalAPI,
				err,
			)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()

			return nil, fmt.Errorf(
				"%w: external API returned status %s",
				domain.ErrExternalAPI,
				resp.Status,
			)
		}

		var result struct {
			Repositories  []domain.Repository `json:"repositories"`
			NextPageToken string              `json:"next_page_token"`
		}

		err = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if err != nil {
			return nil, fmt.Errorf(
				"%w: %w",
				domain.ErrInternalError,
				err,
			)
		}

		repositories = append(repositories, result.Repositories...)

		if result.NextPageToken == "" {
			break
		}

		pageToken = result.NextPageToken
	}

	return repositories, nil
}

func tokenFromContext(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", domain.ErrUnauthenticated
	}

	values := md.Get("authorization")
	if len(values) == 0 {
		return "", domain.ErrUnauthenticated
	}

	token := strings.TrimSpace(values[0])
	if token == "" {
		return "", domain.ErrUnauthenticated
	}

	token = strings.TrimPrefix(token, "Bearer ")
	token = strings.TrimPrefix(token, "bearer ")
	token = strings.TrimSpace(token)

	if token == "" {
		return "", domain.ErrUnauthenticated
	}

	return token, nil
}

func (scw *SCWrapper) GetMyRepos(ctx context.Context) (domain.MyRepositoriesResponse, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return domain.MyRepositoriesResponse{}, fmt.Errorf("%w:%w", domain.ErrUnauthenticated, err)
	}

	basePath := "/me/repos"

	var all domain.MyRepositoriesResponse
	pageToken := ""

	for page := 0; page < maxPages; page++ {
		var rr domain.MyRepositoriesResponse

		next, err := scw.getPage(
			ctx,
			token,
			basePath,
			pageToken,
			&rr,
			func() string {
				return rr.NextPageToken
			},
		)
		if err != nil {
			return domain.MyRepositoriesResponse{}, err
		}

		all.Repositories = append(all.Repositories, rr.Repositories...)

		if next == "" {
			break
		}

		pageToken = next
	}

	return all, nil
}

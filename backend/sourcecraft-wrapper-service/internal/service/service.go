package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sourcecraft-wrapper-service/internal/domain"
	"strings"

	"google.golang.org/grpc/metadata"
)

type SCService interface {
	GetRepoIdBySlugs(ctx context.Context, repoSlug string, orgSlug string) (string, error)
	GetGitUrl(ctx context.Context, repoID string) (domain.CloneURL, error)
	GetCiCdRuns(ctx context.Context, repoID string) (domain.Runs, error)
	GetIssues(ctx context.Context, repoID string) (domain.Issues, error)
	GetMilestones(ctx context.Context, repoID string) (domain.Milestones, error)
	ListAllPublicReposId(ctx context.Context) ([]string, error)
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

func (scw *SCWrapper) GetRepoIdBySlugs(ctx context.Context, repoSlug, orgSlug string) (string, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return "", fmt.Errorf("%w:%w", domain.ErrUnauthenticated, err)
	}
	url := fmt.Sprintf("%s/repos/%s/%s", scw.apiURL, orgSlug, repoSlug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
		} else if resp.StatusCode == http.StatusForbidden {
			return "", domain.ErrNoAccess
		}
		return "", fmt.Errorf(
			"%w: external API returned status %s",
			domain.ErrExternalAPI,
			resp.Status,
		)
	}
	var rr domain.RepositoryID
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return "", fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	return rr.RepoId, nil
}

func (scw *SCWrapper) GetGitUrl(ctx context.Context, repoID string) (domain.CloneURL, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return domain.CloneURL{}, fmt.Errorf("%w:%w", domain.ErrUnauthenticated, err)
	}
	url := fmt.Sprintf("%s/repos/%s", scw.apiURL, repoID)

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
	var urlResponse domain.CloneURL
	if err := json.NewDecoder(resp.Body).Decode(&urlResponse); err != nil {
		return domain.CloneURL{}, fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	return urlResponse, nil
}

func (scw *SCWrapper) GetCiCdRuns(ctx context.Context, repoID string) (domain.Runs, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return domain.Runs{}, fmt.Errorf("%w:%w", domain.ErrUnauthenticated, err)
	}
	url := fmt.Sprintf("%s/repos/%s/cicd/runs", scw.apiURL, repoID)
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
	url := fmt.Sprintf("%s/repos/%s/issues", scw.apiURL, repoID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return domain.Issues{}, fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := scw.client.Do(req)
	if err != nil {
		return domain.Issues{}, fmt.Errorf("%w: %w", domain.ErrExternalAPI, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return domain.Issues{}, domain.ErrRepositoryNotFound
		}
		return domain.Issues{}, fmt.Errorf(
			"%w: external API returned status %s",
			domain.ErrExternalAPI,
			resp.Status,
		)
	}
	var issues domain.Issues

	if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
		return domain.Issues{}, fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	return issues, nil
}

func (scw *SCWrapper) GetMilestones(ctx context.Context, repoID string) (domain.Milestones, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return domain.Milestones{}, fmt.Errorf("%w:%w", domain.ErrUnauthenticated, err)
	}
	url := fmt.Sprintf("%s/repos/%s/milestones", scw.apiURL, repoID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return domain.Milestones{}, fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := scw.client.Do(req)
	if err != nil {
		return domain.Milestones{}, fmt.Errorf("%w: %w", domain.ErrExternalAPI, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return domain.Milestones{}, domain.ErrRepositoryNotFound
		}
		return domain.Milestones{}, fmt.Errorf(
			"%w: external API returned status %s",
			domain.ErrExternalAPI,
			resp.Status,
		)
	}
	var milestones domain.Milestones

	if err := json.NewDecoder(resp.Body).Decode(&milestones); err != nil {
		return domain.Milestones{}, fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	return milestones, nil
}

func (scw *SCWrapper) ListAllPublicReposId(ctx context.Context) ([]string, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w:%w", domain.ErrUnauthenticated, err)
	}
	url := fmt.Sprintf("%s/repos", scw.apiURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := scw.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrExternalAPI, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"%w: external API returned status %s",
			domain.ErrExternalAPI,
			resp.Status,
		)
	}
	var ids domain.RepositoriesID

	if err := json.NewDecoder(resp.Body).Decode(&ids); err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrInternalError, err)
	}
	repoIDs := make([]string, 0, len(ids.Repositories))

	for _, repo := range ids.Repositories {
		repoIDs = append(repoIDs, repo.RepoId)
	}
	return repoIDs, nil
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

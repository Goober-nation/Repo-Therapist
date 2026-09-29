package service

import (
	"context"
	"fmt"
	"strings"

	"rating-service/internal/domain"
)

const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

type RatingService struct {
	repo domain.RankingRepository
}

func NewRatingService(repo domain.RankingRepository) *RatingService {
	return &RatingService{
		repo: repo,
	}
}

func (s *RatingService) GetRanking(ctx context.Context, req domain.RankingRequest) (domain.RankingPage, error) {
	query, page, err := buildQuery(req)
	if err != nil {
		return domain.RankingPage{}, err
	}

	total, err := s.repo.CountRanking(ctx, query.Language)
	if err != nil {
		return domain.RankingPage{}, err
	}

	result := domain.RankingPage{
		Entries:    []domain.RankingEntry{},
		TotalCount: total,
		Page:       page,
		PageSize:   query.Limit,
	}

	// Past the last page — nothing to list.
	if query.Offset >= total {
		return result, nil
	}

	rows, err := s.repo.ListRanking(ctx, query)
	if err != nil {
		return domain.RankingPage{}, err
	}

	result.Entries = make([]domain.RankingEntry, 0, len(rows))
	for i, row := range rows {
		result.Entries = append(result.Entries, domain.RankingEntry{
			Place:      query.Offset + i + 1,
			RankingRow: row,
		})
	}

	return result, nil
}

// GetPublicScore returns the stored public score of a public repo; nil means not graded yet.
func (s *RatingService) GetPublicScore(ctx context.Context, orgSlug, repoSlug string) (*int16, error) {
	orgSlug, repoSlug = strings.TrimSpace(orgSlug), strings.TrimSpace(repoSlug)
	if orgSlug == "" || repoSlug == "" {
		return nil, fmt.Errorf("%w: org_slug and repo_slug are required", domain.ErrInvalidArgument)
	}

	return s.repo.GetPublicScore(ctx, orgSlug, repoSlug)
}

// buildQuery applies defaults and validates the request. Returns the effective page number
// alongside the query, since the query itself only carries limit/offset.
func buildQuery(req domain.RankingRequest) (domain.RankingQuery, int, error) {
	page := req.Page
	if page < 0 {
		return domain.RankingQuery{}, 0, fmt.Errorf("%w: page must not be negative", domain.ErrInvalidArgument)
	}
	if page == 0 {
		page = 1
	}

	pageSize := req.PageSize
	if pageSize < 0 || pageSize > MaxPageSize {
		return domain.RankingQuery{}, 0, fmt.Errorf("%w: page_size must be between 0 and %d", domain.ErrInvalidArgument, MaxPageSize)
	}
	if pageSize == 0 {
		pageSize = DefaultPageSize
	}

	switch req.SortBy {
	case domain.SortByScore, domain.SortByLastActivity, domain.SortByLikes:
	default:
		return domain.RankingQuery{}, 0, fmt.Errorf("%w: unknown sort %q", domain.ErrInvalidArgument, req.SortBy)
	}

	return domain.RankingQuery{
		Language: strings.TrimSpace(req.Language),
		SortBy:   req.SortBy,
		Limit:    pageSize,
		Offset:   (page - 1) * pageSize,
	}, page, nil
}

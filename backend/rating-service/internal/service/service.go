package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
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

	total, err := s.repo.CountRanking(ctx, query.Language, query.HideLowConfidence)
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

// GetPublicSummary returns a public repo's stored grade with category scores decoded. A repo that
// has not been graded yet comes back with a nil score and no categories.
func (s *RatingService) GetPublicSummary(ctx context.Context, orgSlug, repoSlug string) (domain.PublicSummary, error) {
	orgSlug, repoSlug = strings.TrimSpace(orgSlug), strings.TrimSpace(repoSlug)
	if orgSlug == "" || repoSlug == "" {
		return domain.PublicSummary{}, fmt.Errorf("%w: org_slug and repo_slug are required", domain.ErrInvalidArgument)
	}

	row, err := s.repo.GetPublicSummary(ctx, orgSlug, repoSlug)
	if err != nil {
		return domain.PublicSummary{}, err
	}

	categories, err := decodeCategories(row.CategoryScores)
	if err != nil {
		return domain.PublicSummary{}, err
	}

	return domain.PublicSummary{
		Name:        row.Name,
		WebURL:      row.WebURL,
		Score:       row.Score,
		Categories:  categories,
		MissingData: row.MissingData,
		GradedAt:    row.GradedAt,
		Confidence:  row.Confidence,
	}, nil
}

// decodeCategories reads grade-service's category_scores JSON ({id: {no_data, score}}) into a
// list sorted by id. A no-data category keeps a nil score rather than becoming 0.
func decodeCategories(raw []byte) ([]domain.CategoryScore, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}

	var stored map[string]struct {
		NoData bool     `json:"no_data"`
		Score  *float64 `json:"score"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, fmt.Errorf("decode category scores: %w", err)
	}

	categories := make([]domain.CategoryScore, 0, len(stored))
	for id, category := range stored {
		entry := domain.CategoryScore{ID: id}
		if !category.NoData && category.Score != nil {
			score := int(math.Round(*category.Score))
			entry.Score = &score
		}
		categories = append(categories, entry)
	}
	sort.Slice(categories, func(i, j int) bool { return categories[i].ID < categories[j].ID })

	return categories, nil
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
		Language:          strings.TrimSpace(req.Language),
		SortBy:            req.SortBy,
		HideLowConfidence: req.HideLowConfidence,
		Limit:             pageSize,
		Offset:            (page - 1) * pageSize,
	}, page, nil
}

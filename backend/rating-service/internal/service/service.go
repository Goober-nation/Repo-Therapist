package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"rating-service/internal/domain"
)

const (
	DefaultPageSize = 20
	MaxPageSize     = 100

	// Default size-bucket limits in lines of code: small below 2000, large from 20000.
	DefaultSizeSmallLOC = 2000
	DefaultSizeLargeLOC = 20000
	// MaxTopicLength bounds the topic text of a ranking filter.
	MaxTopicLength = 64

	// DefaultDynamicsDays is the default window of the score and place change.
	DefaultDynamicsDays = 7
	// MaxHistoryPoints is how many of the latest history points GetScoreHistory returns.
	MaxHistoryPoints = 200
	// topN is how many top repos the explanation's per-category median is taken over, and
	// minMedianValues how many of them must have the category for a median to be shown.
	topN            = 10
	minMedianValues = 3
)

type RatingService struct {
	repo         domain.RankingRepository
	dynamicsDays int
	sizeSmallLOC int
	sizeLargeLOC int

	// analytics feeds the analytics tab and the export; nil until SetAnalytics.
	analytics     domain.AnalyticsRepository
	analyticsTTL  time.Duration
	cacheRecorder CacheRecorder
	cache         analyticsCache
}

// NewRatingService builds the service; dynamicsDays is the window of the score and place change
// in ranking pages (a value below 1 means DefaultDynamicsDays).
func NewRatingService(repo domain.RankingRepository, dynamicsDays int) *RatingService {
	if dynamicsDays < 1 {
		dynamicsDays = DefaultDynamicsDays
	}

	return &RatingService{
		repo:         repo,
		dynamicsDays: dynamicsDays,
		sizeSmallLOC: DefaultSizeSmallLOC,
		sizeLargeLOC: DefaultSizeLargeLOC,
	}
}

// SetSizeBuckets sets the limits (lines of code) between the small, medium and large size filters;
// a non-positive or inverted pair keeps the defaults.
func (s *RatingService) SetSizeBuckets(small, large int) {
	if small > 0 && large > small {
		s.sizeSmallLOC, s.sizeLargeLOC = small, large
	}
}

func (s *RatingService) GetRanking(ctx context.Context, req domain.RankingRequest) (domain.RankingPage, error) {
	query, page, err := buildQuery(req, s.dynamicsDays, s.sizeSmallLOC, s.sizeLargeLOC)
	if err != nil {
		return domain.RankingPage{}, err
	}

	total, err := s.repo.CountRanking(ctx, query.Language, query.HideLowConfidence, query.Extra)
	if err != nil {
		return domain.RankingPage{}, err
	}

	result := domain.RankingPage{
		Entries:    []domain.RankingEntry{},
		TotalCount: total,
		Page:       page,
		PageSize:   query.Limit,

		DynamicsDays: s.dynamicsDays,
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
		place := query.Offset + i + 1
		applyDynamics(&row, place, query.SortBy == domain.SortByScore)

		result.Entries = append(result.Entries, domain.RankingEntry{
			Place:      place,
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
func buildQuery(req domain.RankingRequest, dynamicsDays, sizeSmallLOC, sizeLargeLOC int) (domain.RankingQuery, int, error) {
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

	extra := req.Extra
	switch extra.Size {
	case "", "small", "medium", "large":
	default:
		return domain.RankingQuery{}, 0, fmt.Errorf("%w: size must be small, medium or large, got %q", domain.ErrInvalidArgument, extra.Size)
	}

	extra.Topic = strings.TrimSpace(extra.Topic)
	if utf8.RuneCountInString(extra.Topic) > MaxTopicLength {
		return domain.RankingQuery{}, 0, fmt.Errorf("%w: topic must be at most %d characters", domain.ErrInvalidArgument, MaxTopicLength)
	}

	extra.SmallLOC, extra.LargeLOC = sizeSmallLOC, sizeLargeLOC

	return domain.RankingQuery{
		Extra:             extra,
		Language:          strings.TrimSpace(req.Language),
		SortBy:            req.SortBy,
		HideLowConfidence: req.HideLowConfidence,
		Limit:             pageSize,
		Offset:            (page - 1) * pageSize,
		DynamicsDays:      dynamicsDays,
	}, page, nil
}

// applyDynamics turns the past score and place the repository found into the changes shown in the
// ranking. Without a past score the repo has no change at all ("new"); the place change needs the
// live place to be a score place, so it is only set when the list is sorted by score. A positive
// place change means places gained.
func applyDynamics(row *domain.RankingRow, place int, byScore bool) {
	if row.PastScore == nil || row.PublicScore == nil {
		return
	}

	scoreChange := int(*row.PublicScore) - int(*row.PastScore)
	row.ScoreChange = &scoreChange

	if byScore && row.PastPlace != nil {
		placeChange := *row.PastPlace - place
		row.PlaceChange = &placeChange
	}
}

// GetScoreHistory returns the latest stored public gradings of a public repo, oldest first, with
// category scores decoded. A repo without history comes back with no points.
func (s *RatingService) GetScoreHistory(ctx context.Context, orgSlug, repoSlug string) ([]domain.HistoryPoint, error) {
	orgSlug, repoSlug = strings.TrimSpace(orgSlug), strings.TrimSpace(repoSlug)
	if orgSlug == "" || repoSlug == "" {
		return nil, fmt.Errorf("%w: org_slug and repo_slug are required", domain.ErrInvalidArgument)
	}

	rows, err := s.repo.ListScoreHistory(ctx, orgSlug, repoSlug, MaxHistoryPoints)
	if err != nil {
		return nil, err
	}

	points := make([]domain.HistoryPoint, 0, len(rows))

	for _, row := range rows {
		categories, err := decodeCategories(row.CategoryScores)
		if err != nil {
			return nil, err
		}

		points = append(points, domain.HistoryPoint{
			GradedAt:      row.GradedAt,
			Score:         int(row.Score),
			Categories:    categories,
			LowConfidence: row.LowConfidence,
		})
	}

	return points, nil
}

// GetPositionExplanation explains a public repo's place in the default ranking. ErrNotFound is
// returned for an unknown or non-public repo; a public repo without a grade is Ranked=false.
func (s *RatingService) GetPositionExplanation(ctx context.Context, orgSlug, repoSlug string) (domain.PositionExplanation, error) {
	orgSlug, repoSlug = strings.TrimSpace(orgSlug), strings.TrimSpace(repoSlug)
	if orgSlug == "" || repoSlug == "" {
		return domain.PositionExplanation{}, fmt.Errorf("%w: org_slug and repo_slug are required", domain.ErrInvalidArgument)
	}

	rows, err := s.repo.ListRanked(ctx)
	if err != nil {
		return domain.PositionExplanation{}, err
	}

	explanation, found, err := explainPosition(rows, orgSlug, repoSlug)
	if err != nil {
		return domain.PositionExplanation{}, err
	}

	if !found {
		// Either unknown/non-public (ErrNotFound from here) or public but not graded yet.
		if _, err := s.repo.GetPublicScore(ctx, orgSlug, repoSlug); err != nil {
			return domain.PositionExplanation{}, err
		}

		return domain.PositionExplanation{Ranked: false}, nil
	}

	return explanation, nil
}

// explainPosition finds the repo in the ranking (best first) and compares it with its neighbours
// and, per category, with the median of the top repos. found is false when the repo is not in rows.
func explainPosition(rows []domain.RankedRow, orgSlug, repoSlug string) (domain.PositionExplanation, bool, error) {
	index := -1

	for i, row := range rows {
		if row.OrgSlug == orgSlug && row.Name == repoSlug {
			index = i

			break
		}
	}

	if index < 0 {
		return domain.PositionExplanation{}, false, nil
	}

	self := rows[index]
	result := domain.PositionExplanation{
		Ranked: true,
		Place:  index + 1,
		Total:  len(rows),
		Score:  self.Score,
	}

	if index > 0 {
		above := rows[index-1]
		result.Above = &domain.Neighbour{Place: index, OrgSlug: above.OrgSlug, Name: above.Name, Score: above.Score, Points: above.Score - self.Score}
	}

	if index+1 < len(rows) {
		below := rows[index+1]
		result.Below = &domain.Neighbour{Place: index + 2, OrgSlug: below.OrgSlug, Name: below.Name, Score: below.Score, Points: self.Score - below.Score}
	}

	own, err := decodeCategories(self.CategoryScores)
	if err != nil {
		return domain.PositionExplanation{}, false, err
	}

	top := rows
	if len(top) > topN {
		top = top[:topN]
	}

	values := map[string][]int{}

	for _, row := range top {
		categories, err := decodeCategories(row.CategoryScores)
		if err != nil {
			return domain.PositionExplanation{}, false, err
		}

		for _, category := range categories {
			if category.Score != nil {
				values[category.ID] = append(values[category.ID], *category.Score)
			}
		}
	}

	worstGap := 0

	for _, category := range own {
		if category.Score == nil {
			continue
		}

		comparison := domain.CategoryComparison{ID: category.ID, Score: *category.Score}

		if scores := values[category.ID]; len(scores) >= minMedianValues {
			median := medianInt(scores)
			gap := *category.Score - median
			comparison.Top10Median, comparison.Gap = &median, &gap

			// Strictly below the top-10 median; ties between categories keep the first by id.
			if gap < worstGap {
				worstGap = gap
				result.WeakestCategory = category.ID
			}
		}

		result.Categories = append(result.Categories, comparison)
	}

	return result, true, nil
}

// medianInt is the median of values rounded to the nearest integer (halves round up).
func medianInt(values []int) int {
	sorted := append([]int(nil), values...)
	sort.Ints(sorted)

	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}

	return int(math.Round(float64(sorted[mid-1]+sorted[mid]) / 2))
}

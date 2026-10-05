package rpc

import (
	"context"
	"errors"
	"fmt"
	"log"

	"rating-service/internal/domain"
	pb "rating-service/internal/transport/gen"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type RatingService interface {
	GetRanking(ctx context.Context, req domain.RankingRequest) (domain.RankingPage, error)
	GetPublicScore(ctx context.Context, orgSlug, repoSlug string) (*int16, error)
	GetPublicSummary(ctx context.Context, orgSlug, repoSlug string) (domain.PublicSummary, error)
	GetScoreHistory(ctx context.Context, orgSlug, repoSlug string) ([]domain.HistoryPoint, error)
	GetPositionExplanation(ctx context.Context, orgSlug, repoSlug string) (domain.PositionExplanation, error)
}

type Server struct {
	pb.UnimplementedRatingServer

	service RatingService
}

func NewServer(service RatingService) *Server {
	return &Server{
		service: service,
	}
}

func (s *Server) GetRanking(ctx context.Context, req *pb.GetRankingRequest) (*pb.GetRankingResponse, error) {
	sortBy, err := sortFromProto(req.GetSortBy())
	if err != nil {
		return nil, mapError(err)
	}

	page, err := s.service.GetRanking(ctx, domain.RankingRequest{
		Page:              int(req.GetPage()),
		PageSize:          int(req.GetPageSize()),
		Language:          req.GetLanguage(),
		SortBy:            sortBy,
		HideLowConfidence: req.GetHideLowConfidence(),
		Extra: domain.RankingExtras{
			Size:              req.GetSize(),
			HasCI:             req.HasCi,
			HasSecurityPolicy: req.HasSecurityPolicy,
			Topic:             req.GetTopic(),
		},
	})
	if err != nil {
		return nil, mapError(err)
	}

	entries := make([]*pb.RankingEntry, 0, len(page.Entries))
	for _, entry := range page.Entries {
		entries = append(entries, entryToProto(entry))
	}

	return &pb.GetRankingResponse{
		Entries:    entries,
		TotalCount: int32(page.TotalCount),
		Page:       int32(page.Page),
		PageSize:   int32(page.PageSize),

		DynamicsDays: int32(page.DynamicsDays),
	}, nil
}

func (s *Server) GetPublicScore(ctx context.Context, req *pb.GetPublicScoreRequest) (*pb.GetPublicScoreResponse, error) {
	score, err := s.service.GetPublicScore(ctx, req.GetOrgSlug(), req.GetRepoSlug())
	if err != nil {
		return nil, mapError(err)
	}

	result := &pb.GetPublicScoreResponse{}
	if score != nil {
		value := int32(*score)
		result.PublicScore = &value
	}

	return result, nil
}

func (s *Server) GetPublicSummary(ctx context.Context, req *pb.GetPublicSummaryRequest) (*pb.GetPublicSummaryResponse, error) {
	summary, err := s.service.GetPublicSummary(ctx, req.GetOrgSlug(), req.GetRepoSlug())
	if err != nil {
		return nil, mapError(err)
	}

	result := &pb.GetPublicSummaryResponse{
		Name:        summary.Name,
		WebUrl:      summary.WebURL,
		Categories:  make([]*pb.CategoryScore, 0, len(summary.Categories)),
		MissingData: summary.MissingData,
	}

	if summary.Score != nil {
		score := int32(*summary.Score)
		result.PublicScore = &score
	}

	for _, category := range summary.Categories {
		entry := &pb.CategoryScore{Id: category.ID}
		if category.Score != nil {
			score := int32(*category.Score)
			entry.Score = &score
		}
		result.Categories = append(result.Categories, entry)
	}

	if summary.GradedAt != nil {
		result.GradedAt = timestamppb.New(*summary.GradedAt)
	}

	if summary.Confidence != nil {
		result.LowConfidence = &summary.Confidence.Low
		result.ConfidenceReasons = reasonsToProto(summary.Confidence.Reasons)
	}

	return result, nil
}

func (s *Server) GetScoreHistory(ctx context.Context, req *pb.GetScoreHistoryRequest) (*pb.GetScoreHistoryResponse, error) {
	points, err := s.service.GetScoreHistory(ctx, req.GetOrgSlug(), req.GetRepoSlug())
	if err != nil {
		return nil, mapError(err)
	}

	result := &pb.GetScoreHistoryResponse{Points: make([]*pb.HistoryPoint, 0, len(points))}

	for _, point := range points {
		entry := &pb.HistoryPoint{
			GradedAt:      timestamppb.New(point.GradedAt),
			Score:         int32(point.Score),
			Categories:    categoriesToProto(point.Categories),
			LowConfidence: point.LowConfidence,
		}
		result.Points = append(result.Points, entry)
	}

	return result, nil
}

func (s *Server) GetPositionExplanation(ctx context.Context, req *pb.GetPositionExplanationRequest) (*pb.GetPositionExplanationResponse, error) {
	explanation, err := s.service.GetPositionExplanation(ctx, req.GetOrgSlug(), req.GetRepoSlug())
	if err != nil {
		return nil, mapError(err)
	}

	result := &pb.GetPositionExplanationResponse{Ranked: explanation.Ranked}
	if !explanation.Ranked {
		return result, nil
	}

	result.Place = int32(explanation.Place)
	result.Total = int32(explanation.Total)
	result.Score = int32(explanation.Score)
	result.Above = neighbourToProto(explanation.Above)
	result.Below = neighbourToProto(explanation.Below)
	result.WeakestCategory = explanation.WeakestCategory

	result.Categories = make([]*pb.CategoryComparison, 0, len(explanation.Categories))

	for _, category := range explanation.Categories {
		entry := &pb.CategoryComparison{Id: category.ID, Score: int32(category.Score)}

		if category.Top10Median != nil {
			median, gap := int32(*category.Top10Median), int32(*category.Gap)
			entry.Top10Median, entry.Gap = &median, &gap
		}

		result.Categories = append(result.Categories, entry)
	}

	return result, nil
}

func neighbourToProto(n *domain.Neighbour) *pb.Neighbour {
	if n == nil {
		return nil
	}

	return &pb.Neighbour{
		Place:   int32(n.Place),
		OrgSlug: n.OrgSlug,
		Name:    n.Name,
		Score:   int32(n.Score),
		Points:  int32(n.Points),
	}
}

func categoriesToProto(categories []domain.CategoryScore) []*pb.CategoryScore {
	result := make([]*pb.CategoryScore, 0, len(categories))

	for _, category := range categories {
		entry := &pb.CategoryScore{Id: category.ID}
		if category.Score != nil {
			score := int32(*category.Score)
			entry.Score = &score
		}

		result = append(result, entry)
	}

	return result
}

func sortFromProto(sortBy pb.SortBy) (domain.SortBy, error) {
	switch sortBy {
	case pb.SortBy_SORT_BY_SCORE:
		return domain.SortByScore, nil
	case pb.SortBy_SORT_BY_LAST_ACTIVITY:
		return domain.SortByLastActivity, nil
	case pb.SortBy_SORT_BY_LIKES:
		return domain.SortByLikes, nil
	default:
		return "", fmt.Errorf("%w: unknown sort_by %d", domain.ErrInvalidArgument, sortBy)
	}
}

func entryToProto(entry domain.RankingEntry) *pb.RankingEntry {
	result := &pb.RankingEntry{
		Place:           int32(entry.Place),
		SourcecraftUuid: entry.SourcecraftUUID,
		Name:            entry.Name,
		WebUrl:          entry.WebURL,
		Likes:           entry.Likes,
		Language: &pb.Language{
			Name:  entry.Language.Name,
			Color: entry.Language.Color,
		},
	}

	if entry.PublicScore != nil {
		score := int32(*entry.PublicScore)
		result.PublicScore = &score
	}

	if entry.LastActivityAt != nil {
		result.LastActivityAt = timestamppb.New(*entry.LastActivityAt)
	}

	if entry.Confidence != nil {
		result.LowConfidence = &entry.Confidence.Low
		result.ConfidenceReasons = reasonsToProto(entry.Confidence.Reasons)
	}

	result.GradeCount = int32(entry.GradeCount)

	if entry.ScoreChange != nil {
		change := int32(*entry.ScoreChange)
		result.ScoreChange = &change
	}

	if entry.PlaceChange != nil {
		change := int32(*entry.PlaceChange)
		result.PlaceChange = &change
	}

	return result
}

func reasonsToProto(reasons []domain.ConfidenceReason) []*pb.ConfidenceReason {
	result := make([]*pb.ConfidenceReason, 0, len(reasons))
	for _, reason := range reasons {
		result = append(result, &pb.ConfidenceReason{Code: reason.Code, Text: reason.Text})
	}

	return result
}

func mapError(err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidArgument):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, "repository not found")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "deadline exceeded")
	default:
		log.Printf("rating rpc: %v", err)
		return status.Error(codes.Internal, "internal error")
	}
}

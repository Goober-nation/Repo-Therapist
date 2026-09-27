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
		Page:     int(req.GetPage()),
		PageSize: int(req.GetPageSize()),
		Language: req.GetLanguage(),
		SortBy:   sortBy,
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
	}, nil
}

func sortFromProto(sortBy pb.SortBy) (domain.SortBy, error) {
	switch sortBy {
	case pb.SortBy_SORT_BY_UNSPECIFIED, pb.SortBy_SORT_BY_SCORE:
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
		Language:        entry.Language,
	}

	if entry.PublicScore != nil {
		score := int32(*entry.PublicScore)
		result.PublicScore = &score
	}

	if entry.LastActivityAt != nil {
		result.LastActivityAt = timestamppb.New(*entry.LastActivityAt)
	}

	return result
}

func mapError(err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidArgument):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "deadline exceeded")
	default:
		log.Printf("get ranking: %v", err)
		return status.Error(codes.Internal, "internal error")
	}
}

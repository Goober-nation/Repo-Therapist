package rpc

import (
	"context"

	"sourcecraft-wrapper-service/internal/domain"
	pb "sourcecraft-wrapper-service/internal/transport/gen"
)

func (s *ServerRPC) GetPullRequests(
	ctx context.Context,
	req *pb.GetPullRequestsRequest,
) (*pb.GetPullRequestsResponse, error) {
	prs, err := s.service.GetPullRequests(ctx, req.GetRepoId(), int(req.GetLimit()))
	if err != nil {
		return nil, mapError(err)
	}

	result := make([]*pb.PullRequest, 0, len(prs.PullRequests))
	for _, pr := range prs.PullRequests {
		result = append(result, &pb.PullRequest{
			Id:        pr.ID,
			Slug:      pr.Slug,
			Status:    pr.Status,
			Author:    mapPullRequestUser(pr.Author),
			CreatedAt: mapTimestamp(pr.CreatedAt),
			UpdatedAt: mapTimestamp(pr.UpdatedAt),
		})
	}

	return &pb.GetPullRequestsResponse{PullRequests: result}, nil
}

func (s *ServerRPC) GetPullRequestActivity(
	ctx context.Context,
	req *pb.GetPullRequestActivityRequest,
) (*pb.GetPullRequestActivityResponse, error) {
	activity, err := s.service.GetPullRequestActivity(ctx, req.GetPullRequestId())
	if err != nil {
		return nil, mapError(err)
	}

	resp := &pb.GetPullRequestActivityResponse{}
	for _, reviewer := range activity.Reviewers {
		resp.Reviewers = append(resp.Reviewers, &pb.PullRequestReviewer{
			User:           mapPullRequestUser(reviewer.User),
			ReviewDecision: reviewer.ReviewDecision,
			CreatedAt:      mapTimestamp(reviewer.CreatedAt),
		})
	}

	for _, comment := range activity.Comments {
		resp.Comments = append(resp.Comments, &pb.PullRequestComment{
			Author:      mapPullRequestUser(comment.Author),
			CreatedAt:   mapTimestamp(comment.CreatedAt),
			Type:        comment.Type,
			IsDeleted:   comment.IsDeleted,
			IsPublished: comment.IsPublished,
		})
	}

	return resp, nil
}

func mapPullRequestUser(user domain.PullRequestUser) *pb.PullRequestUser {
	return &pb.PullRequestUser{Id: user.ID, Slug: user.Slug}
}

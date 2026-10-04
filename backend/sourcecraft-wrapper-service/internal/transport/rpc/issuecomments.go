package rpc

import (
	"context"

	pb "sourcecraft-wrapper-service/internal/transport/gen"
)

func (s *ServerRPC) GetIssueComments(
	ctx context.Context,
	req *pb.GetIssueCommentsRequest,
) (*pb.GetIssueCommentsResponse, error) {
	comments, err := s.service.GetIssueComments(ctx, req.GetIssueId())
	if err != nil {
		return nil, mapError(err)
	}

	resp := &pb.GetIssueCommentsResponse{Comments: make([]*pb.IssueCommentMeta, 0, len(comments))}
	for _, c := range comments {
		resp.Comments = append(resp.Comments, &pb.IssueCommentMeta{
			Author:    &pb.IssueUser{Id: c.Author.ID, Slug: c.Author.Slug},
			CreatedAt: mapTimestamp(c.CreatedAt),
		})
	}

	return resp, nil
}

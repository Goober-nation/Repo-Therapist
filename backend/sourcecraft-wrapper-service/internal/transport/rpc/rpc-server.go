package rpc

import (
	"context"
	"errors"
	"log"
	"sourcecraft-wrapper-service/internal/domain"
	"sourcecraft-wrapper-service/internal/service"
	pb "sourcecraft-wrapper-service/internal/transport/gen"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ServerRPC struct {
	pb.UnimplementedCodeHealthServer
	service service.SCService
}

func NewServerRPC(serv service.SCService) *ServerRPC {
	return &ServerRPC{
		service: serv,
	}
}
func (s *ServerRPC) GetRepoIDBySlugs(ctx context.Context, req *pb.GetRepoIDBySlugsRequest) (*pb.GetRepoIDBySlugsResponse, error) {
	repoID, err := s.service.GetRepoIdBySlugs(
		ctx,
		req.GetRepoSlug(),
		req.GetOrgSlug(),
	)
	if err != nil {
		return nil, mapError(err)
	}

	return &pb.GetRepoIDBySlugsResponse{
		RepoId: repoID,
	}, nil
}

func (s *ServerRPC) GetGitURL(
	ctx context.Context,
	req *pb.GetGitURLRequest,
) (*pb.GetGitURLResponse, error) {
	cloneURL, err := s.service.GetGitUrl(
		ctx,
		req.GetRepoId(),
	)
	if err != nil {
		return nil, mapError(err)
	}

	return mapCloneURL(cloneURL), nil
}

func (s *ServerRPC) GetCiCDRuns(
	ctx context.Context,
	req *pb.GetCiCDRunsRequest,
) (*pb.GetCiCDRunsResponse, error) {
	runs, err := s.service.GetCiCdRuns(
		ctx,
		req.GetRepoId(),
	)
	if err != nil {
		return nil, mapError(err)
	}

	return &pb.GetCiCDRunsResponse{
		Runs:          mapRuns(runs.Runs),
		NextPageToken: runs.NextPageToken,
	}, nil
}

func (s *ServerRPC) GetIssues(
	ctx context.Context,
	req *pb.GetIssuesRequest,
) (*pb.GetIssuesResponse, error) {
	issues, err := s.service.GetIssues(
		ctx,
		req.GetRepoId(),
	)
	if err != nil {
		return nil, mapError(err)
	}

	return &pb.GetIssuesResponse{
		Issues:        mapIssues(issues.Issues),
		NextPageToken: issues.NextPageToken,
	}, nil
}

func (s *ServerRPC) GetMilestones(
	ctx context.Context,
	req *pb.GetMilestonesRequest,
) (*pb.GetMilestonesResponse, error) {
	milestones, err := s.service.GetMilestones(
		ctx,
		req.GetRepoId(),
	)
	if err != nil {
		return nil, mapError(err)
	}

	return &pb.GetMilestonesResponse{
		Items:         mapMilestones(milestones.Items),
		NextPageToken: milestones.NextPageToken,
	}, nil
}

func (s *ServerRPC) ListAllPublicRepoIDs(
	ctx context.Context,
	req *pb.ListAllPublicRepoIDsRequest,
) (*pb.ListAllPublicRepoIDsResponse, error) {
	repoIDs, err := s.service.ListAllPublicReposId(ctx)
	if err != nil {
		return nil, mapError(err)
	}

	return &pb.ListAllPublicRepoIDsResponse{
		RepoIds: repoIDs,
	}, nil
}

func mapError(err error) error {
	log.Println(err)
	switch {
	case errors.Is(err, domain.ErrRepositoryNotFound):
		return status.Error(codes.NotFound, "repository not found")
	case errors.Is(err, domain.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, "missing or invalid authorization token")
	case errors.Is(err, domain.ErrNoAccess):
		return status.Error(codes.PermissionDenied, "access denied")

	case errors.Is(err, domain.ErrExternalAPI):
		return status.Error(codes.Internal, "external API error")

	case errors.Is(err, domain.ErrInternalError):
		return status.Error(codes.Internal, "internal error")
	default:
		return status.Error(codes.Internal, "internal error")
	}
}

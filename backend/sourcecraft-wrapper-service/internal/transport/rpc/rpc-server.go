package rpc

import (
	"context"
	"errors"
	"log"
	"sourcecraft-wrapper-service/internal/domain"
	"sourcecraft-wrapper-service/internal/service"
	pb "sourcecraft-wrapper-service/internal/transport/gen"
	"time"

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
func (s *ServerRPC) GetRepositoryBySlugs(
	ctx context.Context,
	req *pb.GetRepositoryBySlugsRequest,
) (*pb.GetRepositoryBySlugsResponse, error) {
	repo, err := s.service.GetRepositoryBySlugs(
		ctx,
		req.GetRepoSlug(),
		req.GetOrgSlug(),
	)
	if err != nil {
		return nil, mapError(err)
	}

	reactionCounts := make([]*pb.ReactionCount, 0, len(repo.Rating.ReactionCounts))

	for _, reaction := range repo.Rating.ReactionCounts {
		reactionCounts = append(
			reactionCounts,
			&pb.ReactionCount{
				Type:  reaction.Type,
				Count: reaction.Count,
			},
		)
	}

	var lastActivityAt string
	if repo.LastActivity != nil {
		lastActivityAt = repo.LastActivity.Format(time.RFC3339Nano)
	}

	return &pb.GetRepositoryBySlugsResponse{
		Id:             repo.ID,
		Name:           repo.Name,
		Slug:           repo.Slug,
		Description:    repo.Description,
		WebUrl:         repo.WebURL,
		Visibility:     repo.Visibility,
		LastActivityAt: lastActivityAt,

		Organization: &pb.Organization{
			Id:   repo.Organization.ID,
			Slug: repo.Organization.Slug,
		},

		Logo: &pb.Logo{
			Url: repo.Logo.URL,
		},

		Rating: &pb.Rating{
			Value:          repo.Rating.Value,
			Percentile:     repo.Rating.Percentile,
			ReactionCounts: reactionCounts,
		},
		Language: &pb.Language{
			Name:  repo.Language.Name,
			Color: repo.Language.Color,
		},
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

func (s *ServerRPC) ListAllPublicRepos(
	ctx context.Context,
	req *pb.ListAllPublicReposRequest,
) (*pb.ListAllPublicReposResponse, error) {
	repos, err := s.service.ListAllPublicRepos(ctx)
	if err != nil {
		return nil, mapError(err)
	}

	return &pb.ListAllPublicReposResponse{
		Repos: mapRepositories(repos),
	}, nil
}

func (s *ServerRPC) GetMyRepos(
	ctx context.Context,
	req *pb.GetMyReposRequest,
) (*pb.GetMyReposResponse, error) {
	repos, err := s.service.GetMyRepos(ctx)
	if err != nil {
		return nil, mapError(err)
	}

	return &pb.GetMyReposResponse{
		Repositories:  mapRepositories(repos.Repositories),
		NextPageToken: repos.NextPageToken,
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

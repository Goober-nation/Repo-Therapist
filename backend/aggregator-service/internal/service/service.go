package service

import (
	"aggregator-service/internal/domain"
	pb "aggregator-service/internal/transport/rpc/gen"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AggregatorService struct {
	ReposRepo  domain.RepoRepository
	GradesRepo domain.GradesRepository
	LLMRepo    domain.LLMRepository
	ticker     time.Ticker
	clientRPC  pb.CodeHealthClient
}

func NewAggregatorService(reposRepo domain.RepoRepository, gradesRepo domain.GradesRepository,
	llmRepo domain.LLMRepository, clientRPC pb.CodeHealthClient, ) *AggregatorService {
	return &AggregatorService{
		ReposRepo:  reposRepo,
		GradesRepo: gradesRepo,
		LLMRepo:    llmRepo,
		ticker: time.Ticker{
			C:
		}
		clientRPC:  clientRPC,
	}
}

func (as *AggregatorService) StartGrading(ctx context.Context, orgSlug, repoSlug string) error {
	response, err := as.clientRPC.GetRepoIDBySlugs(
		ctx,
		&pb.GetRepoIDBySlugsRequest{
			OrgSlug:  orgSlug,
			RepoSlug: repoSlug,
		},
	)
	if err != nil {
		code := status.Code(err)
		if code == codes.PermissionDenied {
			return domain.ErrNoAccess
		}
		return fmt.Errorf(
			"%w: wrapper returned %s: %v",
			domain.ErrGradingFailed,
			code,
			err,
		)
	}
	repoID, err := uuid.Parse(response.RepoId)
	if err != nil {
		return fmt.Errorf(
			"%w: UUID parsing: %v",
			domain.ErrGradingFailed,
			err,
		)
	}

	repo, err := as.ReposRepo.GetRepositoryByID(ctx, repoID)
	grade err
	if repo.LastActivityAt < {}


}



func needsGrading(repo , grade domain.RepoGrade) bool {
	if grade.GradedAt == nil {
		return true
	}

	if repo.LastActivityAt == nil {
		return false
	}

	return repo.LastActivityAt.After(*grade.GradedAt)
}


// написать мапперы, кафка консумер и продусер, дописать методы и сделать рпс сервер
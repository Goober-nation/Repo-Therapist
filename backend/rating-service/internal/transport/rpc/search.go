package rpc

import (
	"context"

	"rating-service/internal/domain"
	pb "rating-service/internal/transport/gen"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) SuggestCatalog(ctx context.Context, req *pb.SuggestCatalogRequest) (*pb.SuggestCatalogResponse, error) {
	suggestions, err := s.service.SuggestCatalog(ctx, req.GetQuery())
	if err != nil {
		return nil, mapError(err)
	}

	return &pb.SuggestCatalogResponse{
		Repos: catalogReposToProto(suggestions.Repos),
		Orgs:  catalogOrgsToProto(suggestions.Orgs),
	}, nil
}

func (s *Server) SearchCatalogRepos(ctx context.Context, req *pb.SearchCatalogReposRequest) (*pb.SearchCatalogReposResponse, error) {
	page, err := s.service.SearchCatalogRepos(ctx, req.GetQuery(), req.GetOrg(), int(req.GetPage()), int(req.GetPageSize()))
	if err != nil {
		return nil, mapError(err)
	}

	return &pb.SearchCatalogReposResponse{
		Repos:      catalogReposToProto(page.Repos),
		TotalCount: int32(page.Total),
		Page:       int32(page.Page),
		PageSize:   int32(page.PageSize),
	}, nil
}

func (s *Server) SearchCatalogOrgs(ctx context.Context, req *pb.SearchCatalogOrgsRequest) (*pb.SearchCatalogOrgsResponse, error) {
	page, err := s.service.SearchCatalogOrgs(ctx, req.GetQuery(), int(req.GetPage()), int(req.GetPageSize()))
	if err != nil {
		return nil, mapError(err)
	}

	return &pb.SearchCatalogOrgsResponse{
		Orgs:       catalogOrgsToProto(page.Orgs),
		TotalCount: int32(page.Total),
		Page:       int32(page.Page),
		PageSize:   int32(page.PageSize),
	}, nil
}

func catalogReposToProto(repos []domain.CatalogRepo) []*pb.CatalogRepo {
	result := make([]*pb.CatalogRepo, 0, len(repos))

	for _, repo := range repos {
		entry := &pb.CatalogRepo{
			SourcecraftUuid: repo.SourcecraftUUID,
			OrgSlug:         repo.OrgSlug,
			RepoSlug:        repo.RepoSlug,
			Description:     repo.Description,
			WebUrl:          repo.WebURL,
			Language:        &pb.Language{Name: repo.Language.Name, Color: repo.Language.Color},
			Likes:           repo.Likes,
		}

		if repo.LastActivityAt != nil {
			entry.LastActivityAt = timestamppb.New(*repo.LastActivityAt)
		}

		if repo.PublicScore != nil {
			score := int32(*repo.PublicScore)
			entry.PublicScore = &score
		}

		result = append(result, entry)
	}

	return result
}

func catalogOrgsToProto(orgs []domain.CatalogOrg) []*pb.CatalogOrg {
	result := make([]*pb.CatalogOrg, 0, len(orgs))

	for _, org := range orgs {
		result = append(result, &pb.CatalogOrg{Slug: org.Slug, RepoCount: int32(org.RepoCount)})
	}

	return result
}

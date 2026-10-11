package service

import (
	"context"

	"gateway-service/internal/domain"
	"gateway-service/internal/transport/rpc/gen/agg"
)

func catalogRefreshFromProto(r *agg.CatalogRefresh) *domain.CatalogRefresh {
	if r == nil {
		return nil
	}

	out := &domain.CatalogRefresh{
		ID:        r.GetId(),
		StartedAt: r.GetStartedAt().AsTime(),
		Status:    r.GetStatus(),
		Trigger:   r.GetTrigger(),
		ActorHash: r.GetActorHash(),
		Pages:     r.GetPages(),
		Repos:     r.GetRepos(),
		Error:     r.GetError(),
	}

	if r.GetFinishedAt() != nil {
		finished := r.GetFinishedAt().AsTime()
		out.FinishedAt = &finished
	}

	return out
}

// GetCatalogStatus returns the public repo catalog's size, age and refreshes.
func (s *GatewayService) GetCatalogStatus(ctx context.Context) (domain.CatalogStatus, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, s.aggregatorRPCTimeout())
	defer cancel()

	resp, err := s.aggregatorClient.GetCatalogStatus(rpcCtx, &agg.GetCatalogStatusRequest{})
	if err != nil {
		return domain.CatalogStatus{}, batchError("get catalog status", err)
	}

	status := domain.CatalogStatus{
		Size:          resp.GetSize(),
		Current:       catalogRefreshFromProto(resp.GetCurrent()),
		LastCompleted: catalogRefreshFromProto(resp.GetLastCompleted()),
	}

	if resp.GetUpdatedAt() != nil {
		updated := resp.GetUpdatedAt().AsTime()
		status.UpdatedAt = &updated
	}

	return status, nil
}

// RefreshCatalog starts a catalog refresh under the admin's actor hash.
func (s *GatewayService) RefreshCatalog(ctx context.Context, actorHash string) (domain.CatalogRefresh, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, s.aggregatorRPCTimeout())
	defer cancel()

	resp, err := s.aggregatorClient.RefreshCatalog(rpcCtx, &agg.RefreshCatalogRequest{ActorHash: actorHash})
	if err != nil {
		return domain.CatalogRefresh{}, batchError("refresh catalog", err)
	}

	return *catalogRefreshFromProto(resp.GetRefresh()), nil
}

// CancelCatalogRefresh stops the running catalog refresh.
func (s *GatewayService) CancelCatalogRefresh(ctx context.Context, actorHash string) (domain.CatalogRefresh, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, s.aggregatorRPCTimeout())
	defer cancel()

	resp, err := s.aggregatorClient.CancelCatalogRefresh(rpcCtx, &agg.CancelCatalogRefreshRequest{ActorHash: actorHash})
	if err != nil {
		return domain.CatalogRefresh{}, batchError("cancel catalog refresh", err)
	}

	return *catalogRefreshFromProto(resp.GetRefresh()), nil
}

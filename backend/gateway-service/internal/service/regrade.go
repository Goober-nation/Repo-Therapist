package service

import (
	"context"
	"fmt"
	"time"

	"gateway-service/internal/domain"
	"gateway-service/internal/transport/rpc/gen/agg"

	"google.golang.org/grpc/status"
)

// Reasons the Aggregator gives for a refused batch request; anything else carries no reason.
var batchReasons = map[string]struct{}{
	"nothing_selected": {}, "too_many": {}, "invalid_filter": {},
	"batch_active": {}, "bad_state": {}, "batch_not_found": {},
}

// batchError is rpcError plus the Aggregator's reason word, when it sent a known one.
func batchError(op string, err error) error {
	wrapped := rpcError(op, err)

	if reason := status.Convert(err).Message(); reason != "" {
		if _, known := batchReasons[reason]; known {
			return domain.ReasonError{Reason: reason, Err: wrapped}
		}
	}

	return wrapped
}

func filterToProto(f domain.BatchFilter) *agg.BatchFilter {
	return &agg.BatchFilter{
		Languages:     f.Languages,
		MinScore:      f.MinScore,
		MaxScore:      f.MaxScore,
		OlderThanDays: f.OlderThanDays,
		Repos:         f.Repos,
		TopRated:      f.TopRated,
		All:           f.All,
		Force:         f.Force,
	}
}

func filterFromProto(f *agg.BatchFilter) domain.BatchFilter {
	if f == nil {
		return domain.BatchFilter{}
	}

	out := domain.BatchFilter{Languages: f.GetLanguages(), Repos: f.GetRepos(), All: f.GetAll(), Force: f.GetForce()}

	if f.TopRated != nil {
		v := f.GetTopRated()
		out.TopRated = &v
	}

	if f.MinScore != nil {
		v := f.GetMinScore()
		out.MinScore = &v
	}

	if f.MaxScore != nil {
		v := f.GetMaxScore()
		out.MaxScore = &v
	}

	if f.OlderThanDays != nil {
		v := f.GetOlderThanDays()
		out.OlderThanDays = &v
	}

	return out
}

func batchFromProto(b *agg.RegradeBatch) domain.RegradeBatch {
	out := domain.RegradeBatch{
		ID:        b.GetId(),
		CreatedAt: b.GetCreatedAt().AsTime(),
		Status:    b.GetStatus(),
		ActorHash: b.GetActorHash(),
		Filter:    filterFromProto(b.GetFilter()),
		Total:     b.GetTotal(),
		Pending:   b.GetPending(),
		Running:   b.GetRunning(),
		OK:        b.GetOk(),
		Failed:    b.GetFailed(),
		Skipped:   b.GetSkipped(),
	}

	if b.GetFinishedAt() != nil {
		finished := b.GetFinishedAt().AsTime()
		out.FinishedAt = &finished
	}

	return out
}

// topRatedSelectTimeout is the least a preview or start with a top-N criterion gets: the Aggregator
// pages SourceCraft's rating-ordered listing live (about 0.4 s a page, up to 50 pages). Kept under
// the default HTTP_WRITE_TIMEOUT (30s) so the admin gets the answer, not a dropped connection.
const topRatedSelectTimeout = 25 * time.Second

func (s *GatewayService) batchSelectTimeout(filter domain.BatchFilter) time.Duration {
	if filter.TopRated != nil && s.aggregatorTimeout < topRatedSelectTimeout {
		return topRatedSelectTimeout
	}

	return s.aggregatorTimeout
}

// PreviewRegradeBatch is the dry run: what the filter would regrade, without starting anything.
func (s *GatewayService) PreviewRegradeBatch(ctx context.Context, filter domain.BatchFilter) (domain.BatchPreview, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, s.batchSelectTimeout(filter))
	defer cancel()

	resp, err := s.aggregatorClient.PreviewRegradeBatch(rpcCtx, &agg.PreviewRegradeBatchRequest{Filter: filterToProto(filter)})
	if err != nil {
		return domain.BatchPreview{}, batchError("preview regrade batch", err)
	}

	return domain.BatchPreview{
		Count:           resp.GetCount(),
		Capped:          resp.GetCapped(),
		Sample:          resp.GetSample(),
		Unmatched:       resp.GetUnmatched(),
		EstimateSeconds: resp.GetEstimateSeconds(),
		Inactive:        resp.GetInactive(),
	}, nil
}

// StartRegradeBatch starts a batch for the filter under the admin's actor hash.
func (s *GatewayService) StartRegradeBatch(ctx context.Context, actorHash string, filter domain.BatchFilter) (domain.RegradeBatch, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, s.batchSelectTimeout(filter))
	defer cancel()

	resp, err := s.aggregatorClient.StartRegradeBatch(rpcCtx, &agg.StartRegradeBatchRequest{
		Filter: filterToProto(filter), ActorHash: actorHash,
	})
	if err != nil {
		return domain.RegradeBatch{}, batchError("start regrade batch", err)
	}

	return batchFromProto(resp.GetBatch()), nil
}

// ControlRegradeBatch pauses, resumes or cancels a batch.
func (s *GatewayService) ControlRegradeBatch(ctx context.Context, actorHash string, id int64, action string) (domain.RegradeBatch, error) {
	switch action {
	case domain.BatchActionPause, domain.BatchActionResume, domain.BatchActionCancel:
	default:
		return domain.RegradeBatch{}, fmt.Errorf("%w: unknown batch action", domain.ErrInvalidRequest)
	}

	rpcCtx, cancel := context.WithTimeout(ctx, s.aggregatorTimeout)
	defer cancel()

	resp, err := s.aggregatorClient.ControlRegradeBatch(rpcCtx, &agg.ControlRegradeBatchRequest{
		BatchId: id, Action: action, ActorHash: actorHash,
	})
	if err != nil {
		return domain.RegradeBatch{}, batchError("control regrade batch", err)
	}

	return batchFromProto(resp.GetBatch()), nil
}

// GetRegradeBatch returns a batch and its failed repos; id 0 means the active batch, or the latest.
func (s *GatewayService) GetRegradeBatch(ctx context.Context, id int64) (domain.RegradeBatchDetail, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, s.aggregatorTimeout)
	defer cancel()

	resp, err := s.aggregatorClient.GetRegradeBatch(rpcCtx, &agg.GetRegradeBatchRequest{BatchId: id})
	if err != nil {
		return domain.RegradeBatchDetail{}, batchError("get regrade batch", err)
	}

	detail := domain.RegradeBatchDetail{Batch: batchFromProto(resp.GetBatch())}

	for _, f := range resp.GetFailures() {
		detail.Failures = append(detail.Failures, domain.RegradeBatchFailure{
			OrgSlug: f.GetOrgSlug(), RepoSlug: f.GetRepoSlug(), Error: f.GetError(),
		})
	}

	return detail, nil
}

// ListRegradeBatches returns the most recent batches, newest first.
func (s *GatewayService) ListRegradeBatches(ctx context.Context, limit int32) ([]domain.RegradeBatch, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, s.aggregatorTimeout)
	defer cancel()

	resp, err := s.aggregatorClient.ListRegradeBatches(rpcCtx, &agg.ListRegradeBatchesRequest{Limit: limit})
	if err != nil {
		return nil, batchError("list regrade batches", err)
	}

	batches := make([]domain.RegradeBatch, 0, len(resp.GetBatches()))
	for _, b := range resp.GetBatches() {
		batches = append(batches, batchFromProto(b))
	}

	return batches, nil
}

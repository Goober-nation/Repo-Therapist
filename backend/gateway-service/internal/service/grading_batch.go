package service

import (
	"context"
	"fmt"

	"gateway-service/internal/domain"
	"gateway-service/internal/transport/rpc/gen/agg"

	"google.golang.org/grpc/status"
)

// Reasons the Aggregator gives for a refused batch or catalog request; anything else carries no reason.
var batchReasons = map[string]struct{}{
	"nothing_selected": {}, "invalid_filter": {}, "limit_too_high": {},
	"batch_active": {}, "bad_state": {}, "batch_not_found": {},
	"refresh_active": {}, "refresh_not_running": {},
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
		Force:         f.Force,
		Limit:         f.Limit,
		OnlyRegrade:   f.OnlyRegrade,
	}
}

func filterFromProto(f *agg.BatchFilter) domain.BatchFilter {
	if f == nil {
		return domain.BatchFilter{}
	}

	out := domain.BatchFilter{Languages: f.GetLanguages(), Repos: f.GetRepos(), Force: f.GetForce(),
		Limit: f.GetLimit(), OnlyRegrade: f.GetOnlyRegrade()}

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

func batchFromProto(b *agg.GradingBatch) domain.GradingBatch {
	out := domain.GradingBatch{
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

// PreviewGradingBatch is the dry run: what the filter would regrade, without starting anything.
func (s *GatewayService) PreviewGradingBatch(ctx context.Context, filter domain.BatchFilter) (domain.BatchPreview, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, s.aggregatorTimeout)
	defer cancel()

	resp, err := s.aggregatorClient.PreviewGradingBatch(rpcCtx, &agg.PreviewGradingBatchRequest{Filter: filterToProto(filter)})
	if err != nil {
		return domain.BatchPreview{}, batchError("preview grading batch", err)
	}

	return domain.BatchPreview{
		Matched:         resp.GetMatched(),
		Count:           resp.GetCount(),
		Sample:          resp.GetSample(),
		Unmatched:       resp.GetUnmatched(),
		EstimateSeconds: resp.GetEstimateSeconds(),
		Inactive:        resp.GetInactive(),
	}, nil
}

// StartGradingBatch starts a batch for the filter under the admin's actor hash.
func (s *GatewayService) StartGradingBatch(ctx context.Context, actorHash string, filter domain.BatchFilter) (domain.GradingBatch, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, s.aggregatorTimeout)
	defer cancel()

	resp, err := s.aggregatorClient.StartGradingBatch(rpcCtx, &agg.StartGradingBatchRequest{
		Filter: filterToProto(filter), ActorHash: actorHash,
	})
	if err != nil {
		return domain.GradingBatch{}, batchError("start grading batch", err)
	}

	return batchFromProto(resp.GetBatch()), nil
}

// ControlGradingBatch pauses, resumes or cancels a batch.
func (s *GatewayService) ControlGradingBatch(ctx context.Context, actorHash string, id int64, action string) (domain.GradingBatch, error) {
	switch action {
	case domain.BatchActionPause, domain.BatchActionResume, domain.BatchActionCancel:
	default:
		return domain.GradingBatch{}, fmt.Errorf("%w: unknown batch action", domain.ErrInvalidRequest)
	}

	rpcCtx, cancel := context.WithTimeout(ctx, s.aggregatorTimeout)
	defer cancel()

	resp, err := s.aggregatorClient.ControlGradingBatch(rpcCtx, &agg.ControlGradingBatchRequest{
		BatchId: id, Action: action, ActorHash: actorHash,
	})
	if err != nil {
		return domain.GradingBatch{}, batchError("control grading batch", err)
	}

	return batchFromProto(resp.GetBatch()), nil
}

// GetGradingBatch returns a batch and its failed repos; id 0 means the active batch, or the latest.
func (s *GatewayService) GetGradingBatch(ctx context.Context, id int64) (domain.GradingBatchDetail, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, s.aggregatorTimeout)
	defer cancel()

	resp, err := s.aggregatorClient.GetGradingBatch(rpcCtx, &agg.GetGradingBatchRequest{BatchId: id})
	if err != nil {
		return domain.GradingBatchDetail{}, batchError("get grading batch", err)
	}

	detail := domain.GradingBatchDetail{Batch: batchFromProto(resp.GetBatch())}

	for _, f := range resp.GetFailures() {
		detail.Failures = append(detail.Failures, domain.GradingBatchFailure{
			OrgSlug: f.GetOrgSlug(), RepoSlug: f.GetRepoSlug(), Error: f.GetError(),
		})
	}

	return detail, nil
}

// ListGradingBatches returns the most recent batches, newest first, and the stand's limit cap.
func (s *GatewayService) ListGradingBatches(ctx context.Context, limit int32) (domain.GradingBatchList, error) {
	rpcCtx, cancel := context.WithTimeout(ctx, s.aggregatorTimeout)
	defer cancel()

	resp, err := s.aggregatorClient.ListGradingBatches(rpcCtx, &agg.ListGradingBatchesRequest{Limit: limit})
	if err != nil {
		return domain.GradingBatchList{}, batchError("list grading batches", err)
	}

	batches := make([]domain.GradingBatch, 0, len(resp.GetBatches()))
	for _, b := range resp.GetBatches() {
		batches = append(batches, batchFromProto(b))
	}

	return domain.GradingBatchList{Batches: batches, MaxLimit: resp.GetMaxLimit()}, nil
}

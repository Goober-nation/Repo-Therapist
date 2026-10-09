package grpc

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"aggregator-service/internal/domain"
	"aggregator-service/internal/transport/rpc/gen/server"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// batchStub implements only the batch part of AggregatorService; any other call panics.
type batchStub struct {
	AggregatorService

	filter  domain.BatchFilter
	actor   string
	id      int64
	action  string
	batch   domain.Batch
	preview domain.BatchPreview
	failed  []domain.BatchItem
	err     error
}

func (b *batchStub) PreviewBatch(_ context.Context, f domain.BatchFilter) (domain.BatchPreview, error) {
	b.filter = f
	return b.preview, b.err
}

func (b *batchStub) StartBatch(_ context.Context, actor string, f domain.BatchFilter) (domain.Batch, error) {
	b.actor, b.filter = actor, f
	return b.batch, b.err
}

func (b *batchStub) ControlBatch(_ context.Context, actor string, id int64, action string) (domain.Batch, error) {
	b.actor, b.id, b.action = actor, id, action
	return b.batch, b.err
}

func (b *batchStub) BatchStatus(_ context.Context, id int64) (domain.Batch, []domain.BatchItem, error) {
	b.id = id
	return b.batch, b.failed, b.err
}

func (b *batchStub) ListBatches(context.Context, int) ([]domain.Batch, error) {
	return []domain.Batch{b.batch}, b.err
}

func (b *batchStub) MaxBatchLimit() int { return 250 }

func TestListGradingBatchesCarriesTheStandMaximum(t *testing.T) {
	resp, err := NewServer(&batchStub{}).ListGradingBatches(context.Background(), &server.ListGradingBatchesRequest{})
	if err != nil || resp.GetMaxLimit() != 250 {
		t.Fatalf("max_limit = %d, %v", resp.GetMaxLimit(), err)
	}
}

func TestStartGradingBatchMapsFilterAndBatch(t *testing.T) {
	finished := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	stub := &batchStub{batch: domain.Batch{ID: 7, Status: domain.BatchRunning, Total: 5, Pending: 3, OK: 1, Failed: 1, FinishedAt: &finished,
		Filter: domain.BatchFilter{Languages: []string{"go"}}}}

	min, max, days := int32(10), int32(60), int32(30)

	resp, err := NewServer(stub).StartGradingBatch(context.Background(), &server.StartGradingBatchRequest{
		ActorHash: "abc123",
		Filter: &server.BatchFilter{Languages: []string{"go"}, MinScore: &min, MaxScore: &max, OlderThanDays: &days,
			Repos: []string{"org/repo"}, Limit: 250, OnlyRegrade: true},
	})
	if err != nil {
		t.Fatal(err)
	}

	f := stub.filter
	if stub.actor != "abc123" || f.MinScore == nil || *f.MinScore != 10 || *f.MaxScore != 60 || *f.OlderThanDays != 30 ||
		len(f.Languages) != 1 || len(f.Repos) != 1 || f.Limit != 250 || !f.OnlyRegrade {
		t.Errorf("filter = %+v, actor %q", f, stub.actor)
	}

	got := resp.GetBatch()
	if got.GetId() != 7 || got.GetTotal() != 5 || got.GetPending() != 3 || got.GetOk() != 1 || got.GetFailed() != 1 ||
		got.GetFinishedAt() == nil || got.GetFilter().GetLanguages()[0] != "go" {
		t.Errorf("batch = %+v", got)
	}
}

func TestBatchFilterUnsetScoresStayUnset(t *testing.T) {
	stub := &batchStub{}

	if _, err := NewServer(stub).PreviewGradingBatch(context.Background(), &server.PreviewGradingBatchRequest{
		Filter: &server.BatchFilter{Limit: 5},
	}); err != nil {
		t.Fatal(err)
	}

	if f := stub.filter; f.Limit != 5 || f.OnlyRegrade || f.MinScore != nil || f.MaxScore != nil || f.OlderThanDays != nil {
		t.Errorf("filter = %+v: an unset bound must not become 0", f)
	}
}

func TestBatchErrorsMapToStatusCodes(t *testing.T) {
	cases := []struct {
		cause  error
		code   codes.Code
		reason string
	}{
		{domain.ErrInvalidBatch, codes.InvalidArgument, BatchReasonInvalid},
		{domain.ErrBatchNothing, codes.InvalidArgument, BatchReasonNothing},
		{fmt.Errorf("%w (50)", domain.ErrBatchLimitTooHigh), codes.InvalidArgument, BatchReasonTooHigh},
		{domain.ErrBatchActive, codes.AlreadyExists, BatchReasonActive},
		{domain.ErrBatchState, codes.Aborted, BatchReasonState},
		{domain.ErrBatchNotFound, codes.NotFound, BatchReasonNotFound},
		{fmt.Errorf("%w: wrapped detail", domain.ErrInvalidBatch), codes.InvalidArgument, BatchReasonInvalid},
	}

	for _, tc := range cases {
		stub := &batchStub{err: tc.cause}

		_, err := NewServer(stub).ControlGradingBatch(context.Background(), &server.ControlGradingBatchRequest{BatchId: 1, Action: "pause"})
		if status.Code(err) != tc.code || status.Convert(err).Message() != tc.reason {
			t.Errorf("%v: got %v %q, want %v %q", tc.cause, status.Code(err), status.Convert(err).Message(), tc.code, tc.reason)
		}
	}
}

func TestControlGradingBatchPassesActionAndActor(t *testing.T) {
	stub := &batchStub{batch: domain.Batch{ID: 3}}

	if _, err := NewServer(stub).ControlGradingBatch(context.Background(), &server.ControlGradingBatchRequest{BatchId: 3, Action: "cancel", ActorHash: "ff00"}); err != nil {
		t.Fatal(err)
	}

	if stub.id != 3 || stub.action != "cancel" || stub.actor != "ff00" {
		t.Errorf("got id %d action %q actor %q", stub.id, stub.action, stub.actor)
	}
}

func TestGetGradingBatchReturnsFailures(t *testing.T) {
	stub := &batchStub{
		batch:  domain.Batch{ID: 9, Status: domain.BatchCompleted},
		failed: []domain.BatchItem{{OrgSlug: "org", RepoSlug: "bad", Error: domain.BatchErrorTimeout}},
	}

	resp, err := NewServer(stub).GetGradingBatch(context.Background(), &server.GetGradingBatchRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if len(resp.GetFailures()) != 1 || resp.GetFailures()[0].GetError() != "timeout" || resp.GetFailures()[0].GetRepoSlug() != "bad" {
		t.Errorf("failures = %+v", resp.GetFailures())
	}

	stub.err = errors.New("db down")
	if _, err := NewServer(stub).GetGradingBatch(context.Background(), &server.GetGradingBatchRequest{}); status.Code(err) != codes.Internal {
		t.Errorf("code = %v, want Internal", status.Code(err))
	}
}

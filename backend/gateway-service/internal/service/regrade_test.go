package service

import (
	"context"
	"errors"
	"testing"

	"gateway-service/internal/domain"
	"gateway-service/internal/transport/rpc/gen/agg"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type batchAggregator struct {
	fakeAggregator

	startIn   *agg.StartRegradeBatchRequest
	controlIn *agg.ControlRegradeBatchRequest
	startErr  error
}

func (b *batchAggregator) StartRegradeBatch(_ context.Context, in *agg.StartRegradeBatchRequest, _ ...grpc.CallOption) (*agg.StartRegradeBatchResponse, error) {
	b.startIn = in
	if b.startErr != nil {
		return nil, b.startErr
	}

	return &agg.StartRegradeBatchResponse{Batch: &agg.RegradeBatch{Id: 4, Status: "running", Total: 3, Pending: 3}}, nil
}

func (b *batchAggregator) ControlRegradeBatch(_ context.Context, in *agg.ControlRegradeBatchRequest, _ ...grpc.CallOption) (*agg.ControlRegradeBatchResponse, error) {
	b.controlIn = in
	return &agg.ControlRegradeBatchResponse{Batch: &agg.RegradeBatch{Id: in.BatchId}}, nil
}

func (b *batchAggregator) GetRegradeBatch(context.Context, *agg.GetRegradeBatchRequest, ...grpc.CallOption) (*agg.GetRegradeBatchResponse, error) {
	return &agg.GetRegradeBatchResponse{
		Batch:    &agg.RegradeBatch{Id: 4},
		Failures: []*agg.RegradeBatchFailure{{OrgSlug: "o", RepoSlug: "r", Error: "timeout"}},
	}, nil
}

func TestStartRegradeBatchForwardsFilterAndActor(t *testing.T) {
	a := &batchAggregator{}
	svc := NewGatewayService(a, nil, nil, nil, nil, nil, 0, 0, 0)
	min := int32(10)

	batch, err := svc.StartRegradeBatch(context.Background(), "abc123", domain.BatchFilter{Languages: []string{"go"}, MinScore: &min})
	if err != nil {
		t.Fatal(err)
	}

	if batch.ID != 4 || batch.Total != 3 || a.startIn.ActorHash != "abc123" || a.startIn.Filter.GetMinScore() != 10 ||
		a.startIn.Filter.MaxScore != nil {
		t.Fatalf("batch %+v, request %+v", batch, a.startIn)
	}
}

func TestBatchErrorsKeepTheReasonWord(t *testing.T) {
	cases := map[string]struct {
		code codes.Code
		is   error
	}{
		"batch_active":     {codes.AlreadyExists, domain.ErrConflict},
		"too_many":         {codes.InvalidArgument, domain.ErrInvalidRequest},
		"bad_state":        {codes.Aborted, domain.ErrConflict},
		"batch_not_found":  {codes.NotFound, domain.ErrNotFound},
		"nothing_selected": {codes.InvalidArgument, domain.ErrInvalidRequest},
	}

	for reason, tc := range cases {
		a := &batchAggregator{startErr: status.Error(tc.code, reason)}
		svc := NewGatewayService(a, nil, nil, nil, nil, nil, 0, 0, 0)

		_, err := svc.StartRegradeBatch(context.Background(), "abc123", domain.BatchFilter{All: true})

		var got domain.ReasonError
		if !errors.As(err, &got) || got.Reason != reason || !errors.Is(err, tc.is) {
			t.Errorf("%s: err = %v", reason, err)
		}
	}

	a := &batchAggregator{startErr: status.Error(codes.Internal, "boom")}
	svc := NewGatewayService(a, nil, nil, nil, nil, nil, 0, 0, 0)

	var got domain.ReasonError
	if _, err := svc.StartRegradeBatch(context.Background(), "abc123", domain.BatchFilter{All: true}); errors.As(err, &got) {
		t.Errorf("an unknown message must not become a reason: %v", err)
	}
}

func TestControlRegradeBatchChecksTheAction(t *testing.T) {
	a := &batchAggregator{}
	svc := NewGatewayService(a, nil, nil, nil, nil, nil, 0, 0, 0)

	if _, err := svc.ControlRegradeBatch(context.Background(), "abc123", 4, "explode"); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("unknown action: %v", err)
	}

	if a.controlIn != nil {
		t.Fatal("an unknown action must not reach the Aggregator")
	}

	if _, err := svc.ControlRegradeBatch(context.Background(), "abc123", 4, "pause"); err != nil || a.controlIn.Action != "pause" || a.controlIn.ActorHash != "abc123" {
		t.Fatalf("pause: %v %+v", err, a.controlIn)
	}
}

func TestGetRegradeBatchMapsFailures(t *testing.T) {
	svc := NewGatewayService(&batchAggregator{}, nil, nil, nil, nil, nil, 0, 0, 0)

	detail, err := svc.GetRegradeBatch(context.Background(), 0)
	if err != nil || len(detail.Failures) != 1 || detail.Failures[0].Error != "timeout" {
		t.Fatalf("%+v %v", detail, err)
	}
}

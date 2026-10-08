package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"aggregator-service/internal/domain"
	"aggregator-service/internal/transport/rpc/gen/server"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// auditStub implements only the audit part of AggregatorService; the embedded interface is nil, so
// any other call would panic and fail the test.
type auditStub struct {
	AggregatorService

	event  domain.AuditEvent
	filter domain.AuditFilter
	page   domain.AuditPage
	err    error
}

func (a *auditStub) RecordAudit(_ context.Context, event domain.AuditEvent) error {
	a.event = event
	return a.err
}

func (a *auditStub) ListAudit(_ context.Context, filter domain.AuditFilter) (domain.AuditPage, error) {
	a.filter = filter
	return a.page, a.err
}

func TestRecordAuditMapsRequestAndInvalidArgument(t *testing.T) {
	stub := &auditStub{}

	_, err := NewServer(stub).RecordAudit(context.Background(), &server.RecordAuditRequest{
		ActorKind: "admin", ActorHash: "ab12", Action: "admin.denied", TargetKind: "route",
		Target: "/admin/audit", Outcome: "denied", Details: []byte(`{"reason":"not_admin"}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := domain.AuditEvent{
		ActorKind: "admin", ActorHash: "ab12", Action: "admin.denied", TargetKind: "route",
		Target: "/admin/audit", Outcome: "denied", Details: []byte(`{"reason":"not_admin"}`),
	}
	if stub.event.Action != want.Action || stub.event.ActorHash != want.ActorHash || string(stub.event.Details) != string(want.Details) ||
		stub.event.TargetKind != want.TargetKind || stub.event.Target != want.Target || stub.event.Outcome != want.Outcome {
		t.Errorf("event = %+v, want %+v", stub.event, want)
	}

	stub.err = domain.ErrInvalidAudit
	if _, err := NewServer(stub).RecordAudit(context.Background(), &server.RecordAuditRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", status.Code(err))
	}

	stub.err = errors.New("db down")
	if _, err := NewServer(stub).RecordAudit(context.Background(), &server.RecordAuditRequest{}); status.Code(err) != codes.Internal {
		t.Errorf("code = %v, want Internal", status.Code(err))
	}
}

func TestListAuditMapsFilterAndEntries(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	stub := &auditStub{page: domain.AuditPage{
		NextBeforeID: 41,
		Entries: []domain.AuditEntry{{
			ID: 42, OccurredAt: at,
			AuditEvent: domain.AuditEvent{ActorKind: "user", Action: "admin.denied", Outcome: "denied", Details: []byte(`{}`)},
		}},
	}}

	got, err := NewServer(stub).ListAudit(context.Background(), &server.ListAuditRequest{
		Limit: 25, BeforeId: 100, Action: "admin.", Outcome: "denied",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stub.filter != (domain.AuditFilter{Limit: 25, BeforeID: 100, Action: "admin.", Outcome: "denied"}) {
		t.Errorf("filter = %+v", stub.filter)
	}

	if got.GetNextBeforeId() != 41 || len(got.GetEntries()) != 1 {
		t.Fatalf("response = %v", got)
	}

	entry := got.GetEntries()[0]
	if entry.GetId() != 42 || !entry.GetOccurredAt().AsTime().Equal(at) || entry.GetActorKind() != "user" || string(entry.GetDetails()) != "{}" {
		t.Errorf("entry = %v", entry)
	}
}

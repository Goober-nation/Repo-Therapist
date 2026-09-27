package transport

import (
	"context"
	"testing"
	"time"

	"repo-therapist/grade-service/internal/contracts"
)

func TestDocsWaitersDeliversReply(t *testing.T) {
	w := newDocsWaiters()
	ch := w.register("u1")
	if !w.deliver(contracts.DocsGraded{SourcecraftUUID: "u1", Grade: intPtr(84)}) {
		t.Fatal("reply for a waiting repo was not delivered")
	}
	got := w.wait(context.Background(), "u1", ch, time.Now().Add(time.Second))
	if got == nil || *got.Grade != 84 {
		t.Fatalf("got %+v, want grade 84", got)
	}
}

func TestDocsWaitersTimeoutThenLateReplyDropped(t *testing.T) {
	w := newDocsWaiters()
	ch := w.register("u1")
	if got := w.wait(context.Background(), "u1", ch, time.Now().Add(10*time.Millisecond)); got != nil {
		t.Fatalf("got %+v, want nil on timeout", got)
	}
	if w.deliver(contracts.DocsGraded{SourcecraftUUID: "u1"}) {
		t.Fatal("late reply was delivered after the waiter timed out")
	}
}

func TestDocsWaitersDuplicateAndUnknownDropped(t *testing.T) {
	w := newDocsWaiters()
	w.register("u1")
	if w.deliver(contracts.DocsGraded{SourcecraftUUID: "other"}) {
		t.Fatal("reply for an unknown repo was delivered")
	}
	if !w.deliver(contracts.DocsGraded{SourcecraftUUID: "u1"}) {
		t.Fatal("first reply not delivered")
	}
	if w.deliver(contracts.DocsGraded{SourcecraftUUID: "u1"}) {
		t.Fatal("duplicate reply was delivered")
	}
}

func intPtr(v int) *int { return &v }

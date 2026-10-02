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
	if !w.deliver(contracts.DocsGraded{JobID: "u1", Grade: intPtr(84)}) {
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
	if w.deliver(contracts.DocsGraded{JobID: "u1"}) {
		t.Fatal("late reply was delivered after the waiter timed out")
	}
}

func TestDocsWaitersDuplicateAndUnknownDropped(t *testing.T) {
	w := newDocsWaiters()
	w.register("u1")
	if w.deliver(contracts.DocsGraded{JobID: "other"}) {
		t.Fatal("reply for an unknown repo was delivered")
	}
	if !w.deliver(contracts.DocsGraded{JobID: "u1"}) {
		t.Fatal("first reply not delivered")
	}
	if w.deliver(contracts.DocsGraded{JobID: "u1"}) {
		t.Fatal("duplicate reply was delivered")
	}
}

func TestDocsWaitersOverlappingJobsOfOneRepoGetOwnReplies(t *testing.T) {
	w := newDocsWaiters()
	chA := w.register("job-a")
	chB := w.register("job-b")
	if !w.deliver(contracts.DocsGraded{JobID: "job-b", SourcecraftUUID: "u1", Grade: intPtr(20)}) ||
		!w.deliver(contracts.DocsGraded{JobID: "job-a", SourcecraftUUID: "u1", Grade: intPtr(90)}) {
		t.Fatal("replies for both jobs must be delivered")
	}
	a := w.wait(context.Background(), "job-a", chA, time.Now().Add(time.Second))
	b := w.wait(context.Background(), "job-b", chB, time.Now().Add(time.Second))
	if a == nil || b == nil || *a.Grade != 90 || *b.Grade != 20 {
		t.Fatalf("a=%+v b=%+v, want each job's own reply", a, b)
	}
}

func intPtr(v int) *int { return &v }

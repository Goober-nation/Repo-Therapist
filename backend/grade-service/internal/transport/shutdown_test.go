package transport

import (
	"context"
	"testing"

	"repo-therapist/grade-service/internal/contracts"
)

func TestDetachedOnShutdownOutlivesCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	publishCtx, done := detachedOnShutdown(ctx)
	defer done()

	if publishCtx.Err() != nil {
		t.Fatalf("publish context already done: %v", publishCtx.Err())
	}
	if _, ok := publishCtx.Deadline(); !ok {
		t.Error("detached context has no deadline")
	}
}

func TestDetachedOnShutdownKeepsLiveContext(t *testing.T) {
	ctx := context.Background()

	publishCtx, done := detachedOnShutdown(ctx)
	defer done()

	if publishCtx != ctx {
		t.Error("live context was replaced")
	}
}

func TestGradingErrCodeMarksShutdownAsInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	if got := gradingErrCode(ctx); got != contracts.GradingErrGrading {
		t.Errorf("live ctx code = %q", got)
	}
	cancel()
	if got := gradingErrCode(ctx); got != contracts.GradingErrInterrupted {
		t.Errorf("cancelled ctx code = %q", got)
	}
}

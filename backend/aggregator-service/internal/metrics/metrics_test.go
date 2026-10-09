package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func scrape(t *testing.T, m *Metrics) string {
	t.Helper()

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	body, _ := io.ReadAll(rec.Body)

	return string(body)
}

func mustContain(t *testing.T, out string, wants ...string) {
	t.Helper()

	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestGradingCountersAndDuration(t *testing.T) {
	m := New()

	m.GradingRequested("public", "batch")
	m.GradingRequested("public", "user")
	m.GradingCompleted("public", "ok", 45*time.Second)
	m.GradingCompleted("public", "error", 0) // requested before a restart: counted, not timed
	m.GradeLimiterRejected()

	out := scrape(t, m)

	mustContain(t, out,
		`aggregator_gradings_requested_total{trigger="batch",visibility="public"} 1`,
		`aggregator_gradings_requested_total{trigger="user",visibility="public"} 1`,
		`aggregator_gradings_completed_total{outcome="ok",visibility="public"} 1`,
		`aggregator_gradings_completed_total{outcome="error",visibility="public"} 1`,
		`aggregator_grading_duration_seconds_count{outcome="ok",visibility="public"} 1`,
		`aggregator_grading_duration_seconds_sum{outcome="ok",visibility="public"} 45`,
		`aggregator_grade_limiter_rejections_total 1`,
	)

	// The error series exists (it starts at zero) but the untimed result added no observation to it.
	mustContain(t, out, `aggregator_grading_duration_seconds_count{outcome="error",visibility="public"} 0`)
}

func TestLLMAndBatchCounters(t *testing.T) {
	m := New()

	m.LLMRequest("requested")
	m.LLMRequest("received_ok")
	m.BatchItem("ok")
	m.BatchItem("failed")
	m.BatchEvent("start")

	mustContain(t, scrape(t, m),
		`aggregator_llm_requests_total{outcome="requested"} 1`,
		`aggregator_llm_requests_total{outcome="received_ok"} 1`,
		`aggregator_grading_batch_items_total{status="failed"} 1`,
		`aggregator_grading_batch_events_total{event="start"} 1`,
	)
}

func TestScheduledRegradeGaugesReadAtScrapeTime(t *testing.T) {
	m := New()
	running, total, done := false, 0, 0
	m.RegisterScheduledRegrade(func() (bool, int, int) { return running, total, done })

	mustContain(t, scrape(t, m), "aggregator_scheduled_regrade_running 0", "aggregator_scheduled_regrade_repos_total 0")

	running, total, done = true, 20, 7

	mustContain(t, scrape(t, m),
		"aggregator_scheduled_regrade_running 1",
		"aggregator_scheduled_regrade_repos_total 20",
		"aggregator_scheduled_regrade_repos_done 7",
	)
}

func TestGRPCServerInterceptorRecordsMethodAndCode(t *testing.T) {
	m := New()
	intercept := m.UnaryServerInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/aggregator.Aggregator/GetGrading"}

	_, _ = intercept(context.Background(), nil, info, func(context.Context, any) (any, error) { return nil, nil })
	_, _ = intercept(context.Background(), nil, info, func(context.Context, any) (any, error) {
		return nil, status.Error(codes.NotFound, "x")
	})

	mustContain(t, scrape(t, m),
		`aggregator_grpc_requests_total{code="OK",method="/aggregator.Aggregator/GetGrading"} 1`,
		`aggregator_grpc_requests_total{code="NotFound",method="/aggregator.Aggregator/GetGrading"} 1`,
		`aggregator_grpc_request_duration_seconds_count{method="/aggregator.Aggregator/GetGrading"} 2`,
	)
}

func TestNilMetricsAreSafe(t *testing.T) {
	var m *Metrics

	m.GradingRequested("public", "user")
	m.GradingCompleted("public", "ok", time.Second)
	m.GradeLimiterRejected()
	m.LLMRequest("x")
	m.BatchItem("ok")
	m.BatchEvent("start")
}

func TestServeIsOffWithoutAnAddress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	New().Serve(ctx, "")
}

func TestKnownSeriesStartAtZeroSoTheFirstEventCounts(t *testing.T) {
	mustContain(t, scrape(t, New()),
		`aggregator_gradings_completed_total{outcome="ok",visibility="public"} 0`,
		`aggregator_gradings_completed_total{outcome="error",visibility="private"} 0`,
		`aggregator_gradings_requested_total{trigger="batch",visibility="public"} 0`,
		`aggregator_llm_requests_total{outcome="received_ok"} 0`,
		`aggregator_grading_batch_items_total{status="failed"} 0`,
		`aggregator_grading_batch_events_total{event="complete"} 0`,
		`aggregator_grading_duration_seconds_count{outcome="ok",visibility="public"} 0`,
	)
}

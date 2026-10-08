// Package metrics holds the Aggregator's Prometheus metrics (docs/metrics.md). Labels are fixed words
// (visibility, trigger, outcome, gRPC method): never a repository slug, a user ID or a token.
package metrics

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

type Metrics struct {
	registry *prometheus.Registry

	grpcRequests    *prometheus.CounterVec
	grpcDuration    *prometheus.HistogramVec
	gradingsStarted *prometheus.CounterVec
	gradingsDone    *prometheus.CounterVec
	gradingDuration *prometheus.HistogramVec
	limiterRejected prometheus.Counter
	llmRequests     *prometheus.CounterVec
	batchItems      *prometheus.CounterVec
	batchEvents     *prometheus.CounterVec
}

func New() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		grpcRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "aggregator_grpc_requests_total",
			Help: "gRPC requests served by method and result code.",
		}, []string{"method", "code"}),
		grpcDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "aggregator_grpc_request_duration_seconds",
			Help:    "gRPC request duration by method.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method"}),
		gradingsStarted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "aggregator_gradings_requested_total",
			Help: "Gradings sent to grade-service by visibility and trigger (user, schedule, batch).",
		}, []string{"visibility", "trigger"}),
		gradingsDone: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "aggregator_gradings_completed_total",
			Help: "Grading results received by visibility and outcome (ok, error).",
		}, []string{"visibility", "outcome"}),
		gradingDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "aggregator_grading_duration_seconds",
			Help: "Time from the grading request to its result, by visibility and outcome.",
			// Gradings take seconds to minutes: clone, analysis, optional model call.
			Buckets: []float64{5, 10, 20, 30, 60, 120, 300, 600, 1200},
		}, []string{"visibility", "outcome"}),
		limiterRejected: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "aggregator_grade_limiter_rejections_total",
			Help: "User gradings refused by the service-wide grading limit.",
		}),
		llmRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "aggregator_llm_requests_total",
			Help: "LLM recommendation requests by outcome (requested, publish_failed, received_ok, received_error).",
		}, []string{"outcome"}),
		batchItems: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "aggregator_regrade_batch_items_total",
			Help: "Batch regrade repositories by final status (ok, failed, skipped).",
		}, []string{"status"}),
		batchEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "aggregator_regrade_batch_events_total",
			Help: "Batch regrade events (start, pause, resume, cancel, complete).",
		}, []string{"event"}),
	}

	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.grpcRequests, m.grpcDuration, m.gradingsStarted, m.gradingsDone, m.gradingDuration,
		m.limiterRejected, m.llmRequests, m.batchItems, m.batchEvents,
	)

	m.initSeries()

	return m
}

// initSeries creates every known label combination at zero. A counter that first shows up already at 3
// has no earlier sample, so Prometheus' increase() would report 0 for it; starting at 0 makes the first
// event count.
func (m *Metrics) initSeries() {
	for _, visibility := range []string{"public", "private"} {
		for _, trigger := range []string{"user", "schedule", "batch"} {
			m.gradingsStarted.WithLabelValues(visibility, trigger)
		}

		for _, outcome := range []string{"ok", "error"} {
			m.gradingsDone.WithLabelValues(visibility, outcome)
			m.gradingDuration.WithLabelValues(visibility, outcome)
		}
	}

	for _, outcome := range []string{"requested", "publish_failed", "received_ok", "received_error"} {
		m.llmRequests.WithLabelValues(outcome)
	}

	for _, status := range []string{"ok", "failed", "skipped"} {
		m.batchItems.WithLabelValues(status)
	}

	for _, event := range []string{"start", "pause", "resume", "cancel", "complete"} {
		m.batchEvents.WithLabelValues(event)
	}
}

// RegisterScheduledRegrade exposes the progress of the scheduled public regrade, read at scrape time.
func (m *Metrics) RegisterScheduledRegrade(progress func() (running bool, total, done int)) {
	gauge := func(name, help string, value func() float64) {
		m.registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: name, Help: help}, value))
	}

	gauge("aggregator_scheduled_regrade_running", "1 while the scheduled public regrade runs.", func() float64 {
		if running, _, _ := progress(); running {
			return 1
		}

		return 0
	})
	gauge("aggregator_scheduled_regrade_repos_total", "Repositories of the current scheduled regrade run.", func() float64 {
		_, total, _ := progress()

		return float64(total)
	})
	gauge("aggregator_scheduled_regrade_repos_done", "Repositories the current scheduled regrade run has handled.", func() float64 {
		_, _, done := progress()

		return float64(done)
	})
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// The methods below satisfy service.Recorder; all are safe on a nil *Metrics.

func (m *Metrics) GradingRequested(visibility, trigger string) {
	if m != nil {
		m.gradingsStarted.WithLabelValues(visibility, trigger).Inc()
	}
}

// GradingCompleted counts a result; duration is zero when the request was not seen by this process
// (it was made before a restart), and is then not observed.
func (m *Metrics) GradingCompleted(visibility, outcome string, duration time.Duration) {
	if m == nil {
		return
	}

	m.gradingsDone.WithLabelValues(visibility, outcome).Inc()

	if duration > 0 {
		m.gradingDuration.WithLabelValues(visibility, outcome).Observe(duration.Seconds())
	}
}

func (m *Metrics) GradeLimiterRejected() {
	if m != nil {
		m.limiterRejected.Inc()
	}
}

func (m *Metrics) LLMRequest(outcome string) {
	if m != nil {
		m.llmRequests.WithLabelValues(outcome).Inc()
	}
}

func (m *Metrics) BatchItem(status string) {
	if m != nil {
		m.batchItems.WithLabelValues(status).Inc()
	}
}

func (m *Metrics) BatchEvent(event string) {
	if m != nil {
		m.batchEvents.WithLabelValues(event).Inc()
	}
}

// UnaryServerInterceptor records every gRPC request the Aggregator serves.
func (m *Metrics) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)

		m.grpcRequests.WithLabelValues(info.FullMethod, status.Code(err).String()).Inc()
		m.grpcDuration.WithLabelValues(info.FullMethod).Observe(time.Since(start).Seconds())

		return resp, err
	}
}

// Serve exposes the metrics on addr until ctx ends; an empty addr leaves them off. The port is for the
// monitoring network only.
func (m *Metrics) Serve(ctx context.Context, addr string) {
	if addr == "" {
		log.Println("metrics endpoint is off (METRICS_ADDRESS is empty)")

		return
	}

	mux := http.NewServeMux()
	mux.Handle("GET /metrics", m.Handler())

	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		log.Printf("metrics endpoint listening on %s", addr)

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("metrics endpoint: %v", err)
		}
	}()

	go func() {
		<-ctx.Done()

		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		_ = server.Shutdown(shutdown)
	}()
}

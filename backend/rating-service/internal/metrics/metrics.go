// Package metrics holds the Rating service's Prometheus metrics (docs/metrics.md). Labels are fixed
// words: gRPC methods and cache results.
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

	grpcRequests *prometheus.CounterVec
	grpcDuration *prometheus.HistogramVec
	analytics    *prometheus.CounterVec
}

func New() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		grpcRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rating_grpc_requests_total",
			Help: "gRPC requests served by method and result code.",
		}, []string{"method", "code"}),
		grpcDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "rating_grpc_request_duration_seconds",
			Help:    "gRPC request duration by method.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method"}),
		analytics: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rating_analytics_cache_total",
			Help: "Platform analytics requests by cache result (hit, miss).",
		}, []string{"result"}),
	}

	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.grpcRequests, m.grpcDuration, m.analytics,
	)

	// Known series start at zero so that the first event is visible to Prometheus' increase().
	m.analytics.WithLabelValues("hit")
	m.analytics.WithLabelValues("miss")

	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// AnalyticsCache counts one analytics request answered from the cache (hit) or computed (miss).
func (m *Metrics) AnalyticsCache(hit bool) {
	if m == nil {
		return
	}

	result := "miss"
	if hit {
		result = "hit"
	}

	m.analytics.WithLabelValues(result).Inc()
}

// UnaryServerInterceptor records every gRPC request the service answers.
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

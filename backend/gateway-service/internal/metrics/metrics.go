// Package metrics holds the Gateway's Prometheus metrics (docs/metrics.md). Labels are fixed words
// (route patterns, outcome codes): never a path with a repository slug, a user ID, an IP or a query.
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

// UnmatchedRoute labels requests no route matched, so scanners cannot create a series per path.
const UnmatchedRoute = "unmatched"

type Metrics struct {
	registry *prometheus.Registry

	httpRequests *prometheus.CounterVec
	httpDuration *prometheus.HistogramVec
	grpcCalls    *prometheus.CounterVec
	grpcDuration *prometheus.HistogramVec
	adminDenied  *prometheus.CounterVec
	adminUnlock  *prometheus.CounterVec
}

func New() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gateway_http_requests_total",
			Help: "HTTP requests by route pattern and status code.",
		}, []string{"route", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "gateway_http_request_duration_seconds",
			Help:    "HTTP request duration by route pattern.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
		grpcCalls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gateway_grpc_client_calls_total",
			Help: "gRPC calls from the Gateway by target service, method and result code.",
		}, []string{"target", "method", "code"}),
		grpcDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "gateway_grpc_client_call_duration_seconds",
			Help:    "gRPC call duration by target service and method.",
			Buckets: prometheus.DefBuckets,
		}, []string{"target", "method"}),
		adminDenied: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gateway_admin_denied_total",
			Help: "Refused admin route attempts by reason (every attempt, not only those that reach the audit log).",
		}, []string{"reason"}),
		adminUnlock: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gateway_admin_unlock_total",
			Help: "Password unlock attempts by result.",
		}, []string{"result"}),
	}

	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.httpRequests, m.httpDuration, m.grpcCalls, m.grpcDuration, m.adminDenied, m.adminUnlock,
	)

	// Known series start at zero: a counter that first shows up already at N has no earlier sample, so
	// Prometheus' increase() would report 0 for it.
	for _, reason := range []string{"no_session", "invalid_session", "not_admin"} {
		m.adminDenied.WithLabelValues(reason)
	}

	for _, result := range []string{"ok", "wrong_password", "locked", "no_session", "invalid_session"} {
		m.adminUnlock.WithLabelValues(result)
	}

	return m
}

// Handler serves the metrics in the Prometheus text format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// ObserveHTTP records one finished request. pattern is the matched route pattern ("" when none).
func (m *Metrics) ObserveHTTP(pattern string, statusCode int, duration time.Duration) {
	if m == nil {
		return
	}

	if pattern == "" {
		pattern = UnmatchedRoute
	}

	m.httpRequests.WithLabelValues(pattern, statusLabel(statusCode)).Inc()
	m.httpDuration.WithLabelValues(pattern).Observe(duration.Seconds())
}

func statusLabel(code int) string {
	if code < 100 || code > 599 {
		return "other"
	}

	return string(rune('0'+code/100)) + string(rune('0'+code/10%10)) + string(rune('0'+code%10))
}

// AdminDenied counts a refused admin attempt; reason is one of the fixed deny reasons.
func (m *Metrics) AdminDenied(reason string) {
	if m != nil {
		m.adminDenied.WithLabelValues(reason).Inc()
	}
}

// AdminUnlock counts a password unlock attempt; result is ok, wrong_password, locked or another fixed word.
func (m *Metrics) AdminUnlock(result string) {
	if m != nil {
		m.adminUnlock.WithLabelValues(result).Inc()
	}
}

// UnaryClientInterceptor records every gRPC call made through the connection to target.
func (m *Metrics) UnaryClientInterceptor(target string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		start := time.Now()
		err := invoker(ctx, method, req, reply, cc, opts...)

		m.grpcCalls.WithLabelValues(target, method, status.Code(err).String()).Inc()
		m.grpcDuration.WithLabelValues(target, method).Observe(time.Since(start).Seconds())

		return err
	}
}

// Serve exposes the metrics on addr until ctx ends. An empty addr leaves them off. The port is for the
// monitoring network only: it is not the public HTTP port.
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

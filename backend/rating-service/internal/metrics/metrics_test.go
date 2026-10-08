package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestGRPCInterceptorAndCacheCounters(t *testing.T) {
	m := New()
	intercept := m.UnaryServerInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/rating.Rating/GetRanking"}

	_, _ = intercept(context.Background(), nil, info, func(context.Context, any) (any, error) { return nil, nil })
	_, _ = intercept(context.Background(), nil, info, func(context.Context, any) (any, error) {
		return nil, status.Error(codes.InvalidArgument, "x")
	})

	m.AnalyticsCache(true)
	m.AnalyticsCache(true)
	m.AnalyticsCache(false)

	out := scrape(t, m)

	for _, want := range []string{
		`rating_grpc_requests_total{code="OK",method="/rating.Rating/GetRanking"} 1`,
		`rating_grpc_requests_total{code="InvalidArgument",method="/rating.Rating/GetRanking"} 1`,
		`rating_grpc_request_duration_seconds_count{method="/rating.Rating/GetRanking"} 2`,
		`rating_analytics_cache_total{result="hit"} 2`,
		`rating_analytics_cache_total{result="miss"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestNilMetricsAreSafeAndServeCanBeOff(t *testing.T) {
	var m *Metrics
	m.AnalyticsCache(true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	New().Serve(ctx, "")
}

func TestCacheSeriesStartAtZero(t *testing.T) {
	out := scrape(t, New())
	if !strings.Contains(out, `rating_analytics_cache_total{result="hit"} 0`) || !strings.Contains(out, `rating_analytics_cache_total{result="miss"} 0`) {
		t.Fatalf("missing zero series:\n%s", out)
	}
}

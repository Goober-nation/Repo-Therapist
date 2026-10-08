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

func TestHTTPMetricsUseRoutePatternsNotPaths(t *testing.T) {
	m := New()

	// What a mux reports: the pattern with its placeholders, never the real slug.
	m.ObserveHTTP("GET /score/{org}/{repo}", 200, 20*time.Millisecond)
	m.ObserveHTTP("GET /score/{org}/{repo}", 404, time.Millisecond)
	m.ObserveHTTP("", 404, time.Millisecond)

	out := scrape(t, m)

	for _, want := range []string{
		`gateway_http_requests_total{route="GET /score/{org}/{repo}",status="200"} 1`,
		`gateway_http_requests_total{route="GET /score/{org}/{repo}",status="404"} 1`,
		`gateway_http_requests_total{route="unmatched",status="404"} 1`,
		`gateway_http_request_duration_seconds_count{route="GET /score/{org}/{repo}"} 2`,
		"go_goroutines",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestStatusLabelIsBounded(t *testing.T) {
	for code, want := range map[int]string{200: "200", 404: "404", 503: "503", 0: "other", 1000: "other", -5: "other"} {
		if got := statusLabel(code); got != want {
			t.Errorf("statusLabel(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestGRPCInterceptorCountsCodesAndTimes(t *testing.T) {
	m := New()
	intercept := m.UnaryClientInterceptor("aggregator")

	ok := func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error { return nil }
	fail := func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
		return status.Error(codes.Unavailable, "down")
	}

	_ = intercept(context.Background(), "/aggregator.Aggregator/GetGrading", nil, nil, nil, wrap(ok))
	_ = intercept(context.Background(), "/aggregator.Aggregator/GetGrading", nil, nil, nil, wrap(fail))

	out := scrape(t, m)

	for _, want := range []string{
		`gateway_grpc_client_calls_total{code="OK",method="/aggregator.Aggregator/GetGrading",target="aggregator"} 1`,
		`gateway_grpc_client_calls_total{code="Unavailable",method="/aggregator.Aggregator/GetGrading",target="aggregator"} 1`,
		`gateway_grpc_client_call_duration_seconds_count{method="/aggregator.Aggregator/GetGrading",target="aggregator"} 2`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func wrap(f func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error) grpc.UnaryInvoker {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		return f(ctx, method, req, reply, cc, opts...)
	}
}

func TestAdminCountersAndNilSafety(t *testing.T) {
	m := New()
	m.AdminDenied("not_admin")
	m.AdminDenied("not_admin")
	m.AdminUnlock("wrong_password")

	out := scrape(t, m)
	if !strings.Contains(out, `gateway_admin_denied_total{reason="not_admin"} 2`) ||
		!strings.Contains(out, `gateway_admin_unlock_total{result="wrong_password"} 1`) {
		t.Fatalf("counters missing:\n%s", out)
	}

	// A service built without metrics (tests, metrics off) must not crash.
	var none *Metrics
	none.ObserveHTTP("x", 200, 0)
	none.AdminDenied("x")
	none.AdminUnlock("x")
}

func TestServeIsOffWithoutAnAddress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	New().Serve(ctx, "") // returns at once, listens on nothing
}

func TestKnownAdminSeriesStartAtZero(t *testing.T) {
	out := scrape(t, New())

	for _, want := range []string{
		`gateway_admin_denied_total{reason="not_admin"} 0`,
		`gateway_admin_unlock_total{result="wrong_password"} 0`,
		`gateway_admin_unlock_total{result="ok"} 0`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

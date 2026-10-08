package http

import (
	"context"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gateway-service/internal/domain"
	"gateway-service/internal/series"
)

type seriesFake struct {
	adminFake

	publicName string
	adminName  string
	adminRange string
	result     series.Result
	err        error
}

func (s *seriesFake) PublicSeries(_ context.Context, _ string, name string) (series.Result, error) {
	s.publicName = name
	return s.result, s.err
}

func (s *seriesFake) AdminSeries(_ context.Context, name, rangeName string) (series.Result, error) {
	s.adminName, s.adminRange = name, rangeName
	return s.result, s.err
}

func getSeries(svc GatewayService, target string) *httptest.ResponseRecorder {
	mux := stdhttp.NewServeMux()
	NewHandler(svc).Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(stdhttp.MethodGet, target, nil))

	return rec
}

func TestPublicSeriesIsOpenCachedAndCORSOpen(t *testing.T) {
	f := &seriesFake{result: series.Result{Name: "gradings_per_day", Unit: "count", Series: []series.Line{{Label: "", Points: [][2]float64{{1, 2}}}}}}

	rec := getSeries(f, "/stats/series/gradings_per_day")

	if rec.Code != stdhttp.StatusOK || f.publicName != "gradings_per_day" {
		t.Fatalf("status %d, name %q", rec.Code, f.publicName)
	}

	if rec.Header().Get("Cache-Control") != "public, max-age=60" || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("headers = %v", rec.Header())
	}

	if !strings.Contains(rec.Body.String(), `"points":[[1,2]]`) {
		t.Fatalf("body = %s", rec.Body)
	}

	if len(f.authCalls) != 0 {
		t.Fatal("a public graph must not ask for an admin")
	}
}

func TestAdminSeriesNeedsAnAdminAndReadsTheRange(t *testing.T) {
	f := &seriesFake{result: series.Result{Name: "http_rate"}}

	f.authErr = domain.ErrForbidden
	if rec := getSeries(f, "/admin/metrics/http_rate?range=7d"); rec.Code != stdhttp.StatusForbidden || f.adminName != "" {
		t.Fatalf("not admin: %d, queried %q", rec.Code, f.adminName)
	}

	f.authErr = nil

	rec := getSeries(f, "/admin/metrics/http_rate?range=7d")
	if rec.Code != stdhttp.StatusOK || f.adminName != "http_rate" || f.adminRange != "7d" {
		t.Fatalf("admin: %d name %q range %q", rec.Code, f.adminName, f.adminRange)
	}

	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("admin headers = %v", rec.Header())
	}
}

func TestSeriesErrorsBecomeStatusAndCode(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{domain.ReasonError{Reason: "metrics_off", Err: fmt.Errorf("x: %w", domain.ErrUnavailable)}, stdhttp.StatusServiceUnavailable, "metrics_off"},
		{domain.ReasonError{Reason: "metrics_unavailable", Err: fmt.Errorf("x: %w", domain.ErrUnavailable)}, stdhttp.StatusServiceUnavailable, "metrics_unavailable"},
		{fmt.Errorf("%w: series", domain.ErrNotFound), stdhttp.StatusNotFound, ""},
		{fmt.Errorf("%w: range", domain.ErrInvalidRequest), stdhttp.StatusBadRequest, ""},
	}

	for _, tc := range cases {
		rec := getSeries(&seriesFake{err: tc.err}, "/stats/series/x")
		if rec.Code != tc.status {
			t.Errorf("%v: status %d, want %d", tc.err, rec.Code, tc.status)
		}

		if tc.code != "" && !strings.Contains(rec.Body.String(), `"code":"`+tc.code+`"`) {
			t.Errorf("%v: body %s lacks code %s", tc.err, rec.Body, tc.code)
		}
	}
}

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gateway-service/internal/domain"
	"gateway-service/internal/series"
)

type fakeSeries struct {
	result series.Result
	err    error
	calls  []string
}

func (f *fakeSeries) Query(_ context.Context, name string, public bool, rangeName string) (series.Result, error) {
	scope := "admin"
	if public {
		scope = "public"
	}

	f.calls = append(f.calls, scope+"/"+name+"/"+rangeName)

	return f.result, f.err
}

func seriesService(source SeriesSource) *GatewayService {
	svc := NewGatewayService(nil, nil, nil, allowAll{}, nil, nil, time.Second, time.Second, time.Second)
	svc.SetSeries(source)

	return svc
}

func TestSeriesPassThroughWithScopeAndRange(t *testing.T) {
	src := &fakeSeries{result: series.Result{Name: "x"}}
	svc := seriesService(src)

	if _, err := svc.PublicSeries(context.Background(), "1.2.3.4", "gradings_per_day"); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.AdminSeries(context.Background(), "http_rate", "7d"); err != nil {
		t.Fatal(err)
	}

	if len(src.calls) != 2 || src.calls[0] != "public/gradings_per_day/" || src.calls[1] != "admin/http_rate/7d" {
		t.Fatalf("calls = %v", src.calls)
	}
}

func TestSeriesErrorsKeepTheirClassAndReasonWord(t *testing.T) {
	cases := []struct {
		cause  error
		is     error
		reason string
	}{
		{series.ErrOff, domain.ErrUnavailable, "metrics_off"},
		{series.ErrUnavailable, domain.ErrUnavailable, "metrics_unavailable"},
		{series.ErrUnknown, domain.ErrNotFound, ""},
		{series.ErrBadRange, domain.ErrInvalidRequest, ""},
	}

	for _, tc := range cases {
		_, err := seriesService(&fakeSeries{err: tc.cause}).AdminSeries(context.Background(), "x", "")
		if !errors.Is(err, tc.is) {
			t.Errorf("%v: err = %v, want class %v", tc.cause, err, tc.is)
		}

		var reason domain.ReasonError

		got := ""
		if errors.As(err, &reason) {
			got = reason.Reason
		}

		if got != tc.reason {
			t.Errorf("%v: reason %q, want %q", tc.cause, got, tc.reason)
		}
	}
}

func TestGraphsAreOffWithoutASource(t *testing.T) {
	svc := seriesService(nil)

	var reason domain.ReasonError

	if _, err := svc.AdminSeries(context.Background(), "x", ""); !errors.As(err, &reason) || reason.Reason != "metrics_off" {
		t.Fatalf("err = %v", err)
	}
}

func TestPublicSeriesRespectsThePublicLimit(t *testing.T) {
	src := &fakeSeries{}
	svc := seriesService(src)
	svc.publicAPILimiter = denyAll{}

	if _, err := svc.PublicSeries(context.Background(), "1.2.3.4", "gradings_per_day"); err == nil || len(src.calls) != 0 {
		t.Fatalf("a limited caller must not reach the source: err %v calls %v", err, src.calls)
	}
}

type denyAll struct{}

func (denyAll) Allow(context.Context, string) (bool, error) { return false, nil }

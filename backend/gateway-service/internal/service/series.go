package service

import (
	"context"
	"errors"
	"fmt"

	"gateway-service/internal/domain"
	"gateway-service/internal/series"
)

// SeriesSource answers the graph series of the admin and Analytics tabs (Prometheus behind a cache).
type SeriesSource interface {
	Query(ctx context.Context, name string, public bool, rangeName string) (series.Result, error)
}

// SetSeries turns the graphs on; nil leaves them off (every graph request is then "metrics_off").
func (s *GatewayService) SetSeries(source SeriesSource) {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	s.series = source
}

// PublicSeries answers a graph of the Analytics tab: only series built from public gradings, behind the
// public API's per-IP limit.
func (s *GatewayService) PublicSeries(ctx context.Context, clientIP, name string) (series.Result, error) {
	if err := s.allowPublicAPI(ctx, clientIP); err != nil {
		return series.Result{}, err
	}

	return s.querySeries(ctx, name, true, "")
}

// AdminSeries answers a graph of the admin tab; the caller has already been checked as an admin.
func (s *GatewayService) AdminSeries(ctx context.Context, name, rangeName string) (series.Result, error) {
	return s.querySeries(ctx, name, false, rangeName)
}

func (s *GatewayService) querySeries(ctx context.Context, name string, public bool, rangeName string) (series.Result, error) {
	s.liveMu.RLock()
	source := s.series
	s.liveMu.RUnlock()
	if source == nil {
		return series.Result{}, domain.ReasonError{Reason: "metrics_off", Err: fmt.Errorf("graphs: %w", domain.ErrUnavailable)}
	}

	result, err := source.Query(ctx, name, public, rangeName)

	switch {
	case err == nil:
		return result, nil
	case errors.Is(err, series.ErrOff):
		return series.Result{}, domain.ReasonError{Reason: "metrics_off", Err: fmt.Errorf("graphs: %w", domain.ErrUnavailable)}
	case errors.Is(err, series.ErrUnavailable):
		return series.Result{}, domain.ReasonError{Reason: "metrics_unavailable", Err: fmt.Errorf("graphs: %w", domain.ErrUnavailable)}
	case errors.Is(err, series.ErrUnknown):
		return series.Result{}, fmt.Errorf("%w: series", domain.ErrNotFound)
	case errors.Is(err, series.ErrBadRange):
		return series.Result{}, fmt.Errorf("%w: range", domain.ErrInvalidRequest)
	default:
		return series.Result{}, fmt.Errorf("query series: %w", err)
	}
}

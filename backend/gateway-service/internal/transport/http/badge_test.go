package http

import (
	"errors"
	"fmt"
	stdhttp "net/http"
	"strings"
	"testing"

	"gateway-service/internal/domain"
)

func TestBadgeShowsStoredScore(t *testing.T) {
	score := int32(87)
	svc := &fakeService{score: &score}

	rec := serve(t, svc, "GET", "/badge/org/repo.svg", nil)

	if rec.Code != stdhttp.StatusOK || svc.badgeFor != "org/repo" {
		t.Fatalf("status = %d, repo = %q", rec.Code, svc.badgeFor)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/svg+xml") {
		t.Errorf("content type = %q", ct)
	}
	if rec.Header().Get("Cross-Origin-Resource-Policy") != "cross-origin" {
		t.Error("badge must be loadable cross-origin")
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=300" {
		t.Errorf("cache control = %q", cc)
	}
	body := rec.Body.String()
	if !strings.Contains(body, ">87<") || !strings.Contains(body, badgeColor(87)) || !strings.HasPrefix(body, "<svg") {
		t.Errorf("body = %s", body)
	}
}

func TestBadgeFallsBackToNoData(t *testing.T) {
	cases := map[string]*fakeService{
		"ungraded":    {},
		"unknown":     {err: fmt.Errorf("get public score rpc: %w", domain.ErrNotFound)},
		"unavailable": {err: fmt.Errorf("get public score rpc: %w", domain.ErrUnavailable)},
	}
	for name, svc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := serve(t, svc, "GET", "/badge/org/repo.svg", nil)
			if rec.Code != stdhttp.StatusOK || !strings.Contains(rec.Body.String(), ">n/a<") {
				t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
			}
			if errors.Is(svc.err, domain.ErrUnavailable) && rec.Header().Get("Cache-Control") != "public, max-age=60" {
				t.Errorf("error badge cache = %q, want the short one", rec.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestBadgeNeedsSvgSuffix(t *testing.T) {
	svc := &fakeService{}
	rec := serve(t, svc, "GET", "/badge/org/repo", nil)
	if rec.Code != stdhttp.StatusNotFound || svc.called {
		t.Fatalf("status = %d called = %v, want 404 without a lookup", rec.Code, svc.called)
	}
}

func TestBadgeColorsFollowHealthLevels(t *testing.T) {
	if badgeColor(90) == badgeColor(75) || badgeColor(75) == badgeColor(50) || badgeColor(50) == badgeColor(30) {
		t.Error("each health level needs its own color")
	}
}

package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type observed struct {
	pattern string
	status  int
}

func TestLoggingMiddlewareReportsThePatternNotThePath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /score/{org}/{repo}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	var seen []observed

	handler := loggingMiddleware(mux, func(pattern string, status int, _ time.Duration) {
		seen = append(seen, observed{pattern, status})
	})

	for _, path := range []string{"/score/secret-org/private-repo", "/no/such/route"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}

	if len(seen) != 2 {
		t.Fatalf("observed %d requests", len(seen))
	}

	if seen[0] != (observed{"GET /score/{org}/{repo}", 404}) {
		t.Errorf("first = %+v: the real slugs must not reach the metrics", seen[0])
	}

	// No route matched: an empty pattern, which the metrics label as "unmatched".
	if seen[1].pattern != "" || seen[1].status != 404 {
		t.Errorf("second = %+v", seen[1])
	}
}

package retry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newClient(attempts int, wait, attemptTimeout time.Duration) *http.Client {
	return &http.Client{Transport: &Transport{Attempts: attempts, Wait: wait, AttemptTimeout: attemptTimeout}}
}

// server answers with statuses[i] on call i, repeating the last one afterwards.
func server(t *testing.T, calls *atomic.Int32, statuses ...int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1)) - 1
		w.WriteHeader(statuses[min(n, len(statuses)-1)])
		io.WriteString(w, "body")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRetriesRateLimitThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := server(t, &calls, http.StatusTooManyRequests, http.StatusOK)

	resp, err := newClient(3, 10*time.Millisecond, time.Second).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "body" || calls.Load() != 2 {
		t.Fatalf("status=%d body=%q calls=%d", resp.StatusCode, body, calls.Load())
	}
}

func TestGivesUpAfterAttempts(t *testing.T) {
	var calls atomic.Int32
	srv := server(t, &calls, http.StatusServiceUnavailable)

	resp, err := newClient(3, time.Millisecond, time.Second).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || calls.Load() != 3 {
		t.Fatalf("status=%d calls=%d", resp.StatusCode, calls.Load())
	}
}

func TestDoesNotRetryClientErrors(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError} {
		var calls atomic.Int32
		srv := server(t, &calls, status)
		resp, err := newClient(3, time.Millisecond, time.Second).Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if calls.Load() != 1 {
			t.Fatalf("status %d: calls=%d, want 1", status, calls.Load())
		}
	}
}

func TestHonorsRetryAfter(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	start := time.Now()
	resp, err := newClient(3, 10*time.Millisecond, time.Second).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("retried after %v, want >= 1s", elapsed)
	}
}

func TestRetryAfterIsCapped(t *testing.T) {
	resp := &http.Response{Header: http.Header{"Retry-After": {"120"}}}
	if got := retryAfter(resp); got != maxRetryAfter {
		t.Fatalf("got %v, want %v", got, maxRetryAfter)
	}
}

func TestCancelledContextStopsWaiting(t *testing.T) {
	var calls atomic.Int32
	srv := server(t, &calls, http.StatusTooManyRequests)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)

	start := time.Now()
	_, err := newClient(3, 10*time.Second, time.Second).Do(req)
	if err == nil {
		t.Fatal("expected context error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("waited %v despite cancelled context", elapsed)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d, want 1", calls.Load())
	}
}

func TestAttemptTimeoutThenRetrySucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
			return
		}
		io.WriteString(w, "ok")
	}))
	defer srv.Close()

	resp, err := newClient(3, time.Millisecond, 100*time.Millisecond).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "ok" || calls.Load() != 2 {
		t.Fatalf("body=%q calls=%d", body, calls.Load())
	}
}

func TestPostWithBodyIsResent(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if string(b) != "payload" {
			t.Errorf("attempt %d got body %q", calls.Load()+1, b)
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer srv.Close()

	resp, err := newClient(3, time.Millisecond, time.Second).Post(srv.URL, "text/plain", strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if calls.Load() != 2 {
		t.Fatalf("calls=%d, want 2", calls.Load())
	}
}

func TestRateLimitBacksOffExponentiallyWithItsOwnBudget(t *testing.T) {
	var calls atomic.Int32
	var times []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		times = append(times, time.Now())
		if calls.Add(1) < 5 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := &http.Client{Transport: &Transport{Attempts: 3, RateLimitAttempts: 6, Wait: 20 * time.Millisecond, AttemptTimeout: time.Second}}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || calls.Load() != 5 {
		t.Fatalf("status=%d calls=%d, want 200 after 5 calls", resp.StatusCode, calls.Load())
	}
	for i, want := range []time.Duration{20, 40, 80, 160} {
		if gap := times[i+1].Sub(times[i]); gap < want*time.Millisecond {
			t.Errorf("gap %d = %s, want >= %dms", i, gap, want)
		}
	}
}

func TestOtherErrorsKeepTheShortBudget(t *testing.T) {
	var calls atomic.Int32
	srv := server(t, &calls, http.StatusServiceUnavailable)

	client := &http.Client{Transport: &Transport{Attempts: 3, RateLimitAttempts: 6, Wait: time.Millisecond, AttemptTimeout: time.Second}}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if calls.Load() != 3 {
		t.Fatalf("calls=%d, want 3", calls.Load())
	}
}

func TestBackoffIsCapped(t *testing.T) {
	if got := backoff(time.Second, 10); got != maxRateLimitWait {
		t.Fatalf("backoff = %s, want %s", got, maxRateLimitWait)
	}
	if got := backoff(time.Second, 2); got != 4*time.Second {
		t.Fatalf("backoff = %s, want 4s", got)
	}
}

// Package retry wraps an http.RoundTripper so every outbound API call survives rate limiting
// (429) and transient upstream/network failures without each call site handling it.
package retry

import (
	"context"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

// maxRetryAfter caps how long a server-sent Retry-After may stall a single retry.
const maxRetryAfter = 5 * time.Second

// Transport retries a request up to Attempts times in total. Each attempt gets its own
// AttemptTimeout, so the owning http.Client must not set Client.Timeout (it would cap all
// attempts together). Only 429/502/503/504 and transport errors are retried; every other status
// is a real answer from the API and is returned as is.
type Transport struct {
	Base           http.RoundTripper
	Attempts       int
	Wait           time.Duration
	AttemptTimeout time.Duration
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	attempts := max(t.Attempts, 1)
	// A consumed body can only be re-sent if it can be recreated.
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		attempts = 1
	}

	for attempt := 1; ; attempt++ {
		attemptReq := req
		if attempt > 1 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			attemptReq = req.Clone(req.Context())
			attemptReq.Body = body
		}

		resp, err := t.try(attemptReq)
		if attempt >= attempts || !retryable(resp, err) || req.Context().Err() != nil {
			return resp, err
		}

		wait := t.Wait
		if resp != nil {
			if ra := retryAfter(resp); ra > wait {
				wait = ra
			}
			log.Printf("retry: %s %s -> %d, attempt %d/%d", req.Method, req.URL.Path, resp.StatusCode, attempt, attempts)
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		} else {
			log.Printf("retry: %s %s -> %v, attempt %d/%d", req.Method, req.URL.Path, err, attempt, attempts)
		}

		timer := time.NewTimer(wait)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}
	}
}

// try runs one attempt under AttemptTimeout. The attempt's context is released when the response
// body is closed, not when RoundTrip returns, so the caller can still read the body.
func (t *Transport) try(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if t.AttemptTimeout <= 0 {
		return base.RoundTrip(req)
	}
	ctx, cancel := context.WithTimeout(req.Context(), t.AttemptTimeout)
	resp, err := base.RoundTrip(req.WithContext(ctx))
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

func retryable(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// retryAfter parses a delay-seconds Retry-After header, capped at maxRetryAfter.
func retryAfter(resp *http.Response) time.Duration {
	secs, err := strconv.Atoi(resp.Header.Get("Retry-After"))
	if err != nil || secs <= 0 {
		return 0
	}
	return min(time.Duration(secs)*time.Second, maxRetryAfter)
}

package app

import (
	"net/http"
	"sync/atomic"
)

// swapTransport forwards to the current transport. Config reloads build a new retry transport and
// swap it in, so the long-lived http.Client never changes while requests use it.
type swapTransport struct {
	current atomic.Pointer[http.RoundTripper]
}

func newSwapTransport(rt http.RoundTripper) *swapTransport {
	t := &swapTransport{}
	t.set(rt)
	return t
}

func (t *swapTransport) set(rt http.RoundTripper) { t.current.Store(&rt) }

func (t *swapTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return (*t.current.Load()).RoundTrip(req)
}

package loadtest

import (
	"fmt"
	"math/rand"
	"net/http"
	"sync"
)

// FlakyTransport wraps an http.RoundTripper and randomly fails a
// fraction of requests with a connection-level error — CO-505's own
// "random network drops" testing point. It never returns a malformed
// HTTP response (that would be a different, unrealistic failure mode);
// it fails the way a real dropped connection does, as an error from
// RoundTrip, which is exactly what internal/upload's and
// internal/pull's own retry logic is already written to handle (I3).
type FlakyTransport struct {
	// Next is the real transport to use when a request isn't chosen to
	// fail. Defaults to http.DefaultTransport if nil.
	Next http.RoundTripper
	// Rate is the probability (0..1) that any given request fails.
	Rate float64

	mu  sync.Mutex
	rnd *rand.Rand
}

// droppedConnectionError stands in for the real net.OpError a dropped
// TCP connection produces — internal/upload/internal/pull only care
// that RoundTrip returned a non-nil error, never its concrete type.
type droppedConnectionError struct{ method, url string }

func (e *droppedConnectionError) Error() string {
	return fmt.Sprintf("loadtest: simulated network drop on %s %s", e.method, e.url)
}

func (t *FlakyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.shouldDrop() {
		return nil, &droppedConnectionError{method: req.Method, url: req.URL.String()}
	}
	next := t.Next
	if next == nil {
		next = http.DefaultTransport
	}
	return next.RoundTrip(req)
}

func (t *FlakyTransport) shouldDrop() bool {
	if t.Rate <= 0 {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.rnd == nil {
		t.rnd = rand.New(rand.NewSource(rand.Int63())) // #nosec G404 -- load-test traffic shaping, not a security context
	}
	return t.rnd.Float64() < t.Rate
}

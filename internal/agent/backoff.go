package agent

import (
	"math/rand/v2"
	"time"
)

// CO-112: exponential backoff from a 5-second base to a 10-minute cap,
// with jitter so many sessions failing around the same time (e.g. the
// laptop just went offline) don't all retry in perfect lockstep — the
// actual mechanism behind "offline detection pauses the workers instead
// of spinning": between attempts, a session's worker isn't polling the
// network at all, it's just waiting for backoffDelay to elapse.
const (
	retryBaseDelay = 5 * time.Second
	retryMaxDelay  = 10 * time.Minute
)

// backoffDelay returns the delay before the next retry after attempt
// consecutive failures (attempt 1 is the first failure, so
// backoffDelay(1) is the wait before the second try). Grows
// exponentially (5s, 10s, 20s, 40s, ...), capped at retryMaxDelay, plus
// up to 50% jitter on top of whatever the capped value is.
func backoffDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 20 { // 2^20 * 5s already dwarfs retryMaxDelay many times over — avoids any overflow risk for a pathological attempt count
		shift = 20
	}

	d := retryBaseDelay * time.Duration(int64(1)<<uint(shift))
	if d > retryMaxDelay || d <= 0 {
		d = retryMaxDelay
	}

	jitter := time.Duration(rand.Int64N(int64(d)/2 + 1)) // #nosec G404 -- retry jitter, not a security context
	return d + jitter
}

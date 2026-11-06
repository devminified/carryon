package agent

import (
	"testing"
	"time"
)

// backoffFloor returns the pre-jitter value backoffDelay(attempt) is
// built from, so tests can assert bounds without depending on the exact
// (randomized) jitter amount.
func backoffFloor(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 20 {
		shift = 20
	}
	d := retryBaseDelay * time.Duration(int64(1)<<uint(shift))
	if d > retryMaxDelay || d <= 0 {
		d = retryMaxDelay
	}
	return d
}

func TestBackoffDelayWithinFloorAndFloorPlusJitter(t *testing.T) {
	for _, attempt := range []int{0, 1, 2, 3, 5, 10} {
		floor := backoffFloor(attempt)
		maxJitter := floor / 2

		for i := 0; i < 20; i++ { // jitter is randomized — sample several draws
			got := backoffDelay(attempt)
			if got < floor {
				t.Errorf("backoffDelay(%d) = %v, want >= floor %v", attempt, got, floor)
			}
			if got > floor+maxJitter {
				t.Errorf("backoffDelay(%d) = %v, want <= floor+jitter %v", attempt, got, floor+maxJitter)
			}
		}
	}
}

func TestBackoffDelayGrowsExponentially(t *testing.T) {
	if backoffFloor(1) != retryBaseDelay {
		t.Errorf("backoffFloor(1) = %v, want the base delay %v", backoffFloor(1), retryBaseDelay)
	}
	if backoffFloor(2) != 2*retryBaseDelay {
		t.Errorf("backoffFloor(2) = %v, want %v", backoffFloor(2), 2*retryBaseDelay)
	}
	if backoffFloor(3) != 4*retryBaseDelay {
		t.Errorf("backoffFloor(3) = %v, want %v", backoffFloor(3), 4*retryBaseDelay)
	}
}

func TestBackoffDelayCapsAtMaxDelay(t *testing.T) {
	// 2^(20-1) * 5s is many orders of magnitude past the 10-minute cap.
	if backoffFloor(20) != retryMaxDelay {
		t.Errorf("backoffFloor(20) = %v, want the cap %v", backoffFloor(20), retryMaxDelay)
	}
	// An absurd attempt count must never overflow or exceed the cap.
	got := backoffDelay(1_000_000)
	if got < retryMaxDelay || got > retryMaxDelay+retryMaxDelay/2 {
		t.Errorf("backoffDelay(1_000_000) = %v, want within [cap, cap*1.5]", got)
	}
}

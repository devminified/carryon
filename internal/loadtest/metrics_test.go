package loadtest

import (
	"errors"
	"testing"
	"time"
)

func TestMetricsRecordUploadAggregatesCountsAndBytes(t *testing.T) {
	m := &Metrics{}
	m.RecordUpload(10*time.Millisecond, 100, 2, nil)
	m.RecordUpload(20*time.Millisecond, 200, 0, errors.New("boom"))

	snap := m.Snapshot()
	if snap.UploadAttempts != 2 {
		t.Fatalf("UploadAttempts = %d, want 2", snap.UploadAttempts)
	}
	if snap.UploadErrors != 1 {
		t.Fatalf("UploadErrors = %d, want 1", snap.UploadErrors)
	}
	if snap.Redactions != 2 {
		t.Fatalf("Redactions = %d, want 2", snap.Redactions)
	}
	if snap.BytesUploaded != 300 {
		t.Fatalf("BytesUploaded = %d, want 300", snap.BytesUploaded)
	}
}

func TestMetricsRecordPullTracksRebuildMismatch(t *testing.T) {
	m := &Metrics{}
	m.RecordPull(5*time.Millisecond, 1000, nil, true)
	m.RecordPull(5*time.Millisecond, 1000, nil, false)
	m.RecordPull(5*time.Millisecond, 0, errors.New("network"), false)

	snap := m.Snapshot()
	if snap.PullAttempts != 3 {
		t.Fatalf("PullAttempts = %d, want 3", snap.PullAttempts)
	}
	if snap.PullErrors != 1 {
		t.Fatalf("PullErrors = %d, want 1", snap.PullErrors)
	}
	// Only the non-error, non-identical pull counts as a rebuild
	// mismatch — an error is a transport failure, not proof the
	// rebuild itself produced wrong bytes.
	if snap.RebuildMismatch != 1 {
		t.Fatalf("RebuildMismatch = %d, want 1", snap.RebuildMismatch)
	}
}

func TestPercentileEmptyIsZero(t *testing.T) {
	if got := percentile(nil, 0.5); got != 0 {
		t.Fatalf("percentile(nil) = %v, want 0", got)
	}
}

func TestPercentileP50AndP95(t *testing.T) {
	samples := []time.Duration{
		1 * time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond,
		4 * time.Millisecond, 5 * time.Millisecond, 6 * time.Millisecond,
		7 * time.Millisecond, 8 * time.Millisecond, 9 * time.Millisecond,
		10 * time.Millisecond,
	}
	p50 := percentile(samples, 0.50)
	p95 := percentile(samples, 0.95)
	if p50 < 4*time.Millisecond || p50 > 6*time.Millisecond {
		t.Fatalf("p50 = %v, want roughly the middle of the sample set", p50)
	}
	if p95 <= p50 {
		t.Fatalf("p95 (%v) should be >= p50 (%v)", p95, p50)
	}
	if p95 > 10*time.Millisecond {
		t.Fatalf("p95 = %v exceeds the largest sample", p95)
	}
}

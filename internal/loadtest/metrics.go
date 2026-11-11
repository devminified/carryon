package loadtest

import (
	"sort"
	"sync"
	"time"
)

// Metrics aggregates everything CO-505's testing points and cost
// extrapolation need, across every simulated developer and pull.
// Safe for concurrent use — one simulated developer per goroutine, plus
// the pull loop, all record into the same Metrics.
type Metrics struct {
	mu sync.Mutex

	uploadLatencies []time.Duration
	pullLatencies   []time.Duration

	uploadAttempts int
	uploadErrors   int // non-retried, i.e. still failed after internal/upload's own retries
	redactions     int
	bytesUploaded  int64

	pullAttempts    int
	pullErrors      int
	rebuildMismatch int // a pull that completed but didn't come back byte-identical (NFR-04 failure)
	bytesDownloaded int64
}

// RecordUpload records one agent.Uploader.Upload call's outcome.
func (m *Metrics) RecordUpload(d time.Duration, bytesSent int64, redactionsApplied int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.uploadAttempts++
	m.uploadLatencies = append(m.uploadLatencies, d)
	m.bytesUploaded += bytesSent
	m.redactions += redactionsApplied
	if err != nil {
		m.uploadErrors++
	}
}

// RecordPull records one simulated pull-and-rebuild's outcome.
// byteIdentical is false when the rebuilt bytes didn't match the
// source transcript exactly — NFR-04's own testing point.
func (m *Metrics) RecordPull(d time.Duration, bytesReceived int64, err error, byteIdentical bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pullAttempts++
	m.pullLatencies = append(m.pullLatencies, d)
	m.bytesDownloaded += bytesReceived
	if err != nil {
		m.pullErrors++
		return
	}
	if !byteIdentical {
		m.rebuildMismatch++
	}
}

// Snapshot is a point-in-time, read-only copy of Metrics — taken once,
// at the end of a run, so the report renderer never races with an
// in-flight simulation.
type Snapshot struct {
	UploadAttempts, UploadErrors, Redactions int
	BytesUploaded                            int64
	UploadLatencyP50, UploadLatencyP95       time.Duration

	PullAttempts, PullErrors, RebuildMismatch int
	BytesDownloaded                           int64
	PullLatencyP50, PullLatencyP95            time.Duration
}

func (m *Metrics) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Snapshot{
		UploadAttempts:   m.uploadAttempts,
		UploadErrors:     m.uploadErrors,
		Redactions:       m.redactions,
		BytesUploaded:    m.bytesUploaded,
		UploadLatencyP50: percentile(m.uploadLatencies, 0.50),
		UploadLatencyP95: percentile(m.uploadLatencies, 0.95),
		PullAttempts:     m.pullAttempts,
		PullErrors:       m.pullErrors,
		RebuildMismatch:  m.rebuildMismatch,
		BytesDownloaded:  m.bytesDownloaded,
		PullLatencyP50:   percentile(m.pullLatencies, 0.50),
		PullLatencyP95:   percentile(m.pullLatencies, 0.95),
	}
}

// percentile returns the p-th percentile (0 <= p <= 1) of samples,
// nearest-rank, with no interpolation — simple and sufficient for a
// load-test report, not a statistics library. Returns 0 for an empty
// input rather than panicking, since a run with zero pulls (e.g.
// pullsPerHour=0) must still produce a renderable report.
func percentile(samples []time.Duration, p float64) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(p * float64(len(sorted)-1))
	return sorted[idx]
}

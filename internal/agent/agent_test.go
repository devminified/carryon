package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingUploader records every Upload call, optionally blocking until
// released (for worker-pool concurrency tests) and optionally failing a
// configured number of times before succeeding.
type countingUploader struct {
	mu          sync.Mutex
	calls       map[string]int
	block       chan struct{} // if non-nil, Upload blocks on this until it's closed
	inFlight    int32
	maxInFlight int32
	failTimes   int // fail this many times per session before succeeding
	failed      map[string]int
}

func newCountingUploader() *countingUploader {
	return &countingUploader{calls: map[string]int{}}
}

func (u *countingUploader) Upload(ctx context.Context, sessionID string) error {
	n := atomic.AddInt32(&u.inFlight, 1)
	defer atomic.AddInt32(&u.inFlight, -1)
	for {
		cur := atomic.LoadInt32(&u.maxInFlight)
		if n <= cur || atomic.CompareAndSwapInt32(&u.maxInFlight, cur, n) {
			break
		}
	}

	u.mu.Lock()
	u.calls[sessionID]++
	shouldFail := u.failed[sessionID] < u.failTimes
	if shouldFail {
		u.failed[sessionID]++
	}
	u.mu.Unlock()

	if u.block != nil {
		// Deliberately does NOT also select on ctx.Done(): tests that use
		// block want precise control over exactly when Upload returns,
		// including proving Agent.Run() waits for it rather than
		// abandoning it the instant ctx is cancelled.
		<-u.block
	}

	if shouldFail {
		return fmt.Errorf("simulated upload failure")
	}
	return nil
}

func (u *countingUploader) count(sessionID string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls[sessionID]
}

func (u *countingUploader) totalCalls() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	n := 0
	for _, c := range u.calls {
		n += c
	}
	return n
}

func agentTestPaths(t *testing.T) (queuePath, cursorPath string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "queue.jsonl"), filepath.Join(dir, "cursor.json")
}

func appendHookEvent(t *testing.T, queuePath, sessionID, event string) {
	t.Helper()
	line := fmt.Sprintf(`{"session_id":%q,"transcript_path":"/tmp/t.jsonl","cwd":"/repo","event":%q,"ts":"2026-01-01T00:00:00Z"}`+"\n", sessionID, event)
	f, err := os.OpenFile(queuePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304,G302 -- test fixture, mirrors internal/hook's own append pattern
	if err != nil {
		t.Fatalf("open %s: %v", queuePath, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(line); err != nil {
		t.Fatalf("append %s: %v", queuePath, err)
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

func TestAgentDebouncesManyEventsIntoOneUpload(t *testing.T) {
	// CO-107 testing point: "Ten events for one session within 60 seconds
	// produce exactly one upload" — exercised here with a short interval
	// instead of a real 60s wait.
	queuePath, cursorPath := agentTestPaths(t)
	uploader := newCountingUploader()

	a, err := New(Config{
		QueuePath:      queuePath,
		CursorPath:     cursorPath,
		Uploader:       uploader,
		UploadInterval: 150 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
		WorkerPoolSize: 2,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	for i := 0; i < 10; i++ {
		appendHookEvent(t, queuePath, "s1", "Stop")
	}

	waitFor(t, 2*time.Second, func() bool { return uploader.count("s1") >= 1 })
	// Hold a bit past the debounce window to make sure a second upload
	// doesn't sneak in.
	time.Sleep(300 * time.Millisecond)

	if got := uploader.count("s1"); got != 1 {
		t.Errorf("upload count for s1 = %d, want exactly 1", got)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() returned error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after cancellation")
	}
}

func TestAgentSessionEndTriggersUploadQuickly(t *testing.T) {
	// CO-107 testing point: "A SessionEnd triggers an upload within 5
	// seconds." PollInterval is small here so the bound is easy to prove
	// tightly; production's 1s default still comfortably clears 5s.
	queuePath, cursorPath := agentTestPaths(t)
	uploader := newCountingUploader()

	a, err := New(Config{
		QueuePath:      queuePath,
		CursorPath:     cursorPath,
		Uploader:       uploader,
		UploadInterval: time.Hour, // large, so only SessionEnd's immediate path could trigger this
		PollInterval:   10 * time.Millisecond,
		WorkerPoolSize: 2,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = a.Run(ctx) }()

	start := time.Now()
	appendHookEvent(t, queuePath, "s1", "SessionEnd")

	waitFor(t, 1*time.Second, func() bool { return uploader.count("s1") >= 1 })
	elapsed := time.Since(start)
	if elapsed > 1*time.Second {
		t.Errorf("SessionEnd upload took %v, want well under the 5s budget", elapsed)
	}
}

func TestAgentWorkerPoolBoundsConcurrency(t *testing.T) {
	queuePath, cursorPath := agentTestPaths(t)
	uploader := newCountingUploader()
	uploader.block = make(chan struct{})

	const poolSize = 2
	a, err := New(Config{
		QueuePath:      queuePath,
		CursorPath:     cursorPath,
		Uploader:       uploader,
		UploadInterval: time.Hour,
		PollInterval:   10 * time.Millisecond,
		WorkerPoolSize: poolSize,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = a.Run(ctx) }()

	for i := 0; i < 5; i++ {
		appendHookEvent(t, queuePath, fmt.Sprintf("s%d", i), "SessionEnd")
	}

	waitFor(t, 2*time.Second, func() bool { return atomic.LoadInt32(&uploader.inFlight) == poolSize })
	// Give it a moment to prove it stays at the bound, not just touches it.
	time.Sleep(100 * time.Millisecond)
	if cur := atomic.LoadInt32(&uploader.inFlight); cur > poolSize {
		t.Errorf("in-flight uploads = %d, want at most %d", cur, poolSize)
	}

	close(uploader.block)
	waitFor(t, 2*time.Second, func() bool { return uploader.totalCalls() == 5 })

	if max := atomic.LoadInt32(&uploader.maxInFlight); max > poolSize {
		t.Errorf("max concurrent uploads observed = %d, want at most %d", max, poolSize)
	}

	cancel()
}

func TestAgentResumesAfterRestartWithoutLosingNewEvents(t *testing.T) {
	// CO-107 testing point: "Killing the agent mid-cycle and restarting it
	// resumes from the cursor with no lost or doubled events."
	queuePath, cursorPath := agentTestPaths(t)

	uploaderA := newCountingUploader()
	agentA, err := New(Config{
		QueuePath:      queuePath,
		CursorPath:     cursorPath,
		Uploader:       uploaderA,
		UploadInterval: 20 * time.Millisecond,
		PollInterval:   5 * time.Millisecond,
		WorkerPoolSize: 2,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctxA, cancelA := context.WithCancel(context.Background())
	doneA := make(chan error, 1)
	go func() { doneA <- agentA.Run(ctxA) }()

	appendHookEvent(t, queuePath, "s1", "SessionEnd")
	waitFor(t, 1*time.Second, func() bool { return uploaderA.count("s1") >= 1 })

	cancelA()
	select {
	case err := <-doneA:
		if err != nil {
			t.Errorf("agent A Run() returned error = %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("agent A did not stop")
	}

	// Simulate a hook firing while the agent is down.
	appendHookEvent(t, queuePath, "s2", "SessionEnd")

	uploaderB := newCountingUploader()
	agentB, err := New(Config{
		QueuePath:      queuePath,
		CursorPath:     cursorPath,
		Uploader:       uploaderB,
		UploadInterval: 20 * time.Millisecond,
		PollInterval:   5 * time.Millisecond,
		WorkerPoolSize: 2,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	go func() { _ = agentB.Run(ctxB) }()

	waitFor(t, 1*time.Second, func() bool { return uploaderB.count("s2") >= 1 })
}

func TestAgentGracefulShutdownWaitsForInFlightUpload(t *testing.T) {
	queuePath, cursorPath := agentTestPaths(t)
	uploader := newCountingUploader()
	uploader.block = make(chan struct{})

	a, err := New(Config{
		QueuePath:      queuePath,
		CursorPath:     cursorPath,
		Uploader:       uploader,
		UploadInterval: time.Hour,
		PollInterval:   5 * time.Millisecond,
		WorkerPoolSize: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	appendHookEvent(t, queuePath, "s1", "SessionEnd")
	waitFor(t, 1*time.Second, func() bool { return atomic.LoadInt32(&uploader.inFlight) == 1 })

	cancel()
	// Run must not return while the upload is still blocked in flight.
	select {
	case err := <-done:
		t.Fatalf("Run() returned (error = %v) before its in-flight upload finished", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(uploader.block) // let the uploader's ctx.Done() path (or completion) unblock it
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() returned error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after the in-flight upload finished")
	}
}

func TestNewValidatesRequiredFields(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Error("New(Config{}) = nil error, want an error (missing required fields)")
	}
	if _, err := New(Config{QueuePath: "q", CursorPath: "c"}); err == nil {
		t.Error("New() without an Uploader = nil error, want an error")
	}
}

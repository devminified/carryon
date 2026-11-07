package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock lets CO-112's backoff tests advance simulated time in
// controlled jumps, independent of PollInterval's real-wall-clock
// ticking — a poll tick fires constantly, but dispatchDue only acts once
// fakeClock.Now() has actually reached a session's dueAt.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// runAgent starts a.Run in the background and registers a cleanup that
// cancels it and waits for it to actually finish before the test
// returns — a fire-and-forget `go a.Run(ctx)` with no such wait races
// t.TempDir()'s own cleanup against whatever the agent's goroutine is
// still writing to queue.jsonl/the cursor file.
func runAgent(t *testing.T, a *Agent) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run() returned error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("agent did not stop within 2s of cancellation")
		}
	})
}

// countingUploader records every Upload call, optionally blocking until
// released (for worker-pool concurrency tests) and optionally failing a
// configured number of times before succeeding.
type countingUploader struct {
	mu          sync.Mutex
	calls       map[string]int
	lastFinal   map[string]bool // Session.Final as seen on the most recent call per session
	block       chan struct{}   // if non-nil, Upload blocks on this until it's closed
	inFlight    int32
	maxInFlight int32
	failTimes   int // fail this many times per session before succeeding
	failed      map[string]int

	// errFor, if set for a session ID, makes every call for that session
	// return the given error (checked before failTimes) — CO-112 tests
	// use this to make one session fail forever (fatally or just
	// retryably) while another succeeds normally.
	errFor map[string]error
}

func newCountingUploader() *countingUploader {
	return &countingUploader{calls: map[string]int{}, failed: map[string]int{}}
}

func (u *countingUploader) Upload(ctx context.Context, session Session) error {
	n := atomic.AddInt32(&u.inFlight, 1)
	defer atomic.AddInt32(&u.inFlight, -1)
	for {
		cur := atomic.LoadInt32(&u.maxInFlight)
		if n <= cur || atomic.CompareAndSwapInt32(&u.maxInFlight, cur, n) {
			break
		}
	}

	u.mu.Lock()
	u.calls[session.ID]++
	if u.lastFinal == nil {
		u.lastFinal = map[string]bool{}
	}
	u.lastFinal[session.ID] = session.Final
	if fixedErr, ok := u.errFor[session.ID]; ok {
		u.mu.Unlock()
		return fixedErr
	}
	shouldFail := u.failed[session.ID] < u.failTimes
	if shouldFail {
		u.failed[session.ID]++
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

func (u *countingUploader) final(sessionID string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.lastFinal[sessionID]
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

	runAgent(t, a)

	start := time.Now()
	appendHookEvent(t, queuePath, "s1", "SessionEnd")

	waitFor(t, 1*time.Second, func() bool { return uploader.count("s1") >= 1 })
	elapsed := time.Since(start)
	if elapsed > 1*time.Second {
		t.Errorf("SessionEnd upload took %v, want well under the 5s budget", elapsed)
	}
}

func TestAgentSetsSessionFinalOnlyForASessionEndDispatch(t *testing.T) {
	// CO-111: Agent must tell the Uploader which dispatch is covering a
	// SessionEnd event (Session.Final) — internal/upload uses it to call
	// end-session only after that upload succeeds. A normal debounced
	// upload must never be marked Final.
	queuePath, cursorPath := agentTestPaths(t)
	uploader := newCountingUploader()

	a, err := New(Config{
		QueuePath:      queuePath,
		CursorPath:     cursorPath,
		Uploader:       uploader,
		UploadInterval: 50 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
		WorkerPoolSize: 2,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	runAgent(t, a)

	appendHookEvent(t, queuePath, "s1", "Stop")
	waitFor(t, 1*time.Second, func() bool { return uploader.count("s1") >= 1 })
	if uploader.final("s1") {
		t.Error("a normal debounced upload was marked Final, want false")
	}

	appendHookEvent(t, queuePath, "s2", "SessionEnd")
	waitFor(t, 1*time.Second, func() bool { return uploader.count("s2") >= 1 })
	if !uploader.final("s2") {
		t.Error("the SessionEnd-triggered upload was not marked Final, want true")
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

	runAgent(t, a)

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

	runAgent(t, agentB)

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

func TestAgentRetryableFailureBacksOffExponentially(t *testing.T) {
	queuePath, cursorPath := agentTestPaths(t)
	uploader := newCountingUploader()
	uploader.failTimes = 1_000_000 // never succeeds within this test

	clock := newFakeClock(time.Now())
	a, err := New(Config{
		QueuePath:      queuePath,
		CursorPath:     cursorPath,
		Uploader:       uploader,
		UploadInterval: time.Hour,
		PollInterval:   5 * time.Millisecond,
		WorkerPoolSize: 2,
		now:            clock.Now,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	runAgent(t, a)

	appendHookEvent(t, queuePath, "s1", "SessionEnd")
	waitFor(t, 1*time.Second, func() bool { return uploader.count("s1") >= 1 })
	time.Sleep(20 * time.Millisecond) // let handleResult process the failure and set dueAt

	// First failure: attempt=1, dueAt = T0 + backoffDelay(1) — floor 5s,
	// up to +2.5s jitter (so somewhere in [5s, 7.5s] of simulated time).
	clock.Advance(4 * time.Second) // short of the 5s floor
	time.Sleep(50 * time.Millisecond)
	if got := uploader.count("s1"); got != 1 {
		t.Fatalf("count after advancing 4s (< 5s floor) = %d, want still 1", got)
	}

	clock.Advance(4 * time.Second) // total 8s — past the first window's max (7.5s)
	waitFor(t, 1*time.Second, func() bool { return uploader.count("s1") >= 2 })
	time.Sleep(20 * time.Millisecond)

	// Second failure: attempt=2, dueAt = T1 + backoffDelay(2) — floor
	// 10s, up to +5s jitter ([10s, 15s]) — strictly longer than the
	// first window, proving the delay actually grows.
	clock.Advance(9 * time.Second) // short of the 10s floor
	time.Sleep(50 * time.Millisecond)
	if got := uploader.count("s1"); got != 2 {
		t.Fatalf("count after advancing 9s (< 10s floor) = %d, want still 2", got)
	}

	clock.Advance(7 * time.Second) // total 16s since the second failure — past the window's max (15s)
	waitFor(t, 1*time.Second, func() bool { return uploader.count("s1") >= 3 })
}

func TestAgentFatalFailureBlocksSessionAndStopsAutomaticRetries(t *testing.T) {
	queuePath, cursorPath := agentTestPaths(t)
	uploader := newCountingUploader()
	uploader.errFor = map[string]error{"s1": fmt.Errorf("forbidden: %w", ErrFatal)}

	var logged []string
	var logMu sync.Mutex

	clock := newFakeClock(time.Now())
	a, err := New(Config{
		QueuePath:      queuePath,
		CursorPath:     cursorPath,
		Uploader:       uploader,
		UploadInterval: 30 * time.Millisecond,
		PollInterval:   5 * time.Millisecond,
		WorkerPoolSize: 2,
		now:            clock.Now,
		logf: func(msg string) {
			logMu.Lock()
			logged = append(logged, msg)
			logMu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	runAgent(t, a)

	appendHookEvent(t, queuePath, "s1", "SessionEnd")
	waitFor(t, 1*time.Second, func() bool { return uploader.count("s1") >= 1 })
	time.Sleep(50 * time.Millisecond)

	logMu.Lock()
	gotOneBlockedLog := len(logged) == 1 && strings.Contains(logged[0], "blocked")
	logMu.Unlock()
	if !gotOneBlockedLog {
		t.Fatalf("logged messages = %v, want exactly one containing %q", logged, "blocked")
	}

	// Advance the clock a long way — a blocked session has no retry
	// timer at all, so no amount of elapsed time should trigger another
	// call on its own.
	clock.Advance(1 * time.Hour)
	time.Sleep(100 * time.Millisecond)
	if got := uploader.count("s1"); got != 1 {
		t.Fatalf("count after advancing 1h past a fatal failure = %d, want still 1 (no timer-driven retry)", got)
	}

	// A fresh event re-arms it — a developer who keeps working still
	// gets another chance once something changes, without needing the
	// agent restarted.
	appendHookEvent(t, queuePath, "s1", "Stop")
	time.Sleep(20 * time.Millisecond) // let readNew observe it and set dueAt relative to the clock's current value
	clock.Advance(1 * time.Hour)      // past UploadInterval's debounce window
	waitFor(t, 1*time.Second, func() bool { return uploader.count("s1") >= 2 })
}

func TestAgentFatalErrorOnOneSessionDoesNotDelayAnother(t *testing.T) {
	// CO-112 testing point: "A 403 on one session doesn't delay uploads
	// for another."
	queuePath, cursorPath := agentTestPaths(t)
	uploader := newCountingUploader()
	uploader.errFor = map[string]error{"s1": fmt.Errorf("forbidden: %w", ErrFatal)}

	a, err := New(Config{
		QueuePath:      queuePath,
		CursorPath:     cursorPath,
		Uploader:       uploader,
		UploadInterval: time.Hour,
		PollInterval:   5 * time.Millisecond,
		WorkerPoolSize: 2,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	runAgent(t, a)

	appendHookEvent(t, queuePath, "s1", "SessionEnd")
	appendHookEvent(t, queuePath, "s2", "SessionEnd")

	waitFor(t, 1*time.Second, func() bool { return uploader.count("s1") >= 1 && uploader.count("s2") >= 1 })

	time.Sleep(100 * time.Millisecond)
	if got := uploader.count("s2"); got != 1 {
		t.Errorf("s2's upload count = %d, want exactly 1 (a fatal error on s1 must not touch it)", got)
	}
}

func TestAgentLongOfflineOutageCapsBackoffAndEventuallyDelivers(t *testing.T) {
	// NFR-11 / CO-112 testing point: "a 7-day offline simulation with a
	// time-warped clock keeps the queue intact." A fake clock advanced
	// in jumps well past the backoff cap simulates an outage many times
	// longer than any single retry window, at a scale that still runs
	// fast in CI — the mechanism being proven (the cap holds, and
	// delivery resumes the moment attempts start succeeding again) is
	// the same regardless of whether the simulated outage is hours or
	// exactly 7 days.
	queuePath, cursorPath := agentTestPaths(t)
	uploader := newCountingUploader()
	uploader.failTimes = 50 // "offline" for the first 50 attempts

	clock := newFakeClock(time.Now())
	a, err := New(Config{
		QueuePath:      queuePath,
		CursorPath:     cursorPath,
		Uploader:       uploader,
		UploadInterval: time.Hour,
		PollInterval:   2 * time.Millisecond,
		WorkerPoolSize: 2,
		now:            clock.Now,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	runAgent(t, a)

	appendHookEvent(t, queuePath, "s1", "SessionEnd")

	for i := 0; i < 50; i++ {
		before := uploader.count("s1")
		// 1s was too tight on a loaded/throttled CI runner (observed
		// flaking twice in a row on GitHub's hosted macOS runner under
		// -race, never locally) — any one of 50 iterations stalling
		// past a hair-trigger budget shouldn't fail a test that isn't
		// actually timing-sensitive; the real assertion is "eventually
		// fires again after the clock jump," not "within 1s of it."
		waitFor(t, 5*time.Second, func() bool { return uploader.count("s1") > before })
		time.Sleep(2 * time.Millisecond) // let handleResult set the next dueAt
		// Jump past even the maximum possible jittered delay (cap *
		// 1.5) — proves the cap holds no matter how long the simulated
		// outage runs; no attempt ever needs more than that to fire.
		clock.Advance(retryMaxDelay + retryMaxDelay/2 + time.Second)
	}

	// "Network restored" — the 51st attempt succeeds, with no special
	// recovery step needed beyond the same backoff schedule firing again.
	waitFor(t, 2*time.Second, func() bool { return uploader.count("s1") >= 51 })
}

func TestAgentRestartClearsBlockedStateAndRetriesFresh(t *testing.T) {
	// CO-112 testing point ("reboot the machine mid-backlog: no loss, no
	// duplicates") applied to the new blocked state specifically: a
	// restart must not leave a session permanently wedged just because
	// the previous process diagnosed it as fatal.
	queuePath, cursorPath := agentTestPaths(t)

	uploaderA := newCountingUploader()
	uploaderA.errFor = map[string]error{"s1": fmt.Errorf("forbidden: %w", ErrFatal)}

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
	time.Sleep(50 * time.Millisecond) // let it actually become blocked

	cancelA()
	select {
	case err := <-doneA:
		if err != nil {
			t.Errorf("agent A Run() returned error = %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("agent A did not stop")
	}

	// Simulate a reboot with a fresh process — nothing about the fake
	// backend needs to change for this to work.
	uploaderB := newCountingUploader()

	agentB, err := New(Config{
		QueuePath:      queuePath,
		CursorPath:     cursorPath,
		Uploader:       uploaderB,
		UploadInterval: 5 * time.Millisecond,
		PollInterval:   5 * time.Millisecond,
		WorkerPoolSize: 2,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	runAgent(t, agentB)

	waitFor(t, 1*time.Second, func() bool { return uploaderB.count("s1") >= 1 })
}

func TestNewValidatesRequiredFields(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Error("New(Config{}) = nil error, want an error (missing required fields)")
	}
	if _, err := New(Config{QueuePath: "q", CursorPath: "c"}); err == nil {
		t.Error("New() without an Uploader = nil error, want an error")
	}
}

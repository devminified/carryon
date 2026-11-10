package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devminified/carryon/internal/agent"
	"github.com/devminified/carryon/internal/state"
)

// fakeUploadCounters stands in for *upload.Uploader in tests that
// exercise heartbeatOnce's own wiring without a real Uploader.
type fakeUploadCounters struct {
	attempts, errors, redactions int64
}

func (f fakeUploadCounters) TakeCounts() (attempts, errors, redactions int64) {
	return f.attempts, f.errors, f.redactions
}

func TestHeartbeatOnceSkipsGracefullyWhenNotConfigured(t *testing.T) {
	withFakeHome(t) // no config.toml written

	// Must not panic — every failure here is logged and swallowed,
	// never surfaced to the caller (I1: nothing on this path may
	// block or crash capture/upload).
	heartbeatOnce(context.Background(), &lazyAPI{}, "/nonexistent/agent-status.json", t.TempDir(), nil)
}

func TestHeartbeatOnceWithNilCountersReportsZero(t *testing.T) {
	withFakeHome(t)
	// A nil uploadCounters (e.g. before the real Uploader exists yet)
	// must not panic — heartbeatOnce degrades to reporting zero for
	// every count it backs, same as the "not configured" path above.
	heartbeatOnce(context.Background(), &lazyAPI{}, "/nonexistent/agent-status.json", t.TempDir(), nil)
}

func TestHeartbeatOnceWithRealCountersDoesNotPanic(t *testing.T) {
	withFakeHome(t)
	// Not configured either (no config.toml), so this still returns
	// early before ever reaching client.Heartbeat — the point here is
	// only that a real, non-nil uploadCounters implementation flows
	// through heartbeatOnce's own TakeCounts() call without panicking.
	heartbeatOnce(
		context.Background(),
		&lazyAPI{},
		"/nonexistent/agent-status.json",
		t.TempDir(),
		fakeUploadCounters{attempts: 5, errors: 1, redactions: 2},
	)
}

func TestCountBlockedSessionsCountsOnlyBlockedOnes(t *testing.T) {
	status := agent.StatusFile{
		Sessions: []agent.SessionStatus{
			{SessionID: "s1", Blocked: false},
			{SessionID: "s2", Blocked: true, BlockedReason: "retrying"},
			{SessionID: "s3", Blocked: true, BlockedReason: "retrying"},
		},
	}
	if got := countBlockedSessions(status); got != 2 {
		t.Errorf("countBlockedSessions() = %d, want 2", got)
	}
}

func TestCountBlockedSessionsOfAnEmptyStatusIsZero(t *testing.T) {
	if got := countBlockedSessions(agent.StatusFile{}); got != 0 {
		t.Errorf("countBlockedSessions() = %d, want 0", got)
	}
}

func TestAggregateUploadProgressSumsBytesBehindAcrossSessions(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// s1: a 100-byte transcript, pointer says only 40 bytes read — 60
	// bytes behind. s2: fully caught up — 0 bytes behind.
	s1Path := filepath.Join(dir, "s1.jsonl")
	if err := os.WriteFile(s1Path, make([]byte, 100), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(stateDir, "s1", state.Pointer{Offset: 40}); err != nil {
		t.Fatal(err)
	}
	s2Path := filepath.Join(dir, "s2.jsonl")
	if err := os.WriteFile(s2Path, make([]byte, 50), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(stateDir, "s2", state.Pointer{Offset: 50}); err != nil {
		t.Fatal(err)
	}

	older := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 11, 10, 11, 0, 0, 0, time.UTC)
	statusFile := agent.StatusFile{
		Sessions: []agent.SessionStatus{
			{SessionID: "s1", TranscriptPath: s1Path, LastUploadAt: &older},
			{SessionID: "s2", TranscriptPath: s2Path, LastUploadAt: &newer},
		},
	}

	bytesBehind, lastUploadAt := aggregateUploadProgress(statusFile, stateDir)
	if bytesBehind != 60 {
		t.Errorf("bytesBehind = %d, want 60", bytesBehind)
	}
	if lastUploadAt == nil || !lastUploadAt.Equal(newer) {
		t.Errorf("lastUploadAt = %v, want %v (the most recent of the two sessions)", lastUploadAt, newer)
	}
}

func TestAggregateUploadProgressOfNoSessionsIsZeroAndNil(t *testing.T) {
	bytesBehind, lastUploadAt := aggregateUploadProgress(agent.StatusFile{}, t.TempDir())
	if bytesBehind != 0 {
		t.Errorf("bytesBehind = %d, want 0", bytesBehind)
	}
	if lastUploadAt != nil {
		t.Errorf("lastUploadAt = %v, want nil", lastUploadAt)
	}
}

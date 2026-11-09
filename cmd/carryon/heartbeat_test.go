package main

import (
	"context"
	"testing"

	"github.com/devminified/carryon/internal/agent"
)

func TestHeartbeatOnceSkipsGracefullyWhenNotConfigured(t *testing.T) {
	withFakeHome(t) // no config.toml written

	// Must not panic — every failure here is logged and swallowed,
	// never surfaced to the caller (I1: nothing on this path may
	// block or crash capture/upload).
	heartbeatOnce(context.Background(), &lazyAPI{}, "/nonexistent/agent-status.json")
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

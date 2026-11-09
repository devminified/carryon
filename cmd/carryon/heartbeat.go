package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/devminified/carryon/internal/agent"
	"github.com/devminified/carryon/internal/config"
)

// heartbeatInterval is CO-410's "last check-in ... per developer
// machine" — frequent enough that the admin Agents page reads as
// close to live, infrequent enough it's a non-event next to this
// laptop's real upload traffic.
const heartbeatInterval = 5 * time.Minute

// runHeartbeatLoop sends one heartbeat immediately, then once per
// heartbeatInterval, until ctx is cancelled — same fire-and-forget
// shape as runUpdateLoop (update.go's own doc comment), for the same
// reason: a missed or failed heartbeat has no partial-write state
// that needs a graceful shutdown barrier, so ctx cancellation alone
// is enough to stop it.
func runHeartbeatLoop(ctx context.Context, lazy *lazyAPI, statusPath string) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	heartbeatOnce(ctx, lazy, statusPath)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			heartbeatOnce(ctx, lazy, statusPath)
		}
	}
}

// heartbeatOnce sends one heartbeat. Every failure (not configured,
// not logged in, network down, rejected by the server) is logged and
// swallowed — a heartbeat is purely informational for the admin page;
// it must never affect capture or upload in any way (I1).
func heartbeatOnce(ctx context.Context, lazy *lazyAPI, statusPath string) {
	cfg, err := config.Load()
	if err != nil {
		slog.Warn("heartbeat: not configured yet, skipping this check-in", "error", err)
		return
	}

	client, err := lazy.client()
	if err != nil {
		slog.Warn("heartbeat: skipping this check-in", "error", err)
		return
	}
	token, err := lazy.accessToken(ctx)
	if err != nil {
		slog.Warn("heartbeat: skipping this check-in", "error", err)
		return
	}

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	status, err := agent.LoadStatus(statusPath)
	if err != nil {
		slog.Warn("heartbeat: could not read agent status, reporting 0 blocked sessions", "error", err)
	}

	if err := client.Heartbeat(ctx, token, cfg.MachineID, hostname, version, countBlockedSessions(status)); err != nil {
		slog.Warn("heartbeat: check-in failed", "error", err)
	}
}

// countBlockedSessions is a small, directly-testable extraction of
// heartbeatOnce's own "how behind is this machine" signal — see
// client.Heartbeat's own doc comment for why this, and not a
// synthetic queue depth.
func countBlockedSessions(status agent.StatusFile) int {
	n := 0
	for _, s := range status.Sessions {
		if s.Blocked {
			n++
		}
	}
	return n
}

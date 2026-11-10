package main

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"time"

	"github.com/devminified/carryon/internal/agent"
	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/config"
	"github.com/devminified/carryon/internal/status"
)

// heartbeatInterval is CO-410's "last check-in ... per developer
// machine" — frequent enough that the admin Agents page reads as
// close to live, infrequent enough it's a non-event next to this
// laptop's real upload traffic. CO-504's own ticket text says "every
// 15 minutes" in passing; this predates that ticket and isn't one of
// its testing points, so it stays as-is rather than changing already-
// shipped (CO-410) behavior for a line of prose.
const heartbeatInterval = 5 * time.Minute

// uploadCounters is the subset of *upload.Uploader heartbeatOnce
// needs — CO-504's "error counts and redaction counts," read (and
// reset to 0) once per heartbeat so each report covers only the
// interval since the last one. A narrow interface, not the concrete
// type, so tests can inject a fake without a real Uploader.
type uploadCounters interface {
	TakeCounts() (attempts, errors, redactions int64)
}

// runHeartbeatLoop sends one heartbeat immediately, then once per
// heartbeatInterval, until ctx is cancelled — same fire-and-forget
// shape as runUpdateLoop (update.go's own doc comment), for the same
// reason: a missed or failed heartbeat has no partial-write state
// that needs a graceful shutdown barrier, so ctx cancellation alone
// is enough to stop it. counters may be nil (e.g. before the real
// Uploader exists yet) — heartbeatOnce then just reports zero for
// every count it backs, never panics.
func runHeartbeatLoop(ctx context.Context, lazy *lazyAPI, statusPath, stateDir string, counters uploadCounters) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	heartbeatOnce(ctx, lazy, statusPath, stateDir, counters)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			heartbeatOnce(ctx, lazy, statusPath, stateDir, counters)
		}
	}
}

// heartbeatOnce sends one heartbeat. Every failure (not configured,
// not logged in, network down, rejected by the server) is logged and
// swallowed — a heartbeat is purely informational for the admin page;
// it must never affect capture or upload in any way (I1).
func heartbeatOnce(ctx context.Context, lazy *lazyAPI, statusPath, stateDir string, counters uploadCounters) {
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

	statusFile, err := agent.LoadStatus(statusPath)
	if err != nil {
		slog.Warn("heartbeat: could not read agent status, reporting 0 blocked sessions", "error", err)
	}

	bytesBehind, lastUploadAt := aggregateUploadProgress(statusFile, stateDir)

	var attempts, uploadErrors, redactions int64
	if counters != nil {
		attempts, uploadErrors, redactions = counters.TakeCounts()
	}

	in := api.HeartbeatInput{
		MachineID:         cfg.MachineID,
		Hostname:          hostname,
		Version:           version,
		OS:                runtime.GOOS,
		BlockedSessions:   countBlockedSessions(statusFile),
		BytesBehind:       bytesBehind,
		LastUploadAt:      lastUploadAt,
		UploadAttempts:    attempts,
		UploadErrors:      uploadErrors,
		RedactionsApplied: redactions,
	}
	if err := client.Heartbeat(ctx, token, in); err != nil {
		slog.Warn("heartbeat: check-in failed", "error", err)
	}
}

// countBlockedSessions is a small, directly-testable extraction of
// heartbeatOnce's own "how many sessions are stuck" signal.
func countBlockedSessions(status agent.StatusFile) int {
	n := 0
	for _, s := range status.Sessions {
		if s.Blocked {
			n++
		}
	}
	return n
}

// aggregateUploadProgress turns per-session scheduling state (what
// Agent itself tracks, in status.Sessions) plus the real transcript-
// vs-confirmed-offset cross-reference (internal/status.BytesBehind,
// the same one `carryon status` uses) into the two whole-machine
// figures CO-504's heartbeat reports: bytesBehind is the sum across
// every session (this machine's own "queue depth" and "bytes behind"
// collapse into one real number — there's no separate queue-item-
// count this codebase tracks anywhere, see go-conventions.md), and
// lastUploadAt is the most recent of every session's own last
// confirmed upload, nil if none have uploaded yet.
func aggregateUploadProgress(statusFile agent.StatusFile, stateDir string) (bytesBehind int64, lastUploadAt *time.Time) {
	for _, s := range statusFile.Sessions {
		bytesBehind += status.BytesBehind(stateDir, s.SessionID, s.TranscriptPath)
		if s.LastUploadAt != nil && (lastUploadAt == nil || s.LastUploadAt.After(*lastUploadAt)) {
			t := *s.LastUploadAt
			lastUploadAt = &t
		}
	}
	return bytesBehind, lastUploadAt
}

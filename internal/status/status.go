// Package status implements CO-113's `carryon status`: assembling one
// Report from every source the ticket names — internal/config for the
// backend URL, an AuthChecker for login state, internal/agent's own
// status file for agent/session state, internal/state's persisted
// pointers (cross-referenced against each session's live transcript
// size) for bytes behind, internal/filter's skipped-repos file, and
// internal/snapshot's own per-session state (CO-201's "large untracked
// files excluded, with a warning in status") — and rendering it either
// as stable JSON or as the human-readable text `carryon status` prints
// by default.
package status

import (
	"context"
	"os"
	"time"

	"github.com/devminified/carryon/internal/agent"
	"github.com/devminified/carryon/internal/auth"
	"github.com/devminified/carryon/internal/filter"
	"github.com/devminified/carryon/internal/project"
	"github.com/devminified/carryon/internal/snapshot"
	"github.com/devminified/carryon/internal/state"
)

// staleAfter is how long since the agent's last status-file write
// before it's treated as not actually running, rather than merely
// existing — comfortably more than a few of the agent's own default
// 1-second poll intervals, so a momentarily slow tick never produces a
// false "down" reading. A crashed or hung agent stops writing
// altogether, so its file ages past this quickly.
const staleAfter = 5 * time.Second

// Report is the full `carryon status --json` schema (CO-113: "the
// --json schema is stable and documented"). Every field here is part of
// that contract — changing a field's name or meaning is a breaking
// change for scripts that parse it.
type Report struct {
	LoggedIn     bool          `json:"logged_in"`
	User         *UserInfo     `json:"user,omitempty"`
	BackendURL   string        `json:"backend_url"`
	Agent        AgentInfo     `json:"agent"`
	Sessions     []SessionInfo `json:"sessions"`
	SkippedRepos []string      `json:"skipped_repos"`
}

// UserInfo is who's logged in. nil on Report when LoggedIn is false, or
// when fetching it failed (e.g. offline) — login state itself never
// depends on this succeeding.
type UserInfo struct {
	Email    string `json:"email"`
	UserName string `json:"user_name,omitempty"`
}

// AgentInfo is the capture agent daemon's own state. Version and
// UptimeSeconds are only meaningful when Running is true.
type AgentInfo struct {
	Running       bool   `json:"running"`
	Version       string `json:"version,omitempty"`
	UptimeSeconds int64  `json:"uptime_seconds,omitempty"`
}

// SessionInfo is one active session Agent currently knows about.
type SessionInfo struct {
	SessionID string `json:"session_id"`
	// Project is the session's canonical git remote — best-effort
	// (empty if it can no longer be resolved, e.g. the repo was moved).
	Project       string     `json:"project,omitempty"`
	LastUploadAt  *time.Time `json:"last_upload_at,omitempty"`
	BytesBehind   int64      `json:"bytes_behind"`
	Blocked       bool       `json:"blocked"`
	BlockedReason string     `json:"blocked_reason,omitempty"`
	// ExcludedLargeFiles (CO-201) lists untracked files over the
	// snapshot's 50MB limit that the most recent snapshot attempt left
	// out.
	ExcludedLargeFiles []string `json:"excluded_large_files,omitempty"`
}

// AuthChecker is the subset of *auth.Provider Gather needs — a small
// interface rather than the concrete type, so tests can inject a fake
// that never touches the keychain or makes a network call.
// *auth.Provider satisfies it as-is.
type AuthChecker interface {
	LoggedIn() bool
	CurrentUser(ctx context.Context) (*auth.User, error)
}

// Paths bundles the on-disk locations Gather reads. Every one is
// optional in the sense that a missing file is reported as "not
// running" / "nothing yet" — never an error.
type Paths struct {
	AgentStatusPath string // ~/.carryon/agent-status.json
	SkippedPath     string // ~/.carryon/skipped-repos.json
	StateDir        string // ~/.carryon/state
	SnapshotDir     string // ~/.carryon/snapshot-state
}

// Gather assembles a Report. It never returns an error — every source
// is best-effort, and a source that's unavailable (not logged in, agent
// not running, no state file yet) is a normal, reportable finding, not
// a failure of the status command itself.
func Gather(ctx context.Context, backendURL string, authChecker AuthChecker, paths Paths) Report {
	report := Report{
		LoggedIn:   authChecker.LoggedIn(),
		BackendURL: backendURL,
	}

	if report.LoggedIn {
		if u, err := authChecker.CurrentUser(ctx); err == nil {
			report.User = &UserInfo{Email: u.Email, UserName: u.UserName}
		}
	}

	statusFile, _ := agent.LoadStatus(paths.AgentStatusPath)
	report.Agent = AgentInfo{Version: statusFile.Version}
	if !statusFile.UpdatedAt.IsZero() && time.Since(statusFile.UpdatedAt) < staleAfter {
		report.Agent.Running = true
		report.Agent.UptimeSeconds = int64(time.Since(statusFile.StartedAt).Seconds())

		for _, s := range statusFile.Sessions {
			snap, _ := snapshot.Load(paths.SnapshotDir, s.SessionID)
			report.Sessions = append(report.Sessions, SessionInfo{
				SessionID:          s.SessionID,
				Project:            projectLabel(ctx, s.Cwd),
				LastUploadAt:       s.LastUploadAt,
				BytesBehind:        BytesBehind(paths.StateDir, s.SessionID, s.TranscriptPath),
				Blocked:            s.Blocked,
				BlockedReason:      s.BlockedReason,
				ExcludedLargeFiles: snap.ExcludedLargeFiles,
			})
		}
	}

	skipped, _ := filter.LoadSkipped(paths.SkippedPath)
	report.SkippedRepos = skipped.Repos

	return report
}

func projectLabel(ctx context.Context, cwd string) string {
	remote, err := project.ResolveRemote(ctx, cwd)
	if err != nil {
		return ""
	}
	return remote
}

// BytesBehind cross-references a session's live transcript size against
// internal/state's own persisted pointer — Agent itself never computes
// this (it deliberately knows nothing about transcript bytes; see
// internal/agent's package doc comment), so a fresh `carryon status`
// process does the join independently, reading the same durable state
// internal/upload already writes. Exported so cmd/carryon's own
// heartbeat loop (CO-504) can aggregate the same real number across
// every session into one "bytes behind" figure for the admin Agents
// page, instead of a second, divergent computation.
func BytesBehind(stateDir, sessionID, transcriptPath string) int64 {
	info, err := os.Stat(transcriptPath)
	if err != nil {
		return 0
	}
	p, err := state.Load(stateDir, sessionID)
	if err != nil {
		return 0
	}
	behind := info.Size() - p.Offset
	if behind < 0 {
		return 0
	}
	return behind
}

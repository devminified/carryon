package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"
)

// SessionStatus is one session's entry in StatusFile — CO-113's
// `carryon status` reads this to show per-session activity. It
// deliberately carries nothing byte-level (no "bytes behind") — Agent
// only tracks scheduling state, never transcript content or progress;
// `carryon status` itself cross-references TranscriptPath against
// internal/state's own persisted pointer to compute that.
type SessionStatus struct {
	SessionID      string     `json:"session_id"`
	Cwd            string     `json:"cwd"`
	TranscriptPath string     `json:"transcript_path"`
	LastUploadAt   *time.Time `json:"last_upload_at,omitempty"`
	Blocked        bool       `json:"blocked"`
	BlockedReason  string     `json:"blocked_reason,omitempty"`
}

// StatusFile is what Agent persists at Config.StatusPath
// (~/.carryon/agent-status.json in production) on every poll tick, so a
// separate `carryon status` invocation — a different OS process, which
// can't reach into this one's memory — has somewhere to read live agent
// state from. UpdatedAt is the liveness signal: a reader should treat the
// file as stale (the agent has likely crashed or hung) once it's older
// than a few poll intervals, not just trust its mere existence.
type StatusFile struct {
	UpdatedAt time.Time       `json:"updated_at"`
	Version   string          `json:"version"`
	StartedAt time.Time       `json:"started_at"`
	Sessions  []SessionStatus `json:"sessions"`
}

// LoadStatus reads the StatusFile at path. Returns the zero StatusFile,
// not an error, if the agent has never written one (never started, or a
// fresh install) — a missing file and a stale one are different findings
// a caller should report differently.
func LoadStatus(path string) (StatusFile, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is caller-controlled, our own computed ~/.carryon/agent-status.json or a test-controlled path
	if errors.Is(err, os.ErrNotExist) {
		return StatusFile{}, nil
	}
	if err != nil {
		return StatusFile{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var s StatusFile
	if err := json.Unmarshal(data, &s); err != nil {
		return StatusFile{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return s, nil
}

// writeStatus persists the current session snapshot atomically (temp
// file + rename, per I8), same as saveCursor. A no-op if StatusPath
// wasn't configured — existing callers/tests that don't care about
// status reporting don't need to set it.
func (a *Agent) writeStatus() {
	if a.cfg.StatusPath == "" {
		return
	}

	sessions := make([]SessionStatus, 0, len(a.sessions))
	for id, s := range a.sessions {
		var lastUploadAt *time.Time
		if !s.lastUploadAt.IsZero() {
			t := s.lastUploadAt
			lastUploadAt = &t
		}
		sessions = append(sessions, SessionStatus{
			SessionID:      id,
			Cwd:            s.cwd,
			TranscriptPath: s.transcriptPath,
			LastUploadAt:   lastUploadAt,
			Blocked:        s.blocked,
			BlockedReason:  s.blockedReason,
		})
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].SessionID < sessions[j].SessionID })

	file := StatusFile{
		UpdatedAt: a.cfg.now(),
		Version:   a.cfg.Version,
		StartedAt: a.startedAt,
		Sessions:  sessions,
	}

	data, err := json.Marshal(file)
	if err != nil {
		return // best-effort, matching persistCursor's own error handling
	}
	tmp := a.cfg.StatusPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil { // #nosec G304,G306 -- path is our own computed ~/.carryon/agent-status.json, or a test-controlled path
		return
	}
	if err := os.Rename(tmp, a.cfg.StatusPath); err != nil {
		_ = os.Remove(tmp)
	}
}

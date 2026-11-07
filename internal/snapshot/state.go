package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Pointer is one session's persisted snapshot state, written to
// <StateDir>/<session_id>.json. Load/Save follow the exact same
// atomic-write pattern as internal/state and internal/agent's cursor —
// a crash mid-write never leaves a corrupt or partial pointer file.
type Pointer struct {
	// TreeSHA is the last snapshot's tree object — comparing against a
	// freshly computed write-tree is what makes "an unchanged tree
	// makes no push and no new row" (CO-201's testing point) cheap and
	// exact, without needing to ask the server.
	TreeSHA string `json:"tree_sha"`
	// NextSeq is the sequence number the next *actual* snapshot (one
	// that changes the tree) should register with.
	NextSeq int `json:"next_seq"`
	// LastAttemptAt is when MaybeSnapshot last ran for this session,
	// regardless of whether the tree had changed — it's what the
	// 5-minute-per-session rate limit is measured from.
	LastAttemptAt time.Time `json:"last_attempt_at"`
	// ExcludedLargeFiles lists untracked files over the 50MB limit that
	// the most recent snapshot attempt left out — carryon status (CO-
	// 113) surfaces this as a warning (see .claude/rules referenced in
	// this package's doc comment).
	ExcludedLargeFiles []string `json:"excluded_large_files,omitempty"`
}

// Load reads the pointer stored for sessionID, or the zero Pointer
// (meaning "never snapshotted") if nothing is stored yet.
func Load(dir, sessionID string) (Pointer, error) {
	path := filepath.Join(dir, sessionID+".json")
	data, err := os.ReadFile(path) // #nosec G304 -- dir/sessionID are caller-controlled, composed from our own ~/.carryon/snapshot-state and a session ID
	if errors.Is(err, os.ErrNotExist) {
		return Pointer{}, nil
	}
	if err != nil {
		return Pointer{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var p Pointer
	if err := json.Unmarshal(data, &p); err != nil {
		return Pointer{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return p, nil
}

// Save atomically persists p for sessionID (temp file + rename, I8).
func Save(dir, sessionID string, p Pointer) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("encoding snapshot pointer for %s: %w", sessionID, err)
	}

	path := filepath.Join(dir, sessionID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil { // #nosec G304,G306 -- path is caller-controlled, composed from our own ~/.carryon/snapshot-state and a session ID
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming %s to %s: %w", tmp, path, err)
	}
	return nil
}

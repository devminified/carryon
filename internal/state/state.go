// Package state persists per-stream read pointers (CO-109): how far
// internal/reader has read into one JSONL stream — a session's main
// transcript, or one of its subagent children — and what chunk sequence
// number comes next, so a restarted agent resumes exactly where it left
// off instead of re-uploading or skipping anything.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Pointer is one stream's persisted state, written to
// ~/.carryon/state/<key>.json.
type Pointer struct {
	// Offset is the byte offset into the stream file already read.
	Offset int64 `json:"offset"`
	// NextSeq is the chunk sequence number the next successful upload
	// should use. Reader never advances this itself — only the caller,
	// after confirming a chunk upload actually succeeded (CO-110), so a
	// crash between upload and state write replays the same seq instead
	// of skipping or double-assigning one.
	NextSeq int `json:"next_seq"`
	// FileSize is the stream file's size as of this pointer, used
	// together with HeadHash to detect the file was truncated or
	// rewritten since.
	FileSize int64 `json:"file_size"`
	// HeadHash is the hex-encoded SHA-256 of the stream file's first 4KB
	// as of this pointer. Empty means "never read" — distinct from the
	// hash of an empty or small real file, which is still a real hash.
	HeadHash string `json:"head_hash"`
}

// SubagentKey returns the storage key for one subagent child stream of
// sessionID — a sibling of sessionID's own key (sessionID itself), not
// nested under it, so both are plain files directly in the state
// directory.
func SubagentKey(sessionID, agentID string) string {
	return sessionID + ".subagent-" + agentID
}

// Load reads the pointer stored for key, or the zero Pointer (meaning
// "never read") if nothing is stored yet.
func Load(dir, key string) (Pointer, error) {
	path := filepath.Join(dir, key+".json")
	data, err := os.ReadFile(path) // #nosec G304 -- dir/key are caller-controlled, composed from our own ~/.carryon/state and a session/agent ID
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

// Save atomically persists p for key (temp file plus rename), so a crash
// mid-write never leaves a corrupt or partial pointer file.
func Save(dir, key string, p Pointer) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("encoding pointer for %s: %w", key, err)
	}

	path := filepath.Join(dir, key+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil { // #nosec G304,G306 -- path is caller-controlled, composed from our own ~/.carryon/state and a session/agent ID
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming %s to %s: %w", tmp, path, err)
	}
	return nil
}

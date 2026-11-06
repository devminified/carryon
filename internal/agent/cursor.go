package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// cursorState is persisted at Config.CursorPath (~/.carryon/agent-cursor.json
// in production) so a restarted agent resumes tailing queue.jsonl from
// where it left off, per CO-107's "no lost or doubled events" testing
// point.
//
// Offset is NOT simply "how much of the file we've read" — reading a line
// only means we've noticed a session is dirty, not that it's been
// uploaded. Offset is the watermark described in I4 ("a contiguous
// watermark"): the earliest byte offset of any queue line whose session
// hasn't yet had a successful upload dispatched covering it. Everything
// before Offset is safe to skip on restart (and safe to drop during
// compaction); everything at or after it must be re-read, because losing
// it would lose a dirty-session notification with no other record of it.
type cursorState struct {
	Offset int64 `json:"offset"`
}

func loadCursor(path string) (cursorState, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is our own computed ~/.carryon/agent-cursor.json, or a test-controlled path
	if errors.Is(err, os.ErrNotExist) {
		return cursorState{}, nil
	}
	if err != nil {
		return cursorState{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var c cursorState
	if err := json.Unmarshal(data, &c); err != nil {
		return cursorState{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return c, nil
}

// saveCursor writes c atomically (temp file + rename, per I8: crash-safe
// state), so a crash mid-write never leaves a corrupt cursor file.
func saveCursor(path string, c cursorState) error {
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("encoding cursor: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil { // #nosec G304,G306,G703 -- path is our own computed ~/.carryon/agent-cursor.json, or a test-controlled path
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming %s to %s: %w", tmp, path, err)
	}
	return nil
}

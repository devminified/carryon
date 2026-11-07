// CO-301: persists a Redactor's in-flight tool_use -> denylisted-
// basename map (internal/redact.Redactor.Pending/SetPending) across
// agent restarts and across separate Upload calls for the same stream
// — a tool call can finish well after the poll tick that uploaded its
// tool_use line. Kept as its own file (<key>.redact.json) rather than a
// new field on Pointer: Pointer's tests compare it with == in several
// places, which a map field would break, and this is a genuinely
// separate piece of state with its own lifecycle, not an extension of
// "how far has this stream been read."
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// LoadRedactPending reads the pending map stored for key, or an empty
// map (never nil) if nothing is stored yet.
func LoadRedactPending(dir, key string) (map[string]string, error) {
	path := filepath.Join(dir, key+".redact.json")
	data, err := os.ReadFile(path) // #nosec G304 -- dir/key are caller-controlled, composed from our own ~/.carryon/state and a session/agent ID
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var pending map[string]string
	if err := json.Unmarshal(data, &pending); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if pending == nil {
		pending = map[string]string{}
	}
	return pending, nil
}

// SaveRedactPending atomically persists pending for key (temp file plus
// rename, same as Save), so a crash mid-write never leaves a corrupt or
// partial map. An empty map is still written (not skipped) so a
// previously-pending entry that's since resolved is correctly cleared
// on disk too.
func SaveRedactPending(dir, key string, pending map[string]string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	data, err := json.Marshal(pending)
	if err != nil {
		return fmt.Errorf("encoding redact-pending for %s: %w", key, err)
	}

	path := filepath.Join(dir, key+".redact.json")
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

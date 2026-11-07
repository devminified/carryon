package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// bundleFallbackWindow is CO-202's "remember the fallback per repo for
// 24 hours so the push isn't retried on every snapshot" — once a push
// to refs/carryon/* is rejected for a given repo, MaybeSnapshot skips
// straight to the bundle path for this long before trying the ref push
// again.
const bundleFallbackWindow = 24 * time.Hour

// repoFallback is persisted per repo (keyed by its canonical remote,
// not by session — unlike Pointer) because the rejection reason is a
// property of the remote itself (a pre-receive hook blocking
// refs/carryon/*, no push permission, ...), not of any one session.
// Two sessions in the same repo must share this state.
type repoFallback struct {
	LastRejectedAt time.Time `json:"last_rejected_at"`
}

// repoStateFilename hashes remote so it's always a single safe path
// component, regardless of what characters the remote URL itself
// contains.
func repoStateFilename(remote string) string {
	sum := sha256.Sum256([]byte(remote))
	return hex.EncodeToString(sum[:]) + ".json"
}

func loadRepoFallback(dir, remote string) (repoFallback, error) {
	path := filepath.Join(dir, repoStateFilename(remote))
	data, err := os.ReadFile(path) // #nosec G304 -- path is derived from a hash of caller-controlled input, composed from our own ~/.carryon/snapshot-repo-state
	if errors.Is(err, os.ErrNotExist) {
		return repoFallback{}, nil
	}
	if err != nil {
		return repoFallback{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var f repoFallback
	if err := json.Unmarshal(data, &f); err != nil {
		return repoFallback{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return f, nil
}

func saveRepoFallback(dir, remote string, f repoFallback) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("encoding repo fallback state: %w", err)
	}

	path := filepath.Join(dir, repoStateFilename(remote))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil { // #nosec G304,G306 -- path is derived from a hash of caller-controlled input, composed from our own ~/.carryon/snapshot-repo-state
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming %s to %s: %w", tmp, path, err)
	}
	return nil
}

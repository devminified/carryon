package filter

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"
)

// SkippedFile is what Filter persists at Config.SkippedPath
// (~/.carryon/skipped-repos.json in production) — CO-113's `carryon
// status` reads this to show "recently skipped repos" alongside the
// agent's own status file. A separate file, not merged into Agent's own
// StatusFile, because Filter and Agent are different layers (Filter
// wraps Agent's Uploader, not the other way around — see
// .claude/rules/architecture.md) with no shared mutable state to write
// through safely; each owns and writes its own file.
type SkippedFile struct {
	UpdatedAt time.Time `json:"updated_at"`
	Repos     []string  `json:"repos"`
}

// LoadSkipped reads the SkippedFile at path. Returns the zero value, not
// an error, if Filter has never written one (nothing skipped yet, or a
// fresh install).
func LoadSkipped(path string) (SkippedFile, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is caller-controlled, our own computed ~/.carryon/skipped-repos.json or a test-controlled path
	if errors.Is(err, os.ErrNotExist) {
		return SkippedFile{}, nil
	}
	if err != nil {
		return SkippedFile{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var s SkippedFile
	if err := json.Unmarshal(data, &s); err != nil {
		return SkippedFile{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return s, nil
}

// writeSkipped persists the current warnedRemotes set atomically (temp
// file + rename, per I8). Called from warnOnce, under f.mu — the same
// lock that already guards warnedRemotes — so this never races its own
// writes. A no-op if SkippedPath wasn't configured.
func (f *Filter) writeSkipped() {
	if f.skippedPath == "" {
		return
	}

	repos := make([]string, 0, len(f.warnedRemotes))
	for r := range f.warnedRemotes {
		repos = append(repos, r)
	}
	sort.Strings(repos)

	data, err := json.Marshal(SkippedFile{UpdatedAt: f.now(), Repos: repos})
	if err != nil {
		return
	}
	tmp := f.skippedPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil { // #nosec G304,G306 -- path is our own computed ~/.carryon/skipped-repos.json, or a test-controlled path
		return
	}
	if err := os.Rename(tmp, f.skippedPath); err != nil {
		_ = os.Remove(tmp)
	}
}

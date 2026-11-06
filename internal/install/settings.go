// Package install implements CO-106: safely merging Carryon's hooks into
// Claude Code's ~/.claude/settings.json, and reversing that merge on
// uninstall.
package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// HookEntry is one command Claude Code runs for a given hook event.
type HookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// HookGroup is one matcher's worth of hooks for a given event. Stop and
// SessionEnd don't use matchers (there's no tool call to match against),
// so Carryon's own groups omit it — this also means Carryon's group never
// collides with, or needs to merge into, another tool's matcher-specific
// group.
type HookGroup struct {
	Matcher string      `json:"matcher,omitempty"`
	Hooks   []HookEntry `json:"hooks"`
}

const (
	carryonHookCommand      = "carryon hook"
	carryonHookFinalCommand = "carryon hook --final"
	hookTimeoutSeconds      = 5

	// minCleanupPeriodDays is the floor CO-106 raises cleanupPeriodDays to,
	// so Claude Code never cleans up a transcript before the agent (CO-107)
	// has uploaded it. It only ever raises, never lowers, an existing
	// value.
	minCleanupPeriodDays = 3650
)

// InstallState records what InstallSettings changed, so UninstallSettings
// can reverse exactly that and nothing more. Stored at
// ~/.carryon/install-state.json — Carryon's own directory, not inside
// ~/.claude, which belongs to Claude Code.
type InstallState struct {
	// SettingsExisted is false when InstallSettings created settings.json
	// from scratch; UninstallSettings then removes the file entirely
	// instead of restoring a backup.
	SettingsExisted bool `json:"settings_existed"`
	// BackupPath is settings.json's pre-install content, saved at
	// settings.json.carryon-bak-<timestamp>. Empty when SettingsExisted is
	// false.
	BackupPath string `json:"backup_path,omitempty"`
}

// InstallSettings merges Carryon's Stop/SessionEnd hooks and a raised
// cleanupPeriodDays into the settings.json at settingsPath, preserving
// every other key and every existing hook. It backs up the pre-merge
// content and records what it did at installStatePath, so
// UninstallSettings can reverse it later.
//
// Idempotent: if settings.json is already fully merged (both hooks
// present, cleanupPeriodDays already high enough), this is a complete
// no-op — no backup file, no write, no change to installStatePath.
//
// Refuses (returns an error, touching nothing) if settingsPath contains
// invalid JSON, or isn't writable.
func InstallSettings(settingsPath, installStatePath string) error {
	existed := true
	original, err := os.ReadFile(settingsPath) // #nosec G304 -- settingsPath is ~/.claude/settings.json, resolved from the user's own home directory, or a test-controlled path
	switch {
	case errors.Is(err, os.ErrNotExist):
		existed = false
		original = []byte("{}")
	case err != nil:
		return fmt.Errorf("reading %s: %w", settingsPath, err)
	default:
		if err := checkWritable(settingsPath); err != nil {
			return err
		}
	}

	doc := map[string]json.RawMessage{}
	if err := json.Unmarshal(original, &doc); err != nil {
		return fmt.Errorf("%s contains invalid JSON, refusing to touch it: %w", settingsPath, err)
	}

	changed, err := mergeSettings(doc)
	if err != nil {
		return fmt.Errorf("merging %s: %w", settingsPath, err)
	}
	if !changed {
		return nil
	}

	merged, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", settingsPath, err)
	}
	merged = append(merged, '\n')

	var backupPath string
	if existed {
		backupPath = settingsPath + ".carryon-bak-" + time.Now().UTC().Format("20060102150405")
		if err := os.WriteFile(backupPath, original, 0o600); err != nil { // #nosec G304,G306,G703 -- backupPath is settingsPath (caller-controlled, our own computed ~/.claude/settings.json or a test path) with a fixed suffix appended, not externally tainted; perm matches settings.json's own 0600
			return fmt.Errorf("backing up %s: %w", settingsPath, err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(settingsPath), err)
	}
	if err := writeAtomic(settingsPath, merged, 0o600); err != nil {
		return err
	}

	state := InstallState{SettingsExisted: existed, BackupPath: backupPath}
	stateBytes, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encoding install state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(installStatePath), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(installStatePath), err)
	}
	if err := os.WriteFile(installStatePath, stateBytes, 0o600); err != nil { // #nosec G306,G304 -- installStatePath is our own computed ~/.carryon/install-state.json, or a test-controlled path
		return fmt.Errorf("writing install state: %w", err)
	}
	return nil
}

// UninstallSettings reverses InstallSettings, using installStatePath's
// record: restores the pre-install backup verbatim, or removes
// settings.json entirely if it didn't exist before. This restores the
// exact pre-install content as long as nothing else has touched
// settings.json since (CO-106's testing point) — it is not a general
// three-way merge.
//
// A missing installStatePath means there's nothing to reverse (never
// installed, or already uninstalled) — treated as success.
func UninstallSettings(settingsPath, installStatePath string) error {
	data, err := os.ReadFile(installStatePath) // #nosec G304 -- installStatePath is our own computed ~/.carryon/install-state.json, or a test-controlled path
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading install state: %w", err)
	}

	var state InstallState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parsing install state: %w", err)
	}

	if !state.SettingsExisted {
		if err := os.Remove(settingsPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removing %s: %w", settingsPath, err)
		}
	} else {
		backup, err := os.ReadFile(state.BackupPath) // #nosec G304 -- path InstallSettings wrote itself, recorded in installStatePath
		if err != nil {
			return fmt.Errorf("reading backup %s: %w", state.BackupPath, err)
		}
		if err := writeAtomic(settingsPath, backup, 0o600); err != nil {
			return err
		}
		if err := os.Remove(state.BackupPath); err != nil {
			return fmt.Errorf("removing backup %s: %w", state.BackupPath, err)
		}
	}

	if err := os.Remove(installStatePath); err != nil {
		return fmt.Errorf("removing install state: %w", err)
	}
	return nil
}

// mergeSettings mutates doc in place, adding Carryon's hooks and raising
// cleanupPeriodDays. It reports whether anything actually changed.
func mergeSettings(doc map[string]json.RawMessage) (changed bool, err error) {
	hooksChanged, err := mergeHooks(doc)
	if err != nil {
		return false, err
	}
	cleanupChanged, err := raiseCleanupPeriod(doc)
	if err != nil {
		return false, err
	}
	return hooksChanged || cleanupChanged, nil
}

func mergeHooks(doc map[string]json.RawMessage) (bool, error) {
	hooks := map[string]json.RawMessage{}
	if raw, ok := doc["hooks"]; ok {
		if err := json.Unmarshal(raw, &hooks); err != nil {
			return false, fmt.Errorf(`parsing "hooks": %w`, err)
		}
	}

	wanted := []struct{ event, command string }{
		{"Stop", carryonHookCommand},
		{"SessionEnd", carryonHookFinalCommand},
	}

	changed := false
	for _, w := range wanted {
		var groups []HookGroup
		if raw, ok := hooks[w.event]; ok {
			if err := json.Unmarshal(raw, &groups); err != nil {
				return false, fmt.Errorf("parsing \"hooks.%s\": %w", w.event, err)
			}
		}

		if hasCommand(groups, w.command) {
			continue
		}

		groups = append(groups, HookGroup{
			Hooks: []HookEntry{{Type: "command", Command: w.command, Timeout: hookTimeoutSeconds}},
		})
		raw, err := json.Marshal(groups)
		if err != nil {
			return false, fmt.Errorf("marshaling \"hooks.%s\": %w", w.event, err)
		}
		hooks[w.event] = raw
		changed = true
	}

	if !changed {
		return false, nil
	}
	raw, err := json.Marshal(hooks)
	if err != nil {
		return false, fmt.Errorf(`marshaling "hooks": %w`, err)
	}
	doc["hooks"] = raw
	return true, nil
}

func hasCommand(groups []HookGroup, command string) bool {
	for _, g := range groups {
		for _, h := range g.Hooks {
			if h.Command == command {
				return true
			}
		}
	}
	return false
}

func raiseCleanupPeriod(doc map[string]json.RawMessage) (bool, error) {
	current := 0
	if raw, ok := doc["cleanupPeriodDays"]; ok {
		if err := json.Unmarshal(raw, &current); err != nil {
			return false, fmt.Errorf(`parsing "cleanupPeriodDays": %w`, err)
		}
	}
	if current >= minCleanupPeriodDays {
		return false, nil
	}
	raw, err := json.Marshal(minCleanupPeriodDays)
	if err != nil {
		return false, err
	}
	doc["cleanupPeriodDays"] = raw
	return true, nil
}

// checkWritable fails fast with a clear, actionable error if path exists
// but isn't writable, instead of letting a later atomic-rename write
// silently succeed by replacing a read-only file (which a rename can do
// even when the file itself denies writes, as long as its directory is
// writable).
func checkWritable(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0) // #nosec G304 -- settingsPath is ~/.claude/settings.json, resolved from the user's own home directory, or a test-controlled path
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s is not writable, make it writable and try again: %w", path, err)
	}
	return f.Close()
}

// writeAtomic writes data to path via a temp file + rename in the same
// directory, so a reader never observes a partially written file.
func writeAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil { // #nosec G304,G306,G703 -- tmp is path (caller-controlled, our own computed settings.json path or a test path) with a fixed suffix appended, not externally tainted; perm matches settings.json's existing mode
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming %s to %s: %w", tmp, path, err)
	}
	return nil
}

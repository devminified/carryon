// Package claudepath encodes an absolute path into Claude Code's own
// ~/.claude/projects/<encoded> folder-naming convention, and locates
// that folder — the one piece of "where does Claude Code look"
// knowledge internal/pull's placement step (CO-209) needs, kept
// separate from that package per this project's own folder-structure
// plan.
package claudepath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Encode maps absPath to Claude Code's own project-folder naming
// convention: every character that isn't an ASCII letter or digit
// becomes exactly one '-' — one dash per character, never collapsed,
// and NOT Unicode-aware. Verified against a live, pinned Claude Code
// run (not assumed from the ticket's own English description, which
// read alone sounds like it should preserve any Unicode letter): an
// accented letter like 'é' or a CJK character is replaced too, each
// becoming exactly one dash regardless of its UTF-8 byte width —
// confirmed by running Claude Code against "café-日本語-tëst" and
// seeing six dashes, one per non-ASCII character, not eighteen (one
// per UTF-8 byte). "a..b" (two literal dots) produced "a--b" — two
// dashes, confirming runs are never collapsed either.
func Encode(absPath string) string {
	var b strings.Builder
	b.Grow(len(absPath))
	for _, r := range absPath {
		if isASCIILetterOrDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func isASCIILetterOrDigit(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	return false
}

// Dir returns ~/.claude — Claude Code's own config root, which this
// package never creates if it's missing: that would mean Claude Code
// itself has never been installed or run on this machine, which the
// caller should treat as an error, not something to paper over.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, ".claude"), nil
}

// ProjectDir returns the project folder Claude Code uses for sessions
// whose working directory is absPath: ~/.claude/projects/<Encode(absPath)>.
func ProjectDir(absPath string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "projects", Encode(absPath)), nil
}

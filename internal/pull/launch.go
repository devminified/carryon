// CO-210: the last step of the pull pipeline — show the developer what
// they're about to resume, warn if their local Claude Code is too old
// to safely understand the session they're pulling, then either launch
// Claude Code into the restored worktree or (with NoLaunch) hand back
// the exact command to run by hand.
package pull

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ErrClaudeNotFound is returned by Launch when the configured Claude
// Code binary isn't on PATH.
var ErrClaudeNotFound = errors.New("claude not found on PATH")

// HeaderInfo is what FormatHeader needs to render the one-line summary
// shown before launch, e.g.:
//
//	Ali · feature/payments · last activity 18:42 · 3 uncommitted files · Claude Code 2.1.263
type HeaderInfo struct {
	Owner             string
	Branch            string
	LastActivity      time.Time
	UncommittedFiles  int
	ClaudeCodeVersion string
}

// FormatHeader renders in as the pre-launch summary line. LastActivity
// is shown in the local time zone, HH:MM, since that's what a developer
// glancing at their own terminal expects, not UTC.
func FormatHeader(in HeaderInfo) string {
	word := "files"
	if in.UncommittedFiles == 1 {
		word = "file"
	}
	return fmt.Sprintf("%s · %s · last activity %s · %d uncommitted %s · Claude Code %s",
		in.Owner, in.Branch, in.LastActivity.Local().Format("15:04"), in.UncommittedFiles, word, in.ClaudeCodeVersion)
}

// Version is a parsed Claude Code version (major.minor.patch).
type Version struct {
	Major, Minor, Patch int
}

// ParseVersion parses a Claude Code version string ("2.1.263"). ok is
// false if s doesn't have exactly three non-negative integer components.
func ParseVersion(s string) (v Version, ok bool) {
	parts := strings.Split(strings.TrimSpace(s), ".")
	if len(parts) != 3 {
		return Version{}, false
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Version{}, false
		}
		nums[i] = n
	}
	return Version{Major: nums[0], Minor: nums[1], Patch: nums[2]}, true
}

func (v Version) less(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

// VersionRelation is CompareVersions' result.
type VersionRelation int

const (
	VersionEqual VersionRelation = iota
	VersionNewer
	VersionOlder
	VersionUnparseable
)

// CompareVersions reports how localVersion relates to sessionVersion —
// the version the session was originally recorded with. Either side
// failing to parse reports VersionUnparseable rather than guessing.
func CompareVersions(sessionVersion, localVersion string) VersionRelation {
	sv, ok1 := ParseVersion(sessionVersion)
	lv, ok2 := ParseVersion(localVersion)
	if !ok1 || !ok2 {
		return VersionUnparseable
	}
	switch {
	case sv == lv:
		return VersionEqual
	case sv.less(lv):
		return VersionNewer
	default:
		return VersionOlder
	}
}

// NeedsVersionWarning reports whether the puller's local Claude Code is
// old enough, relative to sessionVersion, that CO-210 calls for warning
// and asking before launch: older by a major or minor release. A
// patch-only difference (2.1.263 vs 2.1.260) never warns — only the
// feature-level mismatch a patch bump can't represent does. Either
// version failing to parse also warns: an unverifiable compatibility
// claim is never silently treated as fine.
func NeedsVersionWarning(sessionVersion, localVersion string) bool {
	sv, ok1 := ParseVersion(sessionVersion)
	lv, ok2 := ParseVersion(localVersion)
	if !ok1 || !ok2 {
		return true
	}
	if lv.Major != sv.Major {
		return lv.Major < sv.Major
	}
	return lv.Minor < sv.Minor
}

// LocalClaudeVersion runs `claude --version` and returns the version
// component of its output. Verified against a live, pinned Claude Code
// (2.1.263) run: the real output is "2.1.263 (Claude Code)" — the
// version is the first whitespace-delimited token, not the whole line.
func LocalClaudeVersion(ctx context.Context, claudeBin string) (string, error) {
	if claudeBin == "" {
		claudeBin = "claude"
	}
	out, err := exec.CommandContext(ctx, claudeBin, "--version").Output() // #nosec G204 -- claudeBin is operator-configured, not user/network input
	if err != nil {
		return "", fmt.Errorf("running %s --version: %w", claudeBin, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("running %s --version: empty output", claudeBin)
	}
	return fields[0], nil
}

// LaunchInput is what Launch needs to hand a restored, placed session
// (CO-205..CO-209's output) off to Claude Code itself.
type LaunchInput struct {
	// WorktreePath is the restored worktree (CO-206) to chdir into.
	WorktreePath string
	// SessionID is the new session ID (CO-208) to resume.
	SessionID string
	// NoLaunch, if true, prints the equivalent command to Stdout instead
	// of running it — CO-210's `--no-launch`.
	NoLaunch bool
	// ClaudeBin overrides the "claude" binary name — tests point this at
	// a fake script; production leaves it empty.
	ClaudeBin string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Launch resumes in.SessionID as a forked session inside in.WorktreePath,
// with the TTY passed through so Claude Code's own UI runs normally. If
// in.NoLaunch is set, it prints the command to in.Stdout and returns
// instead of running anything. Returns ErrClaudeNotFound — wrapped,
// check with errors.Is — if the binary isn't on PATH, naming how to fix
// it rather than surfacing a bare "executable file not found" error.
func Launch(ctx context.Context, in LaunchInput) error {
	bin := in.ClaudeBin
	if bin == "" {
		bin = "claude"
	}
	stdin, stdout, stderr := in.Stdin, in.Stdout, in.Stderr
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	args := []string{"--resume", in.SessionID, "--fork-session"}

	if in.NoLaunch {
		_, err := fmt.Fprintf(stdout, "cd %s && %s --resume %s --fork-session\n", shellQuote(in.WorktreePath), bin, shellQuote(in.SessionID))
		return err
	}

	path, err := exec.LookPath(bin)
	if err != nil {
		return fmt.Errorf("%w: install Claude Code and make sure %q is on your PATH", ErrClaudeNotFound, bin)
	}

	cmd := exec.CommandContext(ctx, path, args...) // #nosec G204 -- args are fixed flags plus our own generated session ID, not user/network input
	cmd.Dir = in.WorktreePath
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// shellQuote wraps s in single quotes for the command Launch prints
// under --no-launch, escaping any embedded single quote so the printed
// line is always safe to paste as-is — even a worktree path containing
// a space (an unusual but possible repo/branch name) still works.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

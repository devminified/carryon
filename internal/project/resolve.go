package project

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// ErrNoRepo is returned by ResolveRemote when cwd isn't inside a git
// repository.
var ErrNoRepo = errors.New("not inside a git repository")

// ErrNoRemote is returned by ResolveRemote when cwd is inside a git
// repository that has no remotes configured.
var ErrNoRemote = errors.New("git repository has no remotes")

// gitCommandTimeout bounds each git invocation. These are local,
// metadata-only commands (no network) and should return near-instantly;
// this is a safety net against an unexpected hang, not an expected cost.
const gitCommandTimeout = 5 * time.Second

// Toplevel returns the absolute path to the repository root containing
// cwd ("git rev-parse --show-toplevel") — so a nested subfolder resolves
// the same as the repo root. CO-201's snapshot capture (and anything
// else that needs to run further git commands reliably) uses this
// directly; ResolveRemote and RemoteName use it internally too.
//
// Returns ErrNoRepo if cwd isn't inside a git repository.
func Toplevel(ctx context.Context, cwd string) (string, error) {
	if cwd == "" {
		return "", ErrNoRepo
	}
	toplevel, err := gitOutput(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", ErrNoRepo
	}
	return toplevel, nil
}

// RemoteName returns the local name of the remote ResolveRemote would
// resolve for cwd's repository — "origin" if configured, else the first
// configured remote — matching ResolveRemote's own fallback exactly.
// CO-201's snapshot push needs this: it must push to the SAME remote
// ResolveRemote used to match the registered project, never a different
// one just because more than one happens to be configured.
//
// Returns ErrNoRepo / ErrNoRemote under the same conditions as
// ResolveRemote.
func RemoteName(ctx context.Context, cwd string) (string, error) {
	toplevel, err := Toplevel(ctx, cwd)
	if err != nil {
		return "", err
	}

	if _, err := gitOutput(ctx, toplevel, "remote", "get-url", "origin"); err == nil {
		return "origin", nil
	}

	names, err := gitOutput(ctx, toplevel, "remote")
	if err != nil || names == "" {
		return "", ErrNoRemote
	}
	first, _, _ := strings.Cut(names, "\n")
	return first, nil
}

// ResolveRemote finds the canonical git remote for the repository
// containing cwd (CO-108): resolves the repo's toplevel, picks the
// remote RemoteName would (origin, falling back to the first configured
// remote), and normalizes its URL with NormalizeRemote.
//
// Returns ErrNoRepo if cwd isn't inside a git repository, or ErrNoRemote
// if the repository has no remotes at all.
func ResolveRemote(ctx context.Context, cwd string) (string, error) {
	toplevel, err := Toplevel(ctx, cwd)
	if err != nil {
		return "", err
	}

	name, err := RemoteName(ctx, toplevel)
	if err != nil {
		return "", err
	}

	raw, err := gitOutput(ctx, toplevel, "remote", "get-url", name)
	if err != nil {
		return "", ErrNoRemote
	}

	return NormalizeRemote(raw)
}

// CurrentBranch returns the current branch name for the repository
// containing cwd (CO-111 — one of the agent-derived fields register-chunk
// stores on a session's first chunk, since the server has no way to exec
// git itself). Returns "HEAD" for a detached HEAD, matching `git branch
// --show-current`'s fallback behavior for `rev-parse --abbrev-ref HEAD`.
// Returns an error if cwd isn't inside a git repository.
func CurrentBranch(ctx context.Context, cwd string) (string, error) {
	return gitOutput(ctx, cwd, "rev-parse", "--abbrev-ref", "HEAD")
}

// HeadSHA returns the current commit SHA for the repository containing
// cwd (CO-111, same rationale as CurrentBranch). Returns an error if cwd
// isn't inside a git repository or has no commits yet.
func HeadSHA(ctx context.Context, cwd string) (string, error) {
	return gitOutput(ctx, cwd, "rev-parse", "HEAD")
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- args are fixed literals, dir is caller-controlled but only ever used as a working directory, not interpolated into the command
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

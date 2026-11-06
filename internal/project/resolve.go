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

// ResolveRemote finds the canonical git remote for the repository
// containing cwd (CO-108): it runs `git rev-parse --show-toplevel` (so a
// nested subfolder resolves the same as the repo root), reads
// remote.origin.url, falling back to the first configured remote if
// there's no "origin", and normalizes the result with NormalizeRemote.
//
// Returns ErrNoRepo if cwd isn't inside a git repository, or ErrNoRemote
// if the repository has no remotes at all.
func ResolveRemote(ctx context.Context, cwd string) (string, error) {
	if cwd == "" {
		return "", ErrNoRepo
	}

	toplevel, err := gitOutput(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", ErrNoRepo
	}

	raw, err := gitOutput(ctx, toplevel, "remote", "get-url", "origin")
	if err != nil {
		names, listErr := gitOutput(ctx, toplevel, "remote")
		if listErr != nil || names == "" {
			return "", ErrNoRemote
		}
		first, _, _ := strings.Cut(names, "\n")
		raw, err = gitOutput(ctx, toplevel, "remote", "get-url", first)
		if err != nil {
			return "", ErrNoRemote
		}
	}

	return NormalizeRemote(raw)
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

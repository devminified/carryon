package pull

// CO-206: find a local clone of the project being pulled, or offer to
// clone one. Checked in order: the caller's current working directory,
// then the project's remembered path in config.Repos, then (if
// neither exists) a confirm callback decides whether to clone into
// ~/carryon/repos/<name>.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/devminified/carryon/internal/project"
)

// cloneTimeout bounds the one real network call this file makes.
const cloneTimeout = 5 * time.Minute

// ErrCloneDeclined is returned by LocateClone when no local clone was
// found and confirm returned false.
var ErrCloneDeclined = errors.New("no local clone found and cloning was declined")

// ConfirmFunc decides whether to proceed cloning when no local clone
// was found — the actual interactive prompt (or --yes bypassing it) is
// a cmd/carryon concern (CO-211); this package only needs a yes/no
// answer; passing a func that always returns true is a --yes-mode.
type ConfirmFunc func() bool

// LocateInput is what LocateClone needs to find or prepare a local
// clone. Cwd and Repos come from the caller so this stays testable
// without touching the real filesystem's home directory or the
// process's actual working directory.
type LocateInput struct {
	Cwd string
	// ProjectName names the clone directory when one must be created
	// (~/carryon/repos/<ProjectName>).
	ProjectName string
	// CanonicalRemote is project.NormalizeRemote's form of the
	// project's git remote — the key LocateClone looks up (and the
	// caller should save the result under) in Repos.
	CanonicalRemote string
	// GitRemoteURL is the real remote URL to clone from — CanonicalRemote
	// has already dropped the scheme/credentials/.git suffix a real
	// `git clone` needs.
	GitRemoteURL string
	// Repos is the caller's config.Config.Repos — read-only here; the
	// caller persists any update (a fresh clone's path) back to
	// ~/.carryon/config.toml itself, since this package has no opinion
	// on config file I/O.
	Repos map[string]string
}

// LocateResult is what LocateClone found or created.
type LocateResult struct {
	Path   string
	Cloned bool
}

// LocateClone finds a local clone of in.CanonicalRemote, or clones one
// if confirm allows it.
func LocateClone(ctx context.Context, in LocateInput, confirm ConfirmFunc) (*LocateResult, error) {
	if in.Cwd != "" {
		if remote, err := project.ResolveRemote(ctx, in.Cwd); err == nil && remote == in.CanonicalRemote {
			if toplevel, err := project.Toplevel(ctx, in.Cwd); err == nil {
				return &LocateResult{Path: toplevel}, nil
			}
		}
	}

	if p, ok := in.Repos[in.CanonicalRemote]; ok && p != "" {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			if remote, err := project.ResolveRemote(ctx, p); err == nil && remote == in.CanonicalRemote {
				return &LocateResult{Path: p}, nil
			}
		}
	}

	if confirm == nil || !confirm() {
		return nil, ErrCloneDeclined
	}

	dest, err := defaultCloneDir(in.ProjectName)
	if err != nil {
		return nil, err
	}
	if err := cloneInto(ctx, in.GitRemoteURL, dest); err != nil {
		return nil, err
	}
	return &LocateResult{Path: dest, Cloned: true}, nil
}

// defaultCloneDir is CO-206's "~/carryon/repos/<name>" — deliberately
// not under ~/.carryon (internal/config.Dir()): that directory is
// Carryon's own private state, while a project clone is a real,
// developer-visible checkout meant to be browsed, opened in an editor,
// etc.
func defaultCloneDir(projectName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, "carryon", "repos", projectName), nil
}

// defaultWorkDir is CO-206's "~/carryon/work/<project>-<sid8>" parent —
// where every restored worktree lives, so CleanWorktrees knows exactly
// what's safe to remove.
func defaultWorkDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, "carryon", "work"), nil
}

func cloneInto(ctx context.Context, remoteURL, destDir string) error {
	if err := os.MkdirAll(filepath.Dir(destDir), 0o755); err != nil { // #nosec G301 -- a developer-visible checkout dir, not sensitive state
		return fmt.Errorf("creating %s: %w", filepath.Dir(destDir), err)
	}

	cctx, cancel := context.WithTimeout(ctx, cloneTimeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "git", "clone", "--", remoteURL, destDir) // #nosec G204 -- remoteURL/destDir are this project's own registered remote and a path we built
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cloning %s into %s: %w: %s", remoteURL, destDir, err, string(out))
	}
	return nil
}

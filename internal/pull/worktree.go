package pull

// CO-206: given a manifest's snapshot (a hidden ref, a bundle fallback,
// or neither), fetch it into the project's local clone and check it out
// in a dedicated worktree — never touching the puller's own checkout,
// branch, index, or working-tree files, the same non-destructive
// guarantee CO-201's push side gives the pusher.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/project"
	"github.com/devminified/carryon/internal/snapshot"
)

// localGitTimeout/fetchTimeout mirror internal/snapshot's own split
// between fast local metadata operations and the one real network call.
const (
	localGitTimeout = 10 * time.Second
	fetchTimeout    = 60 * time.Second
)

// shortIDWidth matches CO-204's "short IDs (the first 8 characters)"
// convention — the worktree directory name uses it too.
const shortIDWidth = 8

// RestoreInput is what RestoreWorktree needs: everything get-manifest
// (CO-203) returns about the session, used as-is rather than re-derived.
type RestoreInput struct {
	SessionID   string
	ProjectName string
	HeadSHA     string
	// Snapshot is nil when the session has no code snapshot yet.
	Snapshot *api.ManifestSnapshot
}

// RestoreResult is what RestoreWorktree produced.
type RestoreResult struct {
	Path string
	// Reused is true if an existing worktree at the right commit was
	// found and left alone — CO-206's "pulling the same session twice
	// reuses the worktree."
	Reused bool
	// Warning is non-empty when in.Snapshot was nil: head_sha was
	// checked out instead, and whatever was uncommitted in the
	// original session isn't included.
	Warning string
}

// NoSnapshotWarning is RestoreResult.Warning's exact text when a
// session has no code snapshot — exported so a caller (cmd/carryon,
// CO-211) can match on it rather than parsing prose.
const NoSnapshotWarning = "no code snapshot was captured for this session — checked out head_sha instead; any uncommitted work from the original session is not included"

// RestoreWorktree finds or updates a dedicated worktree at
// ~/carryon/work/<ProjectName>-<sid8> under cloneDir (a path
// LocateClone already resolved) checked out at the session's latest
// snapshot, or its head_sha if it has none.
func RestoreWorktree(ctx context.Context, client *api.Client, httpClient *http.Client, cloneDir string, in RestoreInput) (*RestoreResult, error) {
	remoteName, err := project.RemoteName(ctx, cloneDir)
	if err != nil {
		return nil, fmt.Errorf("resolving remote: %w", err)
	}

	commit, warning, err := resolveSnapshotCommit(ctx, client, httpClient, cloneDir, remoteName, in)
	if err != nil {
		return nil, err
	}

	workDir, err := defaultWorkDir()
	if err != nil {
		return nil, err
	}
	worktreePath := filepath.Join(workDir, in.ProjectName+"-"+shortID(in.SessionID))

	reused, err := ensureWorktree(ctx, cloneDir, worktreePath, commit)
	if err != nil {
		return nil, err
	}

	return &RestoreResult{Path: worktreePath, Reused: reused, Warning: warning}, nil
}

// resolveSnapshotCommit fetches whatever the manifest's snapshot points
// to (a hidden ref, or a bundle fallback) into cloneDir and returns the
// commit SHA it resolved to. With no snapshot at all, it ensures
// in.HeadSHA itself is fetchable and returns that instead, plus
// NoSnapshotWarning.
func resolveSnapshotCommit(ctx context.Context, client *api.Client, httpClient *http.Client, cloneDir, remoteName string, in RestoreInput) (commit, warning string, err error) {
	switch {
	case in.Snapshot != nil && in.Snapshot.WipRef != "":
		if _, err := runGit(ctx, fetchTimeout, cloneDir, "fetch", remoteName, in.Snapshot.WipRef); err != nil {
			return "", "", fmt.Errorf("fetching snapshot ref %s: %w", in.Snapshot.WipRef, err)
		}
		sha, err := runGit(ctx, localGitTimeout, cloneDir, "rev-parse", "FETCH_HEAD")
		if err != nil {
			return "", "", fmt.Errorf("resolving fetched ref: %w", err)
		}
		return sha, "", nil

	case in.Snapshot != nil && in.Snapshot.ObjectKey != "":
		bundlePath, cleanup, err := downloadBundle(ctx, httpClient, client, *in.Snapshot)
		if err != nil {
			return "", "", fmt.Errorf("downloading snapshot bundle: %w", err)
		}
		defer cleanup()

		ref := snapshot.BundleTmpRef(in.SessionID)
		if _, err := runGit(ctx, localGitTimeout, cloneDir, "fetch", bundlePath, ref); err != nil {
			return "", "", fmt.Errorf("fetching snapshot bundle: %w", err)
		}
		sha, err := runGit(ctx, localGitTimeout, cloneDir, "rev-parse", "FETCH_HEAD")
		if err != nil {
			return "", "", fmt.Errorf("resolving fetched bundle ref: %w", err)
		}
		return sha, "", nil

	default:
		if in.HeadSHA == "" {
			return "", "", fmt.Errorf("session has no snapshot and no head_sha to fall back to")
		}
		// Best-effort: head_sha is usually already reachable (the
		// developer's own branch was pushed), but fetch first in case
		// the local clone just hasn't seen it yet. A failure here isn't
		// fatal on its own — the rev-parse check right after is what
		// actually decides whether we can proceed.
		_, _ = runGit(ctx, fetchTimeout, cloneDir, "fetch", remoteName)
		if _, err := runGit(ctx, localGitTimeout, cloneDir, "rev-parse", "--verify", in.HeadSHA+"^{commit}"); err != nil {
			return "", "", fmt.Errorf("head_sha %s isn't reachable from %s (it may never have been pushed): %w", in.HeadSHA, remoteName, err)
		}
		return in.HeadSHA, NoSnapshotWarning, nil
	}
}

// downloadBundle saves snap's bundle object to a temp file, returning
// its path and a cleanup func. Mirrors internal/pull.downloadOneChunk's
// use of Client.StorageDownloadURL — the manifest only ever hands back
// a bare token, never the Edge Function's own signed URL (see
// get-manifest/index.ts's signDownloadToken doc comment).
func downloadBundle(ctx context.Context, httpClient *http.Client, client *api.Client, snap api.ManifestSnapshot) (path string, cleanup func(), err error) {
	url := client.StorageDownloadURL(snap.Bucket, snap.ObjectKey, snap.DownloadToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", nil, fmt.Errorf("building request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("downloading: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	f, err := os.CreateTemp("", "carryon-pull-bundle-")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = os.Remove(f.Name())
		return "", nil, fmt.Errorf("writing bundle: %w", err)
	}
	return f.Name(), func() { _ = os.Remove(f.Name()) }, nil
}

// ensureWorktree makes sure worktreePath, registered against cloneDir,
// is checked out at commit — creating it if it doesn't exist yet,
// moving it in place with a plain checkout if it's already a Carryon
// worktree at a different commit, or leaving it alone (reused = true)
// if it's already exactly where it needs to be. It refuses to touch a
// path that exists but isn't a worktree of cloneDir at all, rather than
// risk clobbering something unrelated.
func ensureWorktree(ctx context.Context, cloneDir, worktreePath, commit string) (reused bool, err error) {
	info, statErr := os.Stat(worktreePath)
	if statErr != nil {
		if !os.IsNotExist(statErr) {
			return false, fmt.Errorf("checking %s: %w", worktreePath, statErr)
		}
		if _, err := runGit(ctx, localGitTimeout, cloneDir, "worktree", "add", "--detach", worktreePath, commit); err != nil {
			return false, fmt.Errorf("adding worktree: %w", err)
		}
		return false, nil
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%s exists and isn't a directory", worktreePath)
	}

	registered, err := isRegisteredWorktree(ctx, cloneDir, worktreePath)
	if err != nil {
		return false, err
	}
	if !registered {
		return false, fmt.Errorf("%s already exists and isn't a Carryon worktree of %s — refusing to touch it", worktreePath, cloneDir)
	}

	head, err := runGit(ctx, localGitTimeout, worktreePath, "rev-parse", "HEAD")
	if err != nil {
		return false, fmt.Errorf("checking existing worktree's HEAD: %w", err)
	}
	if head == commit {
		return true, nil
	}

	if _, err := runGit(ctx, localGitTimeout, worktreePath, "checkout", "--detach", commit); err != nil {
		return false, fmt.Errorf("moving existing worktree to %s: %w", commit, err)
	}
	return false, nil
}

// normalizeWorktreePath resolves symlinks (best-effort) and converts to
// the OS's native separator before comparing or taking a relative path
// — `git worktree list --porcelain` always reports forward slashes,
// even on Windows, which never string-compares or filepath.Rel's
// correctly against a path Go's filepath.Join built with native
// backslashes (found via a real Windows CI run: every worktree lookup
// silently failed to match, even though the same code worked on macOS
// and Linux).
func normalizeWorktreePath(p string) string {
	p = filepath.FromSlash(p)
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return p
}

// isRegisteredWorktree reports whether path is one of cloneDir's
// registered worktrees (git worktree list --porcelain's "worktree "
// lines, one absolute path per entry).
func isRegisteredWorktree(ctx context.Context, cloneDir, path string) (bool, error) {
	out, err := runGit(ctx, localGitTimeout, cloneDir, "worktree", "list", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("listing worktrees: %w", err)
	}
	want := normalizeWorktreePath(path)
	for _, line := range strings.Split(out, "\n") {
		p, ok := strings.CutPrefix(line, "worktree ")
		if !ok {
			continue
		}
		if normalizeWorktreePath(p) == want {
			return true, nil
		}
	}
	return false, nil
}

// CleanWorktrees removes every worktree of cloneDir living under
// ~/carryon/work (CO-206: "carryon pull --clean removes old Carryon
// worktrees"), returning the paths it removed. Never touches cloneDir
// itself or a worktree registered somewhere else entirely.
func CleanWorktrees(ctx context.Context, cloneDir string) ([]string, error) {
	workDir, err := defaultWorkDir()
	if err != nil {
		return nil, err
	}
	workDirNorm := normalizeWorktreePath(workDir)

	out, err := runGit(ctx, localGitTimeout, cloneDir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("listing worktrees: %w", err)
	}

	var removed []string
	for _, line := range strings.Split(out, "\n") {
		p, ok := strings.CutPrefix(line, "worktree ")
		if !ok {
			continue
		}
		rel, err := filepath.Rel(workDirNorm, normalizeWorktreePath(p))
		if err != nil || strings.HasPrefix(rel, "..") {
			continue // not under ~/carryon/work — not ours to remove
		}
		if _, err := runGit(ctx, localGitTimeout, cloneDir, "worktree", "remove", "--force", p); err != nil {
			return removed, fmt.Errorf("removing worktree %s: %w", p, err)
		}
		removed = append(removed, p)
	}
	return removed, nil
}

// shortID matches CO-204's "short IDs (the first 8 characters)."
func shortID(id string) string {
	if len(id) <= shortIDWidth {
		return id
	}
	return id[:shortIDWidth]
}

func runGit(ctx context.Context, timeout time.Duration, dir string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "git", args...) // #nosec G204 -- args are fixed literals/caller-controlled SHAs and paths, never shell-interpolated
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

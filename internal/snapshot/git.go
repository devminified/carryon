package snapshot

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// localGitTimeout bounds every git invocation that never leaves the
// laptop (read-tree, add, write-tree, commit-tree, update-index,
// rev-parse) — these are local, metadata-only operations and should
// return near-instantly, matching internal/project's own
// gitCommandTimeout.
const localGitTimeout = 10 * time.Second

// pushTimeout bounds the one git invocation that's a real network call.
const pushTimeout = 60 * time.Second

// newTempIndex returns a path to use as GIT_INDEX_FILE and a cleanup
// func — CO-201's "never touches the developer's index, stash, branch
// or index.lock": every index-touching git command below runs with
// GIT_INDEX_FILE pointed here instead of the repo's real .git/index, so
// none of them can ever create a real index.lock or disturb what's
// actually staged. The path itself doesn't need to exist yet — git
// creates it fresh on the first read-tree/add.
func newTempIndex() (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "carryon-snapshot-index-")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp index dir: %w", err)
	}
	return filepath.Join(dir, "index"), func() { _ = os.RemoveAll(dir) }, nil
}

// readTreeHead loads HEAD's tree into indexPath. A repo with no commits
// yet has no HEAD to read — that's not an error here, it just means the
// temp index starts empty (git add -A below stages everything as new).
func readTreeHead(ctx context.Context, repoDir, indexPath string) error {
	_, err := runGit(ctx, localGitTimeout, repoDir, indexEnv(indexPath), "read-tree", "HEAD")
	if err != nil && !strings.Contains(err.Error(), "Not a valid object name HEAD") && !strings.Contains(err.Error(), "ambiguous argument 'HEAD'") {
		return err
	}
	return nil
}

// addAll stages every change in the working tree into indexPath,
// respecting .gitignore (git add -A's default behavior — no -f).
func addAll(ctx context.Context, repoDir, indexPath string) error {
	_, err := runGit(ctx, localGitTimeout, repoDir, indexEnv(indexPath), "add", "-A")
	return err
}

// untrackedFiles lists every untracked, non-ignored file — exactly
// what `git add -A` just staged as new, which is also exactly the set
// CO-201's 50MB-exclusion rule applies to (a *tracked* file growing past
// 50MB is never excluded, only brand-new untracked ones).
func untrackedFiles(ctx context.Context, repoDir string) ([]string, error) {
	out, err := runGit(ctx, localGitTimeout, repoDir, nil, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// forceRemoveFromIndex drops path from indexPath without touching the
// working tree file itself — used to exclude a too-large untracked file
// that addAll just staged.
func forceRemoveFromIndex(ctx context.Context, repoDir, indexPath, path string) error {
	_, err := runGit(ctx, localGitTimeout, repoDir, indexEnv(indexPath), "update-index", "--force-remove", "--", path)
	return err
}

// writeTree writes indexPath's current contents as a tree object and
// returns its SHA.
func writeTree(ctx context.Context, repoDir, indexPath string) (string, error) {
	return runGit(ctx, localGitTimeout, repoDir, indexEnv(indexPath), "write-tree")
}

// commitTree creates a commit object for treeSHA with the given parent
// (empty parentSHA means no parent — a repo's first-ever commit) and
// message, returning the new commit's SHA. No index involved at all, so
// no GIT_INDEX_FILE needed here.
func commitTree(ctx context.Context, repoDir, treeSHA, parentSHA, message string) (string, error) {
	args := []string{"commit-tree", treeSHA}
	if parentSHA != "" {
		args = append(args, "-p", parentSHA)
	}
	args = append(args, "-m", message)
	return runGit(ctx, localGitTimeout, repoDir, nil, args...)
}

// pushSnapshot pushes commitSHA to refName on remoteName, force (CO-201:
// the ref is Carryon's own hidden pointer, always meant to move to
// wherever the latest snapshot commit is — never a ref a human is
// expected to fast-forward manually). This is the one network call in
// the whole package.
func pushSnapshot(ctx context.Context, repoDir, remoteName, commitSHA, refName string) error {
	_, err := runGit(ctx, pushTimeout, repoDir, nil, "push", remoteName, commitSHA+":"+refName, "--force")
	return err
}

// BundleTmpRef is a deterministic, Carryon-owned ref name used only for
// the few seconds it takes to create a fallback bundle — `git bundle
// create` refuses a bare commit SHA with "Refusing to create empty
// bundle" (verified against a live repo, not assumed from docs): it
// needs an actual ref it can name in the bundle's header. Using
// sessionID keeps it predictable without colliding with any real
// branch, and it's deleted immediately after (see createBundle).
//
// Exported because the puller (internal/pull, CO-206) needs the exact
// same ref name to fetch a bundle fallback back out again — it's a
// shared contract between the push side and the pull side, not an
// implementation detail of this package alone.
func BundleTmpRef(sessionID string) string {
	return "refs/carryon-tmp/" + sessionID
}

// createBundle writes a git bundle containing only the objects
// reachable from commitSHA that aren't already reachable from any of
// remoteName's remote-tracking refs (CO-202: "bundle size stays
// proportional to the changed objects, not the whole repo") to a fresh
// temp file, returning its path and a cleanup func. The temp ref it
// uses internally is always deleted before this returns, successful or
// not — it never lingers as a side effect a `git branch`/`git log`
// would show the developer.
func createBundle(ctx context.Context, repoDir, commitSHA, remoteName, sessionID string) (path string, cleanup func(), err error) {
	ref := BundleTmpRef(sessionID)
	if _, err := runGit(ctx, localGitTimeout, repoDir, nil, "update-ref", ref, commitSHA); err != nil {
		return "", nil, fmt.Errorf("creating temp ref: %w", err)
	}
	defer func() {
		_, _ = runGit(ctx, localGitTimeout, repoDir, nil, "update-ref", "-d", ref)
	}()

	f, err := os.CreateTemp("", "carryon-snapshot-bundle-")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp bundle file: %w", err)
	}
	bundlePath := f.Name()
	_ = f.Close()
	if err := os.Remove(bundlePath); err != nil { // git bundle create wants to create the file itself
		return "", nil, fmt.Errorf("removing placeholder bundle file: %w", err)
	}

	if _, err := runGit(ctx, localGitTimeout, repoDir, nil, "bundle", "create", bundlePath, ref, "--not", "--remotes="+remoteName); err != nil {
		return "", nil, fmt.Errorf("creating bundle: %w", err)
	}

	return bundlePath, func() { _ = os.Remove(bundlePath) }, nil
}

func indexEnv(indexPath string) []string {
	return []string{"GIT_INDEX_FILE=" + indexPath}
}

func runGit(ctx context.Context, timeout time.Duration, dir string, extraEnv []string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "git", args...) // #nosec G204 -- args are fixed literals/caller-controlled SHAs and paths, never shell-interpolated
	cmd.Dir = dir
	if extraEnv != nil {
		cmd.Env = append(os.Environ(), extraEnv...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

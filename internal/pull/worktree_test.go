package pull

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/snapshot"
)

// pushHiddenRef pushes a new commit (built on top of whatever's
// currently checked out in cloneDir) to refName on the bare remote,
// simulating what CO-201's MaybeSnapshot does for real, and returns
// the new commit's SHA.
func pushHiddenRef(t *testing.T, cloneDir, refName, fileName, content string) string {
	t.Helper()
	sha := commitFile(t, cloneDir, fileName, content)
	runGitCmd(t, cloneDir, "push", "--quiet", "origin", "HEAD:"+refName, "--force")
	return sha
}

func TestRestoreWorktreeViaRefCreatesNewWorktree(t *testing.T) {
	home := withFakeHome(t)
	_, cloneDir := newBareRemote(t)
	snapSHA := pushHiddenRef(t, cloneDir, "refs/carryon/ali/sess1", "wip.txt", "work in progress\n")

	result, err := RestoreWorktree(context.Background(), nil, nil, cloneDir, RestoreInput{
		SessionID:   "sess1",
		ProjectName: "acme-api",
		Snapshot:    &api.ManifestSnapshot{WipRef: "refs/carryon/ali/sess1"},
	})
	if err != nil {
		t.Fatalf("RestoreWorktree() error = %v", err)
	}
	if result.Reused {
		t.Error("Reused = true, want false (first restore)")
	}
	if result.Warning != "" {
		t.Errorf("Warning = %q, want empty (a real snapshot was used)", result.Warning)
	}
	wantPath := filepath.Join(home, "carryon", "work", "acme-api-sess1")
	if result.Path != wantPath {
		t.Errorf("Path = %q, want %q", result.Path, wantPath)
	}

	if _, err := os.Stat(filepath.Join(result.Path, "wip.txt")); err != nil {
		t.Errorf("worktree missing wip.txt: %v", err)
	}
	head := runGitCmd(t, result.Path, "rev-parse", "HEAD")
	if trimmed := head[:len(head)-1]; trimmed != snapSHA {
		t.Errorf("worktree HEAD = %q, want %q", trimmed, snapSHA)
	}
}

func TestRestoreWorktreeDoesNotTouchTheMainClone(t *testing.T) {
	withFakeHome(t)
	_, cloneDir := newBareRemote(t)
	beforeBranch := runGitCmd(t, cloneDir, "branch", "--show-current")

	pushHiddenRef(t, cloneDir, "refs/carryon/ali/sess1", "wip.txt", "work in progress\n")
	// pushHiddenRef's commitFile already advanced cloneDir's own HEAD
	// (it commits for real, same as a developer would) -- what matters
	// is that RestoreWorktree itself, called next, doesn't move it any
	// further or touch its branch/status.
	beforeHead := runGitCmd(t, cloneDir, "rev-parse", "HEAD")

	if _, err := RestoreWorktree(context.Background(), nil, nil, cloneDir, RestoreInput{
		SessionID:   "sess1",
		ProjectName: "acme-api",
		Snapshot:    &api.ManifestSnapshot{WipRef: "refs/carryon/ali/sess1"},
	}); err != nil {
		t.Fatalf("RestoreWorktree() error = %v", err)
	}

	afterHead := runGitCmd(t, cloneDir, "rev-parse", "HEAD")
	afterBranch := runGitCmd(t, cloneDir, "branch", "--show-current")
	afterStatus := runGitCmd(t, cloneDir, "status", "--porcelain")
	if afterHead != beforeHead {
		t.Errorf("main clone HEAD changed: before %q, after %q", beforeHead, afterHead)
	}
	if afterBranch != beforeBranch {
		t.Errorf("main clone branch changed: before %q, after %q", beforeBranch, afterBranch)
	}
	if afterStatus != "" {
		t.Errorf("main clone has uncommitted changes after restore: %q", afterStatus)
	}
}

func TestRestoreWorktreeReusesWhenAlreadyAtTheSameCommit(t *testing.T) {
	withFakeHome(t)
	_, cloneDir := newBareRemote(t)
	pushHiddenRef(t, cloneDir, "refs/carryon/ali/sess1", "wip.txt", "work in progress\n")

	in := RestoreInput{
		SessionID:   "sess1",
		ProjectName: "acme-api",
		Snapshot:    &api.ManifestSnapshot{WipRef: "refs/carryon/ali/sess1"},
	}
	first, err := RestoreWorktree(context.Background(), nil, nil, cloneDir, in)
	if err != nil {
		t.Fatalf("RestoreWorktree() #1 error = %v", err)
	}
	second, err := RestoreWorktree(context.Background(), nil, nil, cloneDir, in)
	if err != nil {
		t.Fatalf("RestoreWorktree() #2 error = %v", err)
	}
	if !second.Reused {
		t.Error("Reused = false on the second identical restore, want true")
	}
	if second.Path != first.Path {
		t.Errorf("Path changed between calls: %q -> %q", first.Path, second.Path)
	}
}

func TestRestoreWorktreeMovesToANewCommitWhenTheRefAdvanced(t *testing.T) {
	withFakeHome(t)
	_, cloneDir := newBareRemote(t)
	pushHiddenRef(t, cloneDir, "refs/carryon/ali/sess1", "wip.txt", "v1\n")

	in := RestoreInput{
		SessionID:   "sess1",
		ProjectName: "acme-api",
		Snapshot:    &api.ManifestSnapshot{WipRef: "refs/carryon/ali/sess1"},
	}
	first, err := RestoreWorktree(context.Background(), nil, nil, cloneDir, in)
	if err != nil {
		t.Fatalf("RestoreWorktree() #1 error = %v", err)
	}

	// The developer kept working: a newer snapshot landed on the same ref.
	newSHA := pushHiddenRef(t, cloneDir, "refs/carryon/ali/sess1", "wip2.txt", "v2\n")
	second, err := RestoreWorktree(context.Background(), nil, nil, cloneDir, in)
	if err != nil {
		t.Fatalf("RestoreWorktree() #2 error = %v", err)
	}
	if second.Reused {
		t.Error("Reused = true, want false (the ref moved to a new commit)")
	}
	if second.Path != first.Path {
		t.Errorf("Path changed between calls: %q -> %q", first.Path, second.Path)
	}
	head := runGitCmd(t, second.Path, "rev-parse", "HEAD")
	if trimmed := head[:len(head)-1]; trimmed != newSHA {
		t.Errorf("worktree HEAD = %q, want the new commit %q", trimmed, newSHA)
	}
	if _, err := os.Stat(filepath.Join(second.Path, "wip2.txt")); err != nil {
		t.Errorf("worktree missing wip2.txt after moving to the new commit: %v", err)
	}
}

func TestRestoreWorktreeWithNoSnapshotChecksOutHeadSHAAndWarns(t *testing.T) {
	withFakeHome(t)
	_, cloneDir := newBareRemote(t)
	headSHA := runGitCmd(t, cloneDir, "rev-parse", "HEAD")
	headSHA = headSHA[:len(headSHA)-1]

	result, err := RestoreWorktree(context.Background(), nil, nil, cloneDir, RestoreInput{
		SessionID:   "sess1",
		ProjectName: "acme-api",
		HeadSHA:     headSHA,
		Snapshot:    nil,
	})
	if err != nil {
		t.Fatalf("RestoreWorktree() error = %v", err)
	}
	if result.Warning != NoSnapshotWarning {
		t.Errorf("Warning = %q, want NoSnapshotWarning", result.Warning)
	}
	head := runGitCmd(t, result.Path, "rev-parse", "HEAD")
	if trimmed := head[:len(head)-1]; trimmed != headSHA {
		t.Errorf("worktree HEAD = %q, want head_sha %q", trimmed, headSHA)
	}
}

func TestRestoreWorktreeWithUnreachableHeadSHAFails(t *testing.T) {
	withFakeHome(t)
	_, cloneDir := newBareRemote(t)

	_, err := RestoreWorktree(context.Background(), nil, nil, cloneDir, RestoreInput{
		SessionID:   "sess1",
		ProjectName: "acme-api",
		HeadSHA:     "0000000000000000000000000000000000dead", // never pushed, never existed
		Snapshot:    nil,
	})
	if err == nil {
		t.Fatal("RestoreWorktree() error = nil, want an error for an unreachable head_sha")
	}
}

func TestRestoreWorktreeRefusesToClobberAnUnrelatedDirectory(t *testing.T) {
	home := withFakeHome(t)
	_, cloneDir := newBareRemote(t)
	pushHiddenRef(t, cloneDir, "refs/carryon/ali/sess1", "wip.txt", "work in progress\n")

	// Something else already occupies the exact path RestoreWorktree
	// would use -- not a Carryon worktree at all.
	collidingPath := filepath.Join(home, "carryon", "work", "acme-api-sess1")
	if err := os.MkdirAll(collidingPath, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(collidingPath, "unrelated.txt"), []byte("not ours\n"), 0o600); err != nil {
		t.Fatalf("writing unrelated file: %v", err)
	}

	_, err := RestoreWorktree(context.Background(), nil, nil, cloneDir, RestoreInput{
		SessionID:   "sess1",
		ProjectName: "acme-api",
		Snapshot:    &api.ManifestSnapshot{WipRef: "refs/carryon/ali/sess1"},
	})
	if err == nil {
		t.Fatal("RestoreWorktree() error = nil, want an error rather than clobbering an unrelated directory")
	}
	if _, err := os.Stat(filepath.Join(collidingPath, "unrelated.txt")); err != nil {
		t.Errorf("unrelated.txt was removed/touched: %v", err)
	}
}

func TestRestoreWorktreeViaBundleFallback(t *testing.T) {
	withFakeHome(t)
	_, cloneDir := newBareRemote(t)

	// Build a real bundle the way CO-202's fallback does: a commit on a
	// temp ref named by snapshot.BundleTmpRef, bundled with --not
	// --remotes=origin, then the temp ref deleted.
	sessionID := "sess1"
	commitSHA := commitFile(t, cloneDir, "wip.txt", "from a bundle\n")
	ref := snapshot.BundleTmpRef(sessionID)
	runGitCmd(t, cloneDir, "update-ref", ref, commitSHA)
	bundlePath := filepath.Join(t.TempDir(), "snapshot.bundle")
	runGitCmd(t, cloneDir, "bundle", "create", bundlePath, ref, "--not", "--remotes=origin")
	runGitCmd(t, cloneDir, "update-ref", "-d", ref)
	// Undo the local commit on the main clone's own branch, so the
	// bundle is genuinely the only place this commit exists -- the
	// same situation a real puller is in.
	runGitCmd(t, cloneDir, "reset", "--hard", "HEAD~1")

	bundleBytes, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatalf("reading bundle: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bundleBytes)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL, "anon-key")
	result, err := RestoreWorktree(context.Background(), client, srv.Client(), cloneDir, RestoreInput{
		SessionID:   sessionID,
		ProjectName: "acme-api",
		Snapshot: &api.ManifestSnapshot{
			ObjectKey:     "doesnt-matter-the-fake-server-ignores-it",
			DownloadToken: "doesnt-matter",
		},
	})
	if err != nil {
		t.Fatalf("RestoreWorktree() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(result.Path, "wip.txt")); err != nil {
		t.Errorf("worktree missing wip.txt restored from the bundle: %v", err)
	}
	head := runGitCmd(t, result.Path, "rev-parse", "HEAD")
	if trimmed := head[:len(head)-1]; trimmed != commitSHA {
		t.Errorf("worktree HEAD = %q, want the bundled commit %q", trimmed, commitSHA)
	}
}

func TestCleanWorktreesRemovesOnlyOnesUnderCarryonWork(t *testing.T) {
	home := withFakeHome(t)
	_, cloneDir := newBareRemote(t)
	pushHiddenRef(t, cloneDir, "refs/carryon/ali/sess1", "wip.txt", "work in progress\n")

	result, err := RestoreWorktree(context.Background(), nil, nil, cloneDir, RestoreInput{
		SessionID:   "sess1",
		ProjectName: "acme-api",
		Snapshot:    &api.ManifestSnapshot{WipRef: "refs/carryon/ali/sess1"},
	})
	if err != nil {
		t.Fatalf("RestoreWorktree() error = %v", err)
	}
	ourWorktree := result.Path
	// ourWorktree is <home>/carryon/work/acme-api-sess1 -- three levels
	// of Dir() back to <home>.
	if dir := filepath.Dir(filepath.Dir(filepath.Dir(ourWorktree))); dir != home {
		t.Fatalf("sanity check failed: ourWorktree = %q isn't under %q", ourWorktree, home)
	}
	// Captured before CleanWorktrees removes the directory —
	// EvalSymlinks needs the path to still exist to resolve it.
	// CleanWorktrees reports git's own canonicalized path (e.g.
	// /private/var/... on macOS), which may differ textually from the
	// unresolved form we built ourWorktree from, so compare resolved.
	wantReal, err := filepath.EvalSymlinks(ourWorktree)
	if err != nil {
		t.Fatalf("EvalSymlinks(ourWorktree): %v", err)
	}

	// A second, unrelated worktree registered against the same clone,
	// but living somewhere else entirely -- must survive the cleanup.
	elsewhereWorktree := filepath.Join(t.TempDir(), "not-ours")
	runGitCmd(t, cloneDir, "worktree", "add", "--detach", elsewhereWorktree, "HEAD")

	removed, err := CleanWorktrees(context.Background(), cloneDir)
	if err != nil {
		t.Fatalf("CleanWorktrees() error = %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("removed = %v, want exactly 1 entry", removed)
	}
	// removed[0] is already git's own canonicalized form (confirmed:
	// `worktree list` reports it resolved) — don't re-resolve it here,
	// the directory is already gone by this point and EvalSymlinks on
	// a path that no longer exists fails silently.
	if removed[0] != wantReal {
		t.Errorf("removed[0] = %q, want %q", removed[0], wantReal)
	}
	if _, err := os.Stat(ourWorktree); !os.IsNotExist(err) {
		t.Errorf("our worktree still exists after CleanWorktrees: stat err = %v", err)
	}
	if _, err := os.Stat(elsewhereWorktree); err != nil {
		t.Errorf("the unrelated worktree was removed too: %v", err)
	}
}

func TestCleanWorktreesWithNoneToRemove(t *testing.T) {
	withFakeHome(t)
	_, cloneDir := newBareRemote(t)

	removed, err := CleanWorktrees(context.Background(), cloneDir)
	if err != nil {
		t.Fatalf("CleanWorktrees() error = %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want none", removed)
	}
}

func TestRestoreWorktreeFailsCleanlyWhenTheBundleDownloadFails(t *testing.T) {
	withFakeHome(t)
	_, cloneDir := newBareRemote(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := api.NewClient(srv.URL, "anon-key")
	_, err := RestoreWorktree(context.Background(), client, srv.Client(), cloneDir, RestoreInput{
		SessionID:   "sess1",
		ProjectName: "acme-api",
		Snapshot: &api.ManifestSnapshot{
			ObjectKey:     "whatever",
			DownloadToken: "whatever",
		},
	})
	if err == nil {
		t.Fatal("RestoreWorktree() error = nil, want an error when the bundle download 404s")
	}
}

func TestShortID(t *testing.T) {
	if got := shortID("11111111-1111-4111-8111-111111111111"); got != "11111111" {
		t.Errorf("shortID() = %q, want %q", got, "11111111")
	}
	if got := shortID("short"); got != "short" {
		t.Errorf("shortID() = %q, want %q (unchanged, already shorter)", got, "short")
	}
}

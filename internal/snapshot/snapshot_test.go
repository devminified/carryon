package snapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devminified/carryon/internal/api"
)

// --- test fixtures -----------------------------------------------------

func execGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...) // #nosec G204 -- test fixture, fixed args
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v: %w: %s", args, err, out)
	}
	return nil
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	if err := execGit(dir, args...); err != nil {
		t.Fatalf("%v", err)
	}
}

func runGitCmdOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...) // #nosec G204 -- test fixture, fixed args
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("write %s: %v", path, err)
	}
}

// setupRepoWithRemote creates a real working repo with one commit
// (README.md), plus a real bare repo wired up as "origin" — a fully
// functional git remote over a file:// path, standing in for GitHub so
// these tests exercise a real `git push`, not a mock.
func setupRepoWithRemote(t *testing.T) string {
	t.Helper()
	remoteDir := t.TempDir()
	runGitCmd(t, remoteDir, "init", "--quiet", "--bare")

	repoDir := t.TempDir()
	runGitCmd(t, repoDir, "init", "--quiet")
	runGitCmd(t, repoDir, "config", "user.email", "test@devminified.com")
	runGitCmd(t, repoDir, "config", "user.name", "Carryon Test")
	runGitCmd(t, repoDir, "remote", "add", "origin", remoteDir)

	write(t, filepath.Join(repoDir, "README.md"), "hello\n")
	runGitCmd(t, repoDir, "add", "README.md")
	runGitCmd(t, repoDir, "commit", "--quiet", "-m", "initial")

	return repoDir
}

func lsTree(t *testing.T, repoDir, treeSHA string) []string {
	t.Helper()
	out := runGitCmdOutput(t, repoDir, "ls-tree", "-r", "--name-only", treeSHA)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

func contains(items []string, want string) bool {
	for _, it := range items {
		if it == want {
			return true
		}
	}
	return false
}

type fakeSnapshotBackend struct {
	mu    sync.Mutex
	calls []map[string]any
	fail  bool
}

func (b *fakeSnapshotBackend) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		b.mu.Lock()
		b.calls = append(b.calls, body)
		fail := b.fail
		b.mu.Unlock()

		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
}

func (b *fakeSnapshotBackend) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.calls)
}

func newTestSnapshotter(t *testing.T, srv *httptest.Server) *Snapshotter {
	t.Helper()
	client := api.NewClient(srv.URL, "anon-key")
	s, err := New(Config{
		ClientSource: func() (*api.Client, error) { return client, nil },
		TokenSource:  func(context.Context) (string, error) { return "fake-token", nil },
		CurrentUser:  func(context.Context) (string, error) { return "ali", nil },
		StateDir:     filepath.Join(t.TempDir(), "state"),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return s
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// --- tests ---------------------------------------------------------------

func TestMaybeSnapshotCreatesCommitAndPushesToHiddenRef(t *testing.T) {
	repoDir := setupRepoWithRemote(t)
	write(t, filepath.Join(repoDir, "new.txt"), "new content\n")

	backend := &fakeSnapshotBackend{}
	srv := backend.server()
	defer srv.Close()
	s := newTestSnapshotter(t, srv)

	if err := s.MaybeSnapshot(context.Background(), "sess1", repoDir, false); err != nil {
		t.Fatalf("MaybeSnapshot() error = %v", err)
	}

	if backend.callCount() != 1 {
		t.Fatalf("register-snapshot called %d times, want 1", backend.callCount())
	}
	call := backend.calls[0]
	if call["sessionId"] != "sess1" {
		t.Errorf("sessionId = %v, want sess1", call["sessionId"])
	}
	wipRef, _ := call["wipRef"].(string)
	if wipRef != "refs/carryon/ali/sess1" {
		t.Errorf("wipRef = %q, want %q", wipRef, "refs/carryon/ali/sess1")
	}

	out := runGitCmdOutput(t, repoDir, "ls-remote", "origin", "refs/carryon/ali/sess1")
	if out == "" {
		t.Error("ref not found on the remote after push")
	}
}

func TestMaybeSnapshotLeavesGitStatusUnchanged(t *testing.T) {
	// CO-201 testing point: "After a snapshot, git status output is
	// identical to before."
	repoDir := setupRepoWithRemote(t)
	write(t, filepath.Join(repoDir, "staged.txt"), "staged content\n")
	runGitCmd(t, repoDir, "add", "staged.txt")
	write(t, filepath.Join(repoDir, "unstaged.txt"), "unstaged content\n")

	before := runGitCmdOutput(t, repoDir, "status", "--porcelain")

	backend := &fakeSnapshotBackend{}
	srv := backend.server()
	defer srv.Close()
	s := newTestSnapshotter(t, srv)
	if err := s.MaybeSnapshot(context.Background(), "sess1", repoDir, false); err != nil {
		t.Fatalf("MaybeSnapshot() error = %v", err)
	}

	after := runGitCmdOutput(t, repoDir, "status", "--porcelain")
	if before != after {
		t.Errorf("git status changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestMaybeSnapshotConcurrentWithRealCommitNoLockErrors(t *testing.T) {
	// CO-201 testing point: "The developer runs git commit while a
	// snapshot is running: both succeed, with no lock errors."
	repoDir := setupRepoWithRemote(t)
	write(t, filepath.Join(repoDir, "file.txt"), "content\n")

	backend := &fakeSnapshotBackend{}
	srv := backend.server()
	defer srv.Close()
	s := newTestSnapshotter(t, srv)

	var wg sync.WaitGroup
	var snapErr, addErr, commitErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		snapErr = s.MaybeSnapshot(context.Background(), "sess1", repoDir, false)
	}()
	go func() {
		defer wg.Done()
		addErr = execGit(repoDir, "add", "file.txt")
		if addErr == nil {
			commitErr = execGit(repoDir, "commit", "--quiet", "-m", "real commit")
		}
	}()
	wg.Wait()

	if snapErr != nil {
		t.Errorf("MaybeSnapshot() error = %v", snapErr)
	}
	if addErr != nil {
		t.Errorf("real git add error = %v", addErr)
	}
	if commitErr != nil {
		t.Errorf("real git commit error = %v", commitErr)
	}
}

func TestMaybeSnapshotHandlesNewDeletedAndRenamedFiles(t *testing.T) {
	repoDir := setupRepoWithRemote(t)
	write(t, filepath.Join(repoDir, "to-delete.txt"), "bye\n")
	runGitCmd(t, repoDir, "add", "to-delete.txt")
	runGitCmd(t, repoDir, "commit", "--quiet", "-m", "add to-delete")

	if err := os.Remove(filepath.Join(repoDir, "to-delete.txt")); err != nil {
		t.Fatalf("removing to-delete.txt: %v", err)
	}
	if err := os.Rename(filepath.Join(repoDir, "README.md"), filepath.Join(repoDir, "RENAMED.md")); err != nil {
		t.Fatalf("renaming README.md: %v", err)
	}
	write(t, filepath.Join(repoDir, "new.txt"), "new\n")

	backend := &fakeSnapshotBackend{}
	srv := backend.server()
	defer srv.Close()
	s := newTestSnapshotter(t, srv)
	if err := s.MaybeSnapshot(context.Background(), "sess1", repoDir, false); err != nil {
		t.Fatalf("MaybeSnapshot() error = %v", err)
	}

	treeSHA, _ := backend.calls[0]["treeSha"].(string)
	files := lsTree(t, repoDir, treeSHA)

	for _, want := range []string{"new.txt", "RENAMED.md"} {
		if !contains(files, want) {
			t.Errorf("%q missing from the snapshot tree: %v", want, files)
		}
	}
	for _, unwanted := range []string{"to-delete.txt", "README.md"} {
		if contains(files, unwanted) {
			t.Errorf("%q still present in the snapshot tree, want it gone: %v", unwanted, files)
		}
	}
}

func TestMaybeSnapshotUnchangedTreeMakesNoPushAndNoNewRow(t *testing.T) {
	repoDir := setupRepoWithRemote(t)
	backend := &fakeSnapshotBackend{}
	srv := backend.server()
	defer srv.Close()
	s := newTestSnapshotter(t, srv)

	// final=true on both calls so the 5-minute rate limit never masks
	// what this test is actually checking (tree-unchanged dedup).
	if err := s.MaybeSnapshot(context.Background(), "sess1", repoDir, true); err != nil {
		t.Fatalf("first MaybeSnapshot() error = %v", err)
	}
	if backend.callCount() != 1 {
		t.Fatalf("register-snapshot called %d times after the first attempt, want 1", backend.callCount())
	}

	if err := s.MaybeSnapshot(context.Background(), "sess1", repoDir, true); err != nil {
		t.Fatalf("second MaybeSnapshot() error = %v", err)
	}
	if backend.callCount() != 1 {
		t.Errorf("register-snapshot called %d times across two unchanged attempts, want still 1", backend.callCount())
	}
}

func TestMaybeSnapshotRespectsGitignore(t *testing.T) {
	repoDir := setupRepoWithRemote(t)
	write(t, filepath.Join(repoDir, ".gitignore"), ".env\n")
	runGitCmd(t, repoDir, "add", ".gitignore")
	runGitCmd(t, repoDir, "commit", "--quiet", "-m", "add gitignore")
	write(t, filepath.Join(repoDir, ".env"), "SECRET=shh\n")
	write(t, filepath.Join(repoDir, "visible.txt"), "ok\n")

	backend := &fakeSnapshotBackend{}
	srv := backend.server()
	defer srv.Close()
	s := newTestSnapshotter(t, srv)
	if err := s.MaybeSnapshot(context.Background(), "sess1", repoDir, true); err != nil {
		t.Fatalf("MaybeSnapshot() error = %v", err)
	}

	treeSHA, _ := backend.calls[0]["treeSha"].(string)
	files := lsTree(t, repoDir, treeSHA)
	if contains(files, ".env") {
		t.Errorf(".env present in the snapshot tree, want it excluded via .gitignore: %v", files)
	}
	if !contains(files, "visible.txt") {
		t.Errorf("visible.txt missing from the snapshot tree: %v", files)
	}
}

func TestMaybeSnapshotExcludesLargeUntrackedFiles(t *testing.T) {
	orig := maxUntrackedFileSize
	maxUntrackedFileSize = 100 // shrink so this test doesn't need a real 50MB fixture
	defer func() { maxUntrackedFileSize = orig }()

	repoDir := setupRepoWithRemote(t)
	write(t, filepath.Join(repoDir, "big.bin"), strings.Repeat("x", 200))
	write(t, filepath.Join(repoDir, "small.txt"), "ok\n")

	backend := &fakeSnapshotBackend{}
	srv := backend.server()
	defer srv.Close()
	s := newTestSnapshotter(t, srv)
	if err := s.MaybeSnapshot(context.Background(), "sess1", repoDir, true); err != nil {
		t.Fatalf("MaybeSnapshot() error = %v", err)
	}

	treeSHA, _ := backend.calls[0]["treeSha"].(string)
	files := lsTree(t, repoDir, treeSHA)
	if contains(files, "big.bin") {
		t.Errorf("big.bin present in the snapshot tree, want it excluded (over the limit): %v", files)
	}
	if !contains(files, "small.txt") {
		t.Errorf("small.txt missing from the snapshot tree: %v", files)
	}

	p, err := Load(s.stateDir, "sess1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(p.ExcludedLargeFiles) != 1 || p.ExcludedLargeFiles[0] != "big.bin" {
		t.Errorf("ExcludedLargeFiles = %v, want [\"big.bin\"]", p.ExcludedLargeFiles)
	}
}

func TestMaybeSnapshotRateLimitsToFiveMinutesUnlessFinal(t *testing.T) {
	repoDir := setupRepoWithRemote(t)
	backend := &fakeSnapshotBackend{}
	srv := backend.server()
	defer srv.Close()

	clock := &fakeClock{now: time.Now()}
	client := api.NewClient(srv.URL, "anon-key")
	s, err := New(Config{
		ClientSource: func() (*api.Client, error) { return client, nil },
		TokenSource:  func(context.Context) (string, error) { return "tok", nil },
		CurrentUser:  func(context.Context) (string, error) { return "ali", nil },
		StateDir:     filepath.Join(t.TempDir(), "state"),
		now:          clock.Now,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	write(t, filepath.Join(repoDir, "a.txt"), "1\n")
	if err := s.MaybeSnapshot(context.Background(), "sess1", repoDir, false); err != nil {
		t.Fatalf("MaybeSnapshot() #1 error = %v", err)
	}
	if backend.callCount() != 1 {
		t.Fatalf("register-snapshot called %d times after attempt 1, want 1", backend.callCount())
	}

	write(t, filepath.Join(repoDir, "b.txt"), "2\n")
	clock.Advance(2 * time.Minute) // within the 5-minute window
	if err := s.MaybeSnapshot(context.Background(), "sess1", repoDir, false); err != nil {
		t.Fatalf("MaybeSnapshot() #2 error = %v", err)
	}
	if backend.callCount() != 1 {
		t.Errorf("register-snapshot called %d times within the 5-minute window, want still 1", backend.callCount())
	}

	clock.Advance(4 * time.Minute) // total 6 minutes since attempt 1
	if err := s.MaybeSnapshot(context.Background(), "sess1", repoDir, false); err != nil {
		t.Fatalf("MaybeSnapshot() #3 error = %v", err)
	}
	if backend.callCount() != 2 {
		t.Errorf("register-snapshot called %d times after the window elapsed, want 2", backend.callCount())
	}

	write(t, filepath.Join(repoDir, "c.txt"), "3\n")
	clock.Advance(10 * time.Second) // nowhere near 5 minutes
	if err := s.MaybeSnapshot(context.Background(), "sess1", repoDir, true); err != nil {
		t.Fatalf("MaybeSnapshot() #4 (final) error = %v", err)
	}
	if backend.callCount() != 3 {
		t.Errorf("register-snapshot called %d times after a final call, want 3 (final bypasses the gate)", backend.callCount())
	}
}

func TestMaybeSnapshotHandlesRepoWithNoCommitsYet(t *testing.T) {
	remoteDir := t.TempDir()
	runGitCmd(t, remoteDir, "init", "--quiet", "--bare")

	repoDir := t.TempDir()
	runGitCmd(t, repoDir, "init", "--quiet")
	runGitCmd(t, repoDir, "config", "user.email", "test@devminified.com")
	runGitCmd(t, repoDir, "config", "user.name", "Carryon Test")
	runGitCmd(t, repoDir, "remote", "add", "origin", remoteDir)
	write(t, filepath.Join(repoDir, "first.txt"), "hello\n")

	backend := &fakeSnapshotBackend{}
	srv := backend.server()
	defer srv.Close()
	s := newTestSnapshotter(t, srv)

	if err := s.MaybeSnapshot(context.Background(), "sess1", repoDir, true); err != nil {
		t.Fatalf("MaybeSnapshot() on a repo with no commits yet error = %v", err)
	}
	if backend.callCount() != 1 {
		t.Fatalf("register-snapshot called %d times, want 1", backend.callCount())
	}
	if headSHA, _ := backend.calls[0]["headSha"].(string); headSHA != "" {
		t.Errorf("headSha = %q, want empty for a repo with no commits yet", headSHA)
	}
}

func TestMaybeSnapshotNoRepoIsAQuietNoOp(t *testing.T) {
	dir := t.TempDir() // never git-inited
	backend := &fakeSnapshotBackend{}
	srv := backend.server()
	defer srv.Close()
	s := newTestSnapshotter(t, srv)

	if err := s.MaybeSnapshot(context.Background(), "sess1", dir, true); err != nil {
		t.Errorf("MaybeSnapshot() for a non-repo error = %v, want nil", err)
	}
	if backend.callCount() != 0 {
		t.Errorf("register-snapshot called %d times for a non-repo, want 0", backend.callCount())
	}
}

func TestNewValidatesRequiredFields(t *testing.T) {
	validClientSource := func() (*api.Client, error) { return api.NewClient("http://local", "key"), nil }
	validTokenSource := func(context.Context) (string, error) { return "tok", nil }
	validCurrentUser := func(context.Context) (string, error) { return "ali", nil }

	if _, err := New(Config{}); err == nil {
		t.Error("New(Config{}) = nil error, want an error (missing required fields)")
	}
	if _, err := New(Config{TokenSource: validTokenSource, CurrentUser: validCurrentUser, StateDir: t.TempDir()}); err == nil {
		t.Error("New() without ClientSource = nil error, want an error")
	}
	if _, err := New(Config{ClientSource: validClientSource, CurrentUser: validCurrentUser, StateDir: t.TempDir()}); err == nil {
		t.Error("New() without TokenSource = nil error, want an error")
	}
	if _, err := New(Config{ClientSource: validClientSource, TokenSource: validTokenSource, StateDir: t.TempDir()}); err == nil {
		t.Error("New() without CurrentUser = nil error, want an error")
	}
	if _, err := New(Config{ClientSource: validClientSource, TokenSource: validTokenSource, CurrentUser: validCurrentUser}); err == nil {
		t.Error("New() without StateDir = nil error, want an error")
	}
}

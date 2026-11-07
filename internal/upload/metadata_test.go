package upload

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) // #nosec G204 -- test fixture, fixed args
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func initRepoWithCommit(t *testing.T, dir string) string {
	t.Helper()
	runGit(t, dir, "init", "--quiet")
	runGit(t, dir, "config", "user.email", "test@devminified.com")
	runGit(t, dir, "config", "user.name", "Carryon Test")
	runGit(t, dir, "commit", "--quiet", "--allow-empty", "-m", "initial")

	cmd := exec.Command("git", "rev-parse", "HEAD") // #nosec G204 -- test fixture, fixed args
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return string(out[:len(out)-1]) // trim trailing newline
}

func TestGatherMetadataNoGitRepo(t *testing.T) {
	dir := t.TempDir() // never git-inited

	got := gatherMetadata(context.Background(), dir)
	if got.Branch != "" {
		t.Errorf("Branch = %q, want empty for a non-repo", got.Branch)
	}
	if got.HeadSHA != "" {
		t.Errorf("HeadSHA = %q, want empty for a non-repo", got.HeadSHA)
	}
	if got.Machine == "" {
		t.Error("Machine = \"\", want a real hostname regardless of git state")
	}
}

func TestGatherMetadataRealGitRepo(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--quiet")
	runGit(t, dir, "checkout", "--quiet", "-b", "feature/co-111")
	sha := initRepoWithCommit(t, dir)

	got := gatherMetadata(context.Background(), dir)
	if got.Branch != "feature/co-111" {
		t.Errorf("Branch = %q, want %q", got.Branch, "feature/co-111")
	}
	if got.HeadSHA != sha {
		t.Errorf("HeadSHA = %q, want %q", got.HeadSHA, sha)
	}
}

// resetClaudeVersionCache clears the package-level cache before a test
// and restores whatever was there before once it's done, so tests in
// this file can't leak cache state into each other or into real usage of
// this package within the same test binary.
func resetClaudeVersionCache(t *testing.T) {
	t.Helper()
	claudeVersionMu.Lock()
	prevVersion, prevAt := claudeVersion, claudeVersionAt
	claudeVersion, claudeVersionAt = "", time.Time{}
	claudeVersionMu.Unlock()

	t.Cleanup(func() {
		claudeVersionMu.Lock()
		claudeVersion, claudeVersionAt = prevVersion, prevAt
		claudeVersionMu.Unlock()
	})
}

// fakeClaudeOnPath writes an executable "claude" script that prints
// version to stdout for --version, and prepends its directory to PATH
// for the duration of the test — so cachedClaudeVersion exercises a real
// exec.Command call, not a mocked one.
func fakeClaudeOnPath(t *testing.T, version string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake claude script is a POSIX shell script")
	}

	dir := t.TempDir()
	script := "#!/bin/sh\necho '" + version + "'\n"
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { // #nosec G306 -- test fixture, needs +x
		t.Fatalf("writing fake claude script: %v", err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCachedClaudeVersionReadsRealOutput(t *testing.T) {
	resetClaudeVersionCache(t)
	fakeClaudeOnPath(t, "1.2.3 (Claude Code)")

	got := cachedClaudeVersion(context.Background())
	if got != "1.2.3" {
		t.Errorf("cachedClaudeVersion() = %q, want just the version token %q", got, "1.2.3")
	}
}

func TestCachedClaudeVersionServesFromCacheWithinTTL(t *testing.T) {
	resetClaudeVersionCache(t)
	fakeClaudeOnPath(t, "1.0.0")

	first := cachedClaudeVersion(context.Background())
	if first != "1.0.0" {
		t.Fatalf("first call = %q, want %q", first, "1.0.0")
	}

	// A change on disk after the first call must not be observed again
	// until the cache expires — "cached per day" (CO-111).
	fakeClaudeOnPath(t, "2.0.0")

	second := cachedClaudeVersion(context.Background())
	if second != "1.0.0" {
		t.Errorf("second call = %q, want the cached %q (not re-run)", second, "1.0.0")
	}
}

func TestCachedClaudeVersionKeepsLastKnownGoodOnFailure(t *testing.T) {
	resetClaudeVersionCache(t)
	claudeVersionMu.Lock()
	claudeVersion = "1.0.0"
	claudeVersionAt = time.Now().Add(-2 * claudeVersionCacheTTL) // force a refresh attempt
	claudeVersionMu.Unlock()

	t.Setenv("PATH", t.TempDir()) // "claude" is nowhere on this PATH

	got := cachedClaudeVersion(context.Background())
	if got != "1.0.0" {
		t.Errorf("cachedClaudeVersion() = %q, want the last known-good %q on exec failure", got, "1.0.0")
	}
}

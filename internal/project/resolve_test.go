package project

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initRepo creates a real git repository at dir (git init only, no
// remotes) — these tests exercise ResolveRemote against actual git
// invocations, not a mocked git, matching how it behaves for real.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init", "--quiet")
	// A fresh `git init` repo has no commits, which is fine — rev-parse
	// --show-toplevel and remote config don't require any.
}

func addRemote(t *testing.T, dir, name, url string) {
	t.Helper()
	runGit(t, dir, "remote", "add", name, url)
}

// commit makes one empty commit in dir, configuring a local git identity
// first — a bare `git init` repo has no identity configured, and CI/
// sandboxed machines can't be assumed to have one globally either.
func commit(t *testing.T, dir, message string) string {
	t.Helper()
	runGit(t, dir, "config", "user.email", "test@devminified.com")
	runGit(t, dir, "config", "user.name", "Carryon Test")
	runGit(t, dir, "commit", "--quiet", "--allow-empty", "-m", message)

	cmd := exec.Command("git", "rev-parse", "HEAD") // #nosec G204 -- test fixture, fixed args
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) // #nosec G204 -- test fixture, fixed args
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestResolveRemoteNotARepo(t *testing.T) {
	dir := t.TempDir() // a plain directory, never git-inited — stands in for ~ or /tmp

	_, err := ResolveRemote(context.Background(), dir)
	if !errors.Is(err, ErrNoRepo) {
		t.Errorf("ResolveRemote() error = %v, want ErrNoRepo", err)
	}
}

func TestResolveRemoteNoRemotes(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	_, err := ResolveRemote(context.Background(), dir)
	if !errors.Is(err, ErrNoRemote) {
		t.Errorf("ResolveRemote() error = %v, want ErrNoRemote", err)
	}
}

func TestResolveRemoteUsesOrigin(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	addRemote(t, dir, "origin", "git@github.com:devminified/acme-api.git")

	got, err := ResolveRemote(context.Background(), dir)
	if err != nil {
		t.Fatalf("ResolveRemote() error = %v", err)
	}
	if want := "github.com/devminified/acme-api"; got != want {
		t.Errorf("ResolveRemote() = %q, want %q", got, want)
	}
}

func TestResolveRemoteFallsBackToFirstRemoteWhenNoOrigin(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	addRemote(t, dir, "upstream", "https://github.com/devminified/acme-api")

	got, err := ResolveRemote(context.Background(), dir)
	if err != nil {
		t.Fatalf("ResolveRemote() error = %v", err)
	}
	if want := "github.com/devminified/acme-api"; got != want {
		t.Errorf("ResolveRemote() = %q, want %q", got, want)
	}
}

func TestResolveRemoteFromNestedSubfolderMatchesToplevel(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	addRemote(t, dir, "origin", "git@github.com:devminified/acme-api.git")

	nested := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	gotTop, err := ResolveRemote(context.Background(), dir)
	if err != nil {
		t.Fatalf("ResolveRemote(toplevel) error = %v", err)
	}
	gotNested, err := ResolveRemote(context.Background(), nested)
	if err != nil {
		t.Fatalf("ResolveRemote(nested) error = %v", err)
	}
	if gotTop != gotNested {
		t.Errorf("ResolveRemote(toplevel) = %q, ResolveRemote(nested) = %q, want equal", gotTop, gotNested)
	}
}

func TestResolveRemoteEmptyCwd(t *testing.T) {
	_, err := ResolveRemote(context.Background(), "")
	if !errors.Is(err, ErrNoRepo) {
		t.Errorf("ResolveRemote(\"\") error = %v, want ErrNoRepo", err)
	}
}

func TestCurrentBranchNotARepo(t *testing.T) {
	dir := t.TempDir()

	if _, err := CurrentBranch(context.Background(), dir); err == nil {
		t.Error("CurrentBranch() error = nil for a non-repo, want an error")
	}
}

func TestCurrentBranchReturnsTheCheckedOutBranch(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	runGit(t, dir, "checkout", "--quiet", "-b", "feature/co-111")
	commit(t, dir, "initial")

	got, err := CurrentBranch(context.Background(), dir)
	if err != nil {
		t.Fatalf("CurrentBranch() error = %v", err)
	}
	if want := "feature/co-111"; got != want {
		t.Errorf("CurrentBranch() = %q, want %q", got, want)
	}
}

func TestCurrentBranchDetachedHEAD(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	sha := commit(t, dir, "initial")
	runGit(t, dir, "checkout", "--quiet", sha)

	got, err := CurrentBranch(context.Background(), dir)
	if err != nil {
		t.Fatalf("CurrentBranch() error = %v", err)
	}
	if want := "HEAD"; got != want {
		t.Errorf("CurrentBranch() = %q, want %q (detached)", got, want)
	}
}

func TestHeadSHANotARepo(t *testing.T) {
	dir := t.TempDir()

	if _, err := HeadSHA(context.Background(), dir); err == nil {
		t.Error("HeadSHA() error = nil for a non-repo, want an error")
	}
}

func TestHeadSHANoCommitsYet(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)

	if _, err := HeadSHA(context.Background(), dir); err == nil {
		t.Error("HeadSHA() error = nil for a repo with no commits, want an error")
	}
}

func TestHeadSHAMatchesTheLatestCommit(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	sha := commit(t, dir, "initial")

	got, err := HeadSHA(context.Background(), dir)
	if err != nil {
		t.Fatalf("HeadSHA() error = %v", err)
	}
	if got != sha {
		t.Errorf("HeadSHA() = %q, want %q", got, sha)
	}
}

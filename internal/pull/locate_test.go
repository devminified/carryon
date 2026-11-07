package pull

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func withFakeHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}

func runGitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...) // #nosec G204 -- test fixture, fixed args
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v (dir=%s): %v\n%s", args, dir, err, out)
	}
	return string(out)
}

func initRepoWithRemote(t *testing.T, dir, remoteName, remoteURL string) {
	t.Helper()
	runGitCmd(t, dir, "init", "--quiet")
	runGitCmd(t, dir, "config", "user.email", "test@devminified.com")
	runGitCmd(t, dir, "config", "user.name", "Carryon Test")
	if remoteURL != "" {
		runGitCmd(t, dir, "remote", "add", remoteName, remoteURL)
	}
}

func commitFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	runGitCmd(t, dir, "add", name)
	runGitCmd(t, dir, "commit", "--quiet", "-m", "commit "+name)
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return string(out[:len(out)-1]) // trim trailing newline
}

// newBareRemote creates a bare repo standing in for GitHub, plus a
// clone of it with one commit already pushed — the shape every test
// below starts from.
func newBareRemote(t *testing.T) (bareDir, cloneDir string) {
	t.Helper()
	bareDir = t.TempDir()
	runGitCmd(t, bareDir, "init", "--quiet", "--bare")

	cloneDir = t.TempDir()
	initRepoWithRemote(t, cloneDir, "origin", bareDir)
	commitFile(t, cloneDir, "README.md", "hello\n")
	runGitCmd(t, cloneDir, "push", "--quiet", "origin", "HEAD:refs/heads/main")
	// A fresh `git init --bare` defaults its symbolic HEAD to whatever
	// init.defaultBranch says (master, in some environments) — not
	// necessarily "main", which is the only branch that actually
	// exists here. Without this, `git clone` checks out nothing.
	runGitCmd(t, bareDir, "symbolic-ref", "HEAD", "refs/heads/main")
	return bareDir, cloneDir
}

func TestLocateCloneFindsCwdWhenRemoteMatches(t *testing.T) {
	cloneDir := t.TempDir()
	initRepoWithRemote(t, cloneDir, "origin", "git@example.test:devminified/acme-api.git")
	commitFile(t, cloneDir, "README.md", "hello\n")

	got, err := LocateClone(context.Background(), LocateInput{
		Cwd:             cloneDir,
		CanonicalRemote: "example.test/devminified/acme-api",
	}, nil)
	if err != nil {
		t.Fatalf("LocateClone() error = %v", err)
	}
	gotReal, _ := filepath.EvalSymlinks(got.Path)
	wantReal, _ := filepath.EvalSymlinks(cloneDir)
	if gotReal != wantReal {
		t.Errorf("Path = %q, want %q", got.Path, cloneDir)
	}
	if got.Cloned {
		t.Error("Cloned = true, want false (found via cwd)")
	}
}

func TestLocateCloneFindsCwdFromNestedSubfolder(t *testing.T) {
	bareDir := t.TempDir()
	runGitCmd(t, bareDir, "init", "--quiet", "--bare")

	cloneDir := t.TempDir()
	initRepoWithRemote(t, cloneDir, "origin", "git@example.test:devminified/acme-api.git")
	commitFile(t, cloneDir, "README.md", "hello\n")

	nested := filepath.Join(cloneDir, "a", "b")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := LocateClone(context.Background(), LocateInput{
		Cwd:             nested,
		CanonicalRemote: "example.test/devminified/acme-api",
	}, nil)
	if err != nil {
		t.Fatalf("LocateClone() error = %v", err)
	}
	gotReal, _ := filepath.EvalSymlinks(got.Path)
	wantReal, _ := filepath.EvalSymlinks(cloneDir)
	if gotReal != wantReal {
		t.Errorf("Path = %q, want %q", got.Path, cloneDir)
	}
	if got.Cloned {
		t.Error("Cloned = true, want false (found via cwd)")
	}
}

func TestLocateCloneFallsBackToReposMapWhenCwdDoesntMatch(t *testing.T) {
	_, cloneDir := newBareRemote(t)
	// Give the clone a real-looking remote so its canonical form is
	// deterministic and doesn't depend on a bare local path.
	runGitCmd(t, cloneDir, "remote", "set-url", "origin", "git@example.test:devminified/acme-api.git")

	elsewhere := t.TempDir() // cwd is NOT the project at all

	got, err := LocateClone(context.Background(), LocateInput{
		Cwd:             elsewhere,
		CanonicalRemote: "example.test/devminified/acme-api",
		Repos:           map[string]string{"example.test/devminified/acme-api": cloneDir},
	}, nil)
	if err != nil {
		t.Fatalf("LocateClone() error = %v", err)
	}
	if got.Path != cloneDir {
		t.Errorf("Path = %q, want %q", got.Path, cloneDir)
	}
	if got.Cloned {
		t.Error("Cloned = true, want false (found via Repos)")
	}
}

func TestLocateCloneIgnoresAStaleReposEntry(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "does-not-exist")
	elsewhere := t.TempDir()

	called := false
	_, err := LocateClone(context.Background(), LocateInput{
		Cwd:             elsewhere,
		CanonicalRemote: "example.test/devminified/acme-api",
		Repos:           map[string]string{"example.test/devminified/acme-api": gone},
	}, func() bool { called = true; return false })
	if err == nil {
		t.Fatal("LocateClone() error = nil, want ErrCloneDeclined (stale Repos entry should fall through to the clone offer)")
	}
	if !called {
		t.Error("confirm was never called — a stale Repos entry should still fall through to offering a clone")
	}
}

func TestLocateCloneIgnoresAReposEntryWithTheWrongRemote(t *testing.T) {
	_, wrongProjectClone := newBareRemote(t)
	runGitCmd(t, wrongProjectClone, "remote", "set-url", "origin", "git@example.test:devminified/other-repo.git")
	elsewhere := t.TempDir()

	called := false
	_, err := LocateClone(context.Background(), LocateInput{
		Cwd:             elsewhere,
		CanonicalRemote: "example.test/devminified/acme-api",
		Repos:           map[string]string{"example.test/devminified/acme-api": wrongProjectClone},
	}, func() bool { called = true; return false })
	if err == nil {
		t.Fatal("LocateClone() error = nil, want ErrCloneDeclined")
	}
	if !called {
		t.Error("confirm was never called — a Repos entry for the wrong remote should fall through to offering a clone")
	}
}

func TestLocateCloneDeclinedReturnsErrCloneDeclined(t *testing.T) {
	elsewhere := t.TempDir()
	_, err := LocateClone(context.Background(), LocateInput{
		Cwd:             elsewhere,
		CanonicalRemote: "example.test/devminified/acme-api",
	}, func() bool { return false })
	if err != ErrCloneDeclined {
		t.Errorf("LocateClone() error = %v, want ErrCloneDeclined", err)
	}
}

func TestLocateCloneNilConfirmActsAsDeclined(t *testing.T) {
	elsewhere := t.TempDir()
	_, err := LocateClone(context.Background(), LocateInput{
		Cwd:             elsewhere,
		CanonicalRemote: "example.test/devminified/acme-api",
	}, nil)
	if err != ErrCloneDeclined {
		t.Errorf("LocateClone() error = %v, want ErrCloneDeclined", err)
	}
}

func TestLocateCloneClonesWhenConfirmed(t *testing.T) {
	home := withFakeHome(t)
	bareDir, cloneDir := newBareRemote(t)
	_ = cloneDir
	elsewhere := t.TempDir()

	got, err := LocateClone(context.Background(), LocateInput{
		Cwd:             elsewhere,
		ProjectName:     "acme-api",
		CanonicalRemote: "irrelevant-for-this-test",
		GitRemoteURL:    bareDir,
	}, func() bool { return true })
	if err != nil {
		t.Fatalf("LocateClone() error = %v", err)
	}
	if !got.Cloned {
		t.Error("Cloned = false, want true")
	}
	wantPath := filepath.Join(home, "carryon", "repos", "acme-api")
	if got.Path != wantPath {
		t.Errorf("Path = %q, want %q", got.Path, wantPath)
	}
	if _, err := os.Stat(filepath.Join(got.Path, "README.md")); err != nil {
		t.Errorf("cloned repo missing README.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(got.Path, ".git")); err != nil {
		t.Errorf("cloned path isn't a git repo: %v", err)
	}
}

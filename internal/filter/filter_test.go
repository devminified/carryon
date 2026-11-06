package filter

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/devminified/carryon/internal/agent"
	"github.com/devminified/carryon/internal/api"
)

// spyUploader is the "file-access spy" CO-108's testing points ask for:
// Filter structurally cannot let a session reach a transcript read (which
// only Inner would ever do, once CO-109/CO-110 build it) without calling
// Inner.Upload first, so asserting Inner was never called for a skipped
// session is exactly that proof.
type spyUploader struct {
	mu    sync.Mutex
	calls []agent.Session
}

func (s *spyUploader) Upload(_ context.Context, session agent.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, session)
	return nil
}

func (s *spyUploader) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func notFoundErr() error {
	return &api.Error{StatusCode: http.StatusNotFound, Code: "not_found", Message: "no such project"}
}

func forbiddenErr() error {
	return &api.Error{StatusCode: http.StatusForbidden, Code: "forbidden", Message: "not a member"}
}

// initRepo and addRemote mirror internal/project's own test helpers —
// duplicated rather than exported from internal/project, since these are
// test-only fixtures, not part of that package's real API.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init", "--quiet")
}

func addRemote(t *testing.T, dir, name, url string) {
	t.Helper()
	runGit(t, dir, "remote", "add", name, url)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) // #nosec G204 -- test fixture, fixed args
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

type countingMatcher struct {
	mu    sync.Mutex
	calls int
	fn    func(ctx context.Context, remote string) (*api.MatchProjectResult, error)
}

func (m *countingMatcher) match(ctx context.Context, remote string) (*api.MatchProjectResult, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	return m.fn(ctx, remote)
}

func (m *countingMatcher) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func TestFilterSkipsSessionWithNoGitRepo(t *testing.T) {
	dir := t.TempDir() // plain dir, no git init — stands in for ~ or /tmp
	inner := &spyUploader{}
	f := New(Config{
		Inner:        inner,
		MatchProject: func(context.Context, string) (*api.MatchProjectResult, error) { t.Fatal("MatchProject should never be called"); return nil, nil },
	})

	if err := f.Upload(context.Background(), agent.Session{ID: "s1", Cwd: dir}); err != nil {
		t.Errorf("Upload() error = %v, want nil (skip, not a failure)", err)
	}
	if inner.callCount() != 0 {
		t.Errorf("Inner.Upload called %d times, want 0", inner.callCount())
	}
}

func TestFilterSkipsRepoWithNoRemotes(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	inner := &spyUploader{}
	f := New(Config{
		Inner:        inner,
		MatchProject: func(context.Context, string) (*api.MatchProjectResult, error) { t.Fatal("MatchProject should never be called"); return nil, nil },
	})

	if err := f.Upload(context.Background(), agent.Session{ID: "s1", Cwd: dir}); err != nil {
		t.Errorf("Upload() error = %v, want nil", err)
	}
	if inner.callCount() != 0 {
		t.Errorf("Inner.Upload called %d times, want 0", inner.callCount())
	}
}

func TestFilterUploadsRegisteredProject(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	addRemote(t, dir, "origin", "git@github.com:devminified/acme-api.git")

	inner := &spyUploader{}
	f := New(Config{
		Inner: inner,
		MatchProject: func(_ context.Context, remote string) (*api.MatchProjectResult, error) {
			if remote != "github.com/devminified/acme-api" {
				t.Errorf("MatchProject called with %q, want the normalized remote", remote)
			}
			return &api.MatchProjectResult{ProjectID: "p1", Role: "resumer"}, nil
		},
	})

	if err := f.Upload(context.Background(), agent.Session{ID: "s1", Cwd: dir}); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if inner.callCount() != 1 {
		t.Errorf("Inner.Upload called %d times, want 1", inner.callCount())
	}
	if got := inner.calls[0].ProjectID; got != "p1" {
		t.Errorf("Inner.Upload received ProjectID = %q, want %q (from the match-project result)", got, "p1")
	}
}

func TestFilterNestedSubfolderOfRegisteredRepoUploads(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	addRemote(t, dir, "origin", "git@github.com:devminified/acme-api.git")
	nested := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	inner := &spyUploader{}
	f := New(Config{
		Inner: inner,
		MatchProject: func(context.Context, string) (*api.MatchProjectResult, error) {
			return &api.MatchProjectResult{ProjectID: "p1", Role: "resumer"}, nil
		},
	})

	if err := f.Upload(context.Background(), agent.Session{ID: "s1", Cwd: nested}); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if inner.callCount() != 1 {
		t.Errorf("Inner.Upload called %d times, want 1", inner.callCount())
	}
}

func TestFilterSkipsUnregisteredRemoteAndLogsOnce(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	addRemote(t, dir, "origin", "git@github.com:devminified/acme-api.git")

	inner := &spyUploader{}
	var logMu sync.Mutex
	var logs []string
	f := New(Config{
		Inner:        inner,
		MatchProject: func(context.Context, string) (*api.MatchProjectResult, error) { return nil, notFoundErr() },
		logf: func(msg string) {
			logMu.Lock()
			defer logMu.Unlock()
			logs = append(logs, msg)
		},
	})

	for i := 0; i < 3; i++ {
		if err := f.Upload(context.Background(), agent.Session{ID: "s1", Cwd: dir}); err != nil {
			t.Fatalf("Upload() #%d error = %v, want nil", i, err)
		}
	}

	if inner.callCount() != 0 {
		t.Errorf("Inner.Upload called %d times, want 0", inner.callCount())
	}
	logMu.Lock()
	defer logMu.Unlock()
	if len(logs) != 1 {
		t.Fatalf("logged %d times across 3 uploads of the same remote, want exactly 1: %v", len(logs), logs)
	}
	want := "Repo github.com/devminified/acme-api is not registered with Carryon"
	if logs[0] != want {
		t.Errorf("log message = %q, want %q", logs[0], want)
	}
}

func TestFilterSkipsRegisteredRemoteCallerIsNotAMemberOf(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	addRemote(t, dir, "origin", "git@github.com:devminified/acme-api.git")

	inner := &spyUploader{}
	f := New(Config{
		Inner:        inner,
		MatchProject: func(context.Context, string) (*api.MatchProjectResult, error) { return nil, forbiddenErr() },
	})

	if err := f.Upload(context.Background(), agent.Session{ID: "s1", Cwd: dir}); err != nil {
		t.Errorf("Upload() error = %v, want nil", err)
	}
	if inner.callCount() != 0 {
		t.Errorf("Inner.Upload called %d times, want 0", inner.callCount())
	}
}

func TestFilterReturnsErrorOnTransientLookupFailure(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	addRemote(t, dir, "origin", "git@github.com:devminified/acme-api.git")

	inner := &spyUploader{}
	wantErr := errors.New("boom: network unreachable")
	f := New(Config{
		Inner:        inner,
		MatchProject: func(context.Context, string) (*api.MatchProjectResult, error) { return nil, wantErr },
	})

	err := f.Upload(context.Background(), agent.Session{ID: "s1", Cwd: dir})
	if err == nil {
		t.Fatal("Upload() error = nil, want a non-nil error so the agent retries")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("Upload() error = %v, want it to wrap %v", err, wantErr)
	}
	if inner.callCount() != 0 {
		t.Errorf("Inner.Upload called %d times, want 0", inner.callCount())
	}
}

func TestFilterCacheHitMakesNoNetworkCall(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	addRemote(t, dir, "origin", "git@github.com:devminified/acme-api.git")

	inner := &spyUploader{}
	matcher := &countingMatcher{fn: func(context.Context, string) (*api.MatchProjectResult, error) {
		return &api.MatchProjectResult{ProjectID: "p1", Role: "resumer"}, nil
	}}
	f := New(Config{Inner: inner, MatchProject: matcher.match})

	for i := 0; i < 5; i++ {
		if err := f.Upload(context.Background(), agent.Session{ID: "s1", Cwd: dir}); err != nil {
			t.Fatalf("Upload() #%d error = %v", i, err)
		}
	}

	if matcher.count() != 1 {
		t.Errorf("MatchProject called %d times across 5 uploads, want exactly 1 (the rest should be cache hits)", matcher.count())
	}
	if inner.callCount() != 5 {
		t.Errorf("Inner.Upload called %d times, want 5 (caching the lookup doesn't skip the actual upload)", inner.callCount())
	}
}

func TestFilterCacheExpiresAfterTTL(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	addRemote(t, dir, "origin", "git@github.com:devminified/acme-api.git")

	now := time.Now()
	matcher := &countingMatcher{fn: func(context.Context, string) (*api.MatchProjectResult, error) {
		return &api.MatchProjectResult{ProjectID: "p1", Role: "resumer"}, nil
	}}
	f := New(Config{
		Inner:        &spyUploader{},
		MatchProject: matcher.match,
		CacheTTL:     time.Hour,
		now:          func() time.Time { return now },
	})

	if err := f.Upload(context.Background(), agent.Session{ID: "s1", Cwd: dir}); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if matcher.count() != 1 {
		t.Fatalf("MatchProject called %d times, want 1", matcher.count())
	}

	now = now.Add(59 * time.Minute)
	if err := f.Upload(context.Background(), agent.Session{ID: "s1", Cwd: dir}); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if matcher.count() != 1 {
		t.Errorf("MatchProject called %d times before TTL expiry, want still 1", matcher.count())
	}

	now = now.Add(2 * time.Minute) // total 61 minutes since the first lookup
	if err := f.Upload(context.Background(), agent.Session{ID: "s1", Cwd: dir}); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if matcher.count() != 2 {
		t.Errorf("MatchProject called %d times after TTL expiry, want 2", matcher.count())
	}
}

func TestNewPanicsWithoutRequiredFields(t *testing.T) {
	mustPanic := func(t *testing.T, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Error("expected a panic, got none")
			}
		}()
		fn()
	}

	mustPanic(t, func() { New(Config{}) })
	mustPanic(t, func() { New(Config{Inner: &spyUploader{}}) })
}

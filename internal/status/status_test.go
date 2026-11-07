package status

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/devminified/carryon/internal/agent"
	"github.com/devminified/carryon/internal/auth"
	"github.com/devminified/carryon/internal/filter"
	"github.com/devminified/carryon/internal/snapshot"
	"github.com/devminified/carryon/internal/state"
)

type fakeAuthChecker struct {
	loggedIn bool
	user     *auth.User
	userErr  error
}

func (f *fakeAuthChecker) LoggedIn() bool { return f.loggedIn }
func (f *fakeAuthChecker) CurrentUser(context.Context) (*auth.User, error) {
	if f.userErr != nil {
		return nil, f.userErr
	}
	return f.user, nil
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) // #nosec G204 -- test fixture, fixed args
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestGatherNotLoggedIn(t *testing.T) {
	r := Gather(context.Background(), "http://local", &fakeAuthChecker{loggedIn: false}, Paths{})
	if r.LoggedIn {
		t.Error("LoggedIn = true, want false")
	}
	if r.User != nil {
		t.Errorf("User = %+v, want nil", r.User)
	}
}

func TestGatherLoggedInWithUser(t *testing.T) {
	r := Gather(context.Background(), "http://local", &fakeAuthChecker{
		loggedIn: true,
		user:     &auth.User{Email: "ali@devminified.com", UserName: "ali"},
	}, Paths{})
	if !r.LoggedIn {
		t.Error("LoggedIn = false, want true")
	}
	if r.User == nil || r.User.Email != "ali@devminified.com" {
		t.Errorf("User = %+v, want ali@devminified.com", r.User)
	}
}

func TestGatherLoggedInButUserFetchFailsStillReportsLoggedIn(t *testing.T) {
	r := Gather(context.Background(), "http://local", &fakeAuthChecker{
		loggedIn: true,
		userErr:  errors.New("offline"),
	}, Paths{})
	if !r.LoggedIn {
		t.Error("LoggedIn = false, want true (user-fetch failure must not change this)")
	}
	if r.User != nil {
		t.Errorf("User = %+v, want nil when the fetch failed", r.User)
	}
}

func TestGatherAgentNotRunningWhenStatusFileMissing(t *testing.T) {
	paths := Paths{AgentStatusPath: filepath.Join(t.TempDir(), "agent-status.json")}
	r := Gather(context.Background(), "http://local", &fakeAuthChecker{}, paths)
	if r.Agent.Running {
		t.Error("Agent.Running = true, want false for a missing status file")
	}
	if len(r.Sessions) != 0 {
		t.Errorf("Sessions = %v, want empty when the agent isn't running", r.Sessions)
	}
}

func TestGatherAgentNotRunningWhenStatusFileIsStale(t *testing.T) {
	dir := t.TempDir()
	statusPath := filepath.Join(dir, "agent-status.json")
	writeJSON(t, statusPath, agent.StatusFile{
		UpdatedAt: time.Now().Add(-time.Hour), // long past staleAfter
		Version:   "1.0.0",
		StartedAt: time.Now().Add(-2 * time.Hour),
	})

	r := Gather(context.Background(), "http://local", &fakeAuthChecker{}, Paths{AgentStatusPath: statusPath})
	if r.Agent.Running {
		t.Error("Agent.Running = true, want false for a stale status file")
	}
}

func TestGatherAgentRunningReportsVersionAndUptime(t *testing.T) {
	dir := t.TempDir()
	statusPath := filepath.Join(dir, "agent-status.json")
	startedAt := time.Now().Add(-90 * time.Minute)
	writeJSON(t, statusPath, agent.StatusFile{
		UpdatedAt: time.Now(),
		Version:   "1.2.3",
		StartedAt: startedAt,
	})

	r := Gather(context.Background(), "http://local", &fakeAuthChecker{}, Paths{AgentStatusPath: statusPath})
	if !r.Agent.Running {
		t.Fatal("Agent.Running = false, want true for a fresh status file")
	}
	if r.Agent.Version != "1.2.3" {
		t.Errorf("Version = %q, want %q", r.Agent.Version, "1.2.3")
	}
	if r.Agent.UptimeSeconds < 89*60 || r.Agent.UptimeSeconds > 91*60 {
		t.Errorf("UptimeSeconds = %d, want roughly %d", r.Agent.UptimeSeconds, 90*60)
	}
}

func TestGatherSessionsIncludeBlockedReasonAndBytesBehind(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")

	repoDir := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repoDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runGit(t, repoDir, "init", "--quiet")
	runGit(t, repoDir, "remote", "add", "origin", "git@github.com:devminified/acme-api.git")

	transcriptPath := filepath.Join(dir, "s1.jsonl")
	content := []byte(`{"line":1}` + "\n" + `{"line":2}` + "\n")
	if err := os.WriteFile(transcriptPath, content, 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	// Only the first line has been confirmed uploaded so far.
	if err := state.Save(stateDir, "s1", state.Pointer{Offset: 10}); err != nil {
		t.Fatalf("state.Save: %v", err)
	}

	lastUpload := time.Now().Add(-5 * time.Minute)
	statusPath := filepath.Join(dir, "agent-status.json")
	writeJSON(t, statusPath, agent.StatusFile{
		UpdatedAt: time.Now(),
		StartedAt: time.Now(),
		Sessions: []agent.SessionStatus{
			{SessionID: "s1", Cwd: repoDir, TranscriptPath: transcriptPath, LastUploadAt: &lastUpload},
			{SessionID: "s2", Cwd: repoDir, TranscriptPath: filepath.Join(dir, "missing.jsonl"), Blocked: true, BlockedReason: "forbidden"},
		},
	})

	r := Gather(context.Background(), "http://local", &fakeAuthChecker{}, Paths{AgentStatusPath: statusPath, StateDir: stateDir})
	if len(r.Sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(r.Sessions))
	}

	byID := map[string]SessionInfo{}
	for _, s := range r.Sessions {
		byID[s.SessionID] = s
	}

	s1 := byID["s1"]
	if s1.Project != "github.com/devminified/acme-api" {
		t.Errorf("s1.Project = %q, want the resolved remote", s1.Project)
	}
	if s1.BytesBehind != int64(len(content))-10 {
		t.Errorf("s1.BytesBehind = %d, want %d", s1.BytesBehind, int64(len(content))-10)
	}
	if s1.Blocked {
		t.Error("s1.Blocked = true, want false")
	}

	s2 := byID["s2"]
	if !s2.Blocked || s2.BlockedReason != "forbidden" {
		t.Errorf("s2 = %+v, want Blocked=true BlockedReason=forbidden", s2)
	}
	if s2.BytesBehind != 0 {
		t.Errorf("s2.BytesBehind = %d, want 0 (its transcript file doesn't exist)", s2.BytesBehind)
	}
}

func TestGatherSessionsIncludeExcludedLargeFiles(t *testing.T) {
	dir := t.TempDir()
	snapshotDir := filepath.Join(dir, "snapshot-state")
	if err := os.MkdirAll(snapshotDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeJSON(t, filepath.Join(snapshotDir, "s1.json"), snapshot.Pointer{
		TreeSHA:            "deadbeef",
		ExcludedLargeFiles: []string{"big.bin"},
	})

	transcriptPath := filepath.Join(dir, "s1.jsonl")
	if err := os.WriteFile(transcriptPath, []byte("hi\n"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	statusPath := filepath.Join(dir, "agent-status.json")
	writeJSON(t, statusPath, agent.StatusFile{
		UpdatedAt: time.Now(),
		StartedAt: time.Now(),
		Sessions: []agent.SessionStatus{
			{SessionID: "s1", TranscriptPath: transcriptPath},
		},
	})

	r := Gather(context.Background(), "http://local", &fakeAuthChecker{}, Paths{AgentStatusPath: statusPath, SnapshotDir: snapshotDir})
	if len(r.Sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(r.Sessions))
	}
	if got := r.Sessions[0].ExcludedLargeFiles; len(got) != 1 || got[0] != "big.bin" {
		t.Errorf("ExcludedLargeFiles = %v, want [\"big.bin\"]", got)
	}
}

func TestGatherSkippedRepos(t *testing.T) {
	dir := t.TempDir()
	skippedPath := filepath.Join(dir, "skipped-repos.json")
	writeJSON(t, skippedPath, filter.SkippedFile{
		UpdatedAt: time.Now(),
		Repos:     []string{"github.com/someone/repo-a", "github.com/someone/repo-b"},
	})

	r := Gather(context.Background(), "http://local", &fakeAuthChecker{}, Paths{SkippedPath: skippedPath})
	if len(r.SkippedRepos) != 2 {
		t.Errorf("SkippedRepos = %v, want 2 entries", r.SkippedRepos)
	}
}

func TestGatherBackendURLIsAlwaysReported(t *testing.T) {
	r := Gather(context.Background(), "https://example.supabase.co", &fakeAuthChecker{}, Paths{})
	if r.BackendURL != "https://example.supabase.co" {
		t.Errorf("BackendURL = %q, want the configured backend", r.BackendURL)
	}
}

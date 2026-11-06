package agent

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLoadStatusMissingFileReturnsZeroValue(t *testing.T) {
	got, err := LoadStatus(filepath.Join(t.TempDir(), "agent-status.json"))
	if err != nil {
		t.Fatalf("LoadStatus() error = %v", err)
	}
	if !got.UpdatedAt.IsZero() || len(got.Sessions) != 0 {
		t.Errorf("LoadStatus() for a missing file = %+v, want the zero value", got)
	}
}

func TestAgentWritesStatusFileWithVersionAndStartedAt(t *testing.T) {
	queuePath, cursorPath := agentTestPaths(t)
	statusPath := filepath.Join(t.TempDir(), "agent-status.json")
	uploader := newCountingUploader()

	clock := newFakeClock(time.Now())
	a, err := New(Config{
		QueuePath:      queuePath,
		CursorPath:     cursorPath,
		StatusPath:     statusPath,
		Version:        "1.2.3",
		Uploader:       uploader,
		UploadInterval: time.Hour,
		PollInterval:   5 * time.Millisecond,
		WorkerPoolSize: 2,
		now:            clock.Now,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	runAgent(t, a)

	waitFor(t, 1*time.Second, func() bool {
		s, err := LoadStatus(statusPath)
		return err == nil && !s.UpdatedAt.IsZero()
	})

	got, err := LoadStatus(statusPath)
	if err != nil {
		t.Fatalf("LoadStatus() error = %v", err)
	}
	if got.Version != "1.2.3" {
		t.Errorf("Version = %q, want %q", got.Version, "1.2.3")
	}
	if got.StartedAt.IsZero() {
		t.Error("StartedAt is zero, want it set when Run() started")
	}
}

func TestAgentStatusFileReflectsSessionActivityAndBlockedState(t *testing.T) {
	queuePath, cursorPath := agentTestPaths(t)
	statusPath := filepath.Join(t.TempDir(), "agent-status.json")
	uploader := newCountingUploader()
	uploader.errFor = map[string]error{"blocked-session": ErrFatal}

	a, err := New(Config{
		QueuePath:      queuePath,
		CursorPath:     cursorPath,
		StatusPath:     statusPath,
		Uploader:       uploader,
		UploadInterval: time.Hour,
		PollInterval:   5 * time.Millisecond,
		WorkerPoolSize: 2,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	runAgent(t, a)

	appendHookEvent(t, queuePath, "ok-session", "SessionEnd")
	appendHookEvent(t, queuePath, "blocked-session", "SessionEnd")

	waitFor(t, 1*time.Second, func() bool {
		s, err := LoadStatus(statusPath)
		if err != nil || len(s.Sessions) != 2 {
			return false
		}
		byID := map[string]SessionStatus{}
		for _, sess := range s.Sessions {
			byID[sess.SessionID] = sess
		}
		ok, hasOK := byID["ok-session"]
		blocked, hasBlocked := byID["blocked-session"]
		return hasOK && hasBlocked && ok.LastUploadAt != nil && !ok.Blocked && blocked.Blocked
	})

	got, err := LoadStatus(statusPath)
	if err != nil {
		t.Fatalf("LoadStatus() error = %v", err)
	}
	byID := map[string]SessionStatus{}
	for _, s := range got.Sessions {
		byID[s.SessionID] = s
	}
	if byID["blocked-session"].BlockedReason == "" {
		t.Error("blocked-session's BlockedReason is empty, want ErrFatal's message")
	}
}

func TestAgentWriteStatusIsANoOpWhenStatusPathIsEmpty(t *testing.T) {
	queuePath, cursorPath := agentTestPaths(t)
	uploader := newCountingUploader()

	a, err := New(Config{
		QueuePath:      queuePath,
		CursorPath:     cursorPath,
		Uploader:       uploader,
		UploadInterval: time.Hour,
		PollInterval:   5 * time.Millisecond,
		WorkerPoolSize: 2,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	runAgent(t, a)

	appendHookEvent(t, queuePath, "s1", "SessionEnd")
	waitFor(t, 1*time.Second, func() bool { return uploader.count("s1") >= 1 })
	// No assertion needed beyond "this doesn't panic or error" — StatusPath
	// being empty must behave exactly like it did before CO-113.
}

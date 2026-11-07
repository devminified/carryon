package pull

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/devminified/carryon/internal/api"
)

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "carryon-place-test-*.jsonl")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(f.Name()) })
	return f.Name()
}

func TestPlaceCopiesTheMainFileToTheEncodedProjectDir(t *testing.T) {
	home := withFakeHome(t)
	src := writeTempFile(t, `{"sessionId":"new-id"}`+"\n")

	result, err := Place(PlaceInput{
		WorktreePath: "/Users/bob/carryon/work/acme-api-sess1",
		SessionID:    "new-id",
		MainFile:     src,
	})
	if err != nil {
		t.Fatalf("Place() error = %v", err)
	}

	wantDir := filepath.Join(home, ".claude", "projects", "-Users-bob-carryon-work-acme-api-sess1")
	wantPath := filepath.Join(wantDir, "new-id.jsonl")
	if result.MainPath != wantPath {
		t.Errorf("MainPath = %q, want %q", result.MainPath, wantPath)
	}

	got, err := os.ReadFile(result.MainPath)
	if err != nil {
		t.Fatalf("reading placed file: %v", err)
	}
	if string(got) != `{"sessionId":"new-id"}`+"\n" {
		t.Errorf("placed file content = %q", got)
	}
}

func TestPlaceCopiesSubagentFilesIntoTheSubagentsFolder(t *testing.T) {
	home := withFakeHome(t)
	main := writeTempFile(t, `{"sessionId":"new-id"}`+"\n")
	sub := writeTempFile(t, `{"agentId":"abc123"}`+"\n")

	result, err := Place(PlaceInput{
		WorktreePath:  "/Users/bob/acme-api",
		SessionID:     "new-id",
		MainFile:      main,
		SubagentFiles: map[string]string{"abc123": sub},
	})
	if err != nil {
		t.Fatalf("Place() error = %v", err)
	}

	wantSubPath := filepath.Join(
		home, ".claude", "projects", "-Users-bob-acme-api", "new-id", "subagents", "agent-abc123.jsonl",
	)
	gotPath, ok := result.SubagentPaths["abc123"]
	if !ok {
		t.Fatalf("SubagentPaths missing abc123: %+v", result.SubagentPaths)
	}
	if gotPath != wantSubPath {
		t.Errorf("SubagentPaths[abc123] = %q, want %q", gotPath, wantSubPath)
	}

	got, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatalf("reading placed subagent file: %v", err)
	}
	if string(got) != `{"agentId":"abc123"}`+"\n" {
		t.Errorf("placed subagent file content = %q", got)
	}
}

func TestPlaceRefusesToOverwriteAnExistingMainFile(t *testing.T) {
	home := withFakeHome(t)
	projectDir := filepath.Join(home, ".claude", "projects", "-Users-bob-acme-api")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existingPath := filepath.Join(projectDir, "new-id.jsonl")
	if err := os.WriteFile(existingPath, []byte("already here\n"), 0o600); err != nil {
		t.Fatalf("writing existing file: %v", err)
	}

	src := writeTempFile(t, `{"sessionId":"new-id"}`+"\n")
	_, err := Place(PlaceInput{
		WorktreePath: "/Users/bob/acme-api",
		SessionID:    "new-id",
		MainFile:     src,
	})
	if !errors.Is(err, ErrAlreadyPlaced) {
		t.Fatalf("Place() error = %v, want ErrAlreadyPlaced", err)
	}

	got, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("reading existing file: %v", err)
	}
	if string(got) != "already here\n" {
		t.Errorf("existing file was overwritten: %q", got)
	}
}

func TestPlaceRefusesToOverwriteAnExistingSubagentFile(t *testing.T) {
	home := withFakeHome(t)
	subDir := filepath.Join(home, ".claude", "projects", "-Users-bob-acme-api", "new-id", "subagents")
	if err := os.MkdirAll(subDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existingPath := filepath.Join(subDir, "agent-abc123.jsonl")
	if err := os.WriteFile(existingPath, []byte("already here\n"), 0o600); err != nil {
		t.Fatalf("writing existing file: %v", err)
	}

	main := writeTempFile(t, `{"sessionId":"new-id"}`+"\n")
	sub := writeTempFile(t, `{"agentId":"abc123"}`+"\n")
	_, err := Place(PlaceInput{
		WorktreePath:  "/Users/bob/acme-api",
		SessionID:     "new-id",
		MainFile:      main,
		SubagentFiles: map[string]string{"abc123": sub},
	})
	if !errors.Is(err, ErrAlreadyPlaced) {
		t.Fatalf("Place() error = %v, want ErrAlreadyPlaced", err)
	}
}

func TestPlaceWithNoSubagentsLeavesSubagentPathsEmpty(t *testing.T) {
	withFakeHome(t)
	main := writeTempFile(t, `{"sessionId":"new-id"}`+"\n")

	result, err := Place(PlaceInput{
		WorktreePath: "/Users/bob/acme-api",
		SessionID:    "new-id",
		MainFile:     main,
	})
	if err != nil {
		t.Fatalf("Place() error = %v", err)
	}
	if len(result.SubagentPaths) != 0 {
		t.Errorf("SubagentPaths = %+v, want empty", result.SubagentPaths)
	}
}

func TestPlaceFailsCleanlyWhenTheSourceFileIsMissing(t *testing.T) {
	withFakeHome(t)
	_, err := Place(PlaceInput{
		WorktreePath: "/Users/bob/acme-api",
		SessionID:    "new-id",
		MainFile:     filepath.Join(t.TempDir(), "does-not-exist.jsonl"),
	})
	if err == nil {
		t.Fatal("Place() error = nil, want an error for a missing source file")
	}
}

// TestFullPullPipelinePlacesAtClaudeCodesRealPath chains every stage
// built so far (CO-205's Rebuild, CO-206's worktree path, CO-207's
// path rewrite, CO-208's session-ID rewrite, CO-209's Place) to prove
// the whole pipeline composes end to end into exactly where Claude
// Code itself would look.
func TestFullPullPipelinePlacesAtClaudeCodesRealPath(t *testing.T) {
	home := withFakeHome(t)
	server := newFakeChunkServer(t)
	oldCwd := "/Users/ali/acme-api"
	newWorktree := "/Users/bob/carryon/work/acme-api-sess1"
	data := []byte(`{"sessionId":"old-id","cwd":"` + oldCwd + `","event":"start"}` + "\n")
	chunks := registerStream(server, mainStreamID, data, 100)
	manifest := &api.Manifest{Chunks: chunks}

	destDir := t.TempDir()
	rebuildResult, err := Rebuild(context.Background(), server.client(), manifest, destDir, DefaultConfig())
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}

	pathRewritten := filepath.Join(destDir, "session.path-rewritten.jsonl")
	pr := NewPathRewriter(oldCwd, newWorktree)
	if _, err := pr.RewriteFile(rebuildResult.MainFile, pathRewritten); err != nil {
		t.Fatalf("RewriteFile() error = %v", err)
	}

	newID, err := NewSessionID()
	if err != nil {
		t.Fatalf("NewSessionID() error = %v", err)
	}
	idRewritten := filepath.Join(destDir, newID+".jsonl")
	if _, err := RewriteSessionIDFile(pathRewritten, idRewritten, newID); err != nil {
		t.Fatalf("RewriteSessionIDFile() error = %v", err)
	}

	result, err := Place(PlaceInput{
		WorktreePath: newWorktree,
		SessionID:    newID,
		MainFile:     idRewritten,
	})
	if err != nil {
		t.Fatalf("Place() error = %v", err)
	}

	wantPath := filepath.Join(home, ".claude", "projects", "-Users-bob-carryon-work-acme-api-sess1", newID+".jsonl")
	if result.MainPath != wantPath {
		t.Errorf("MainPath = %q, want %q", result.MainPath, wantPath)
	}

	got, err := os.ReadFile(result.MainPath)
	if err != nil {
		t.Fatalf("reading placed file: %v", err)
	}
	want := `{"sessionId":"` + newID + `","cwd":"` + newWorktree + `","event":"start"}` + "\n"
	if string(got) != want {
		t.Errorf("placed file content = %q, want %q", got, want)
	}
}

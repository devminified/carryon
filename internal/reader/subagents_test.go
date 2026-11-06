package reader

import (
	"os"
	"path/filepath"
	"testing"
)

// writeSubagentFixture lays out <dir>/<sessionID>.jsonl plus
// <dir>/<sessionID>/subagents/agent-<id>.jsonl for each id — the real
// on-disk shape verified against actual Claude Code session data (not
// assumed from the ticket description alone).
func writeSubagentFixture(t *testing.T, dir, sessionID string, agentIDs ...string) (transcriptPath string) {
	t.Helper()
	transcriptPath = filepath.Join(dir, sessionID+".jsonl")
	write(t, transcriptPath, `{"type":"user"}`+"\n")

	if len(agentIDs) == 0 {
		return transcriptPath
	}
	subagentsDir := filepath.Join(dir, sessionID, "subagents")
	if err := os.MkdirAll(subagentsDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, id := range agentIDs {
		write(t, filepath.Join(subagentsDir, "agent-"+id+".jsonl"), `{"type":"fork-context-ref"}`+"\n")
		// A real session also has a .meta.json sidecar per agent — present
		// to make sure Subagents only matches *.jsonl, not it.
		write(t, filepath.Join(subagentsDir, "agent-"+id+".meta.json"), `{}`)
	}
	return transcriptPath
}

func TestSubagentsNoneYet(t *testing.T) {
	dir := t.TempDir()
	transcriptPath := writeSubagentFixture(t, dir, "s1")

	got, err := Subagents(transcriptPath)
	if err != nil {
		t.Fatalf("Subagents() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Subagents() = %v, want none", got)
	}
}

func TestSubagentsDiscoversAll(t *testing.T) {
	dir := t.TempDir()
	transcriptPath := writeSubagentFixture(t, dir, "s1", "a1", "a2", "a3")

	got, err := Subagents(transcriptPath)
	if err != nil {
		t.Fatalf("Subagents() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d subagents, want 3: %+v", len(got), got)
	}
	for i, wantID := range []string{"a1", "a2", "a3"} {
		if got[i].AgentID != wantID {
			t.Errorf("got[%d].AgentID = %q, want %q (sorted order)", i, got[i].AgentID, wantID)
		}
		if filepath.Base(got[i].Path) != "agent-"+wantID+".jsonl" {
			t.Errorf("got[%d].Path = %q, want it to end in agent-%s.jsonl", i, got[i].Path, wantID)
		}
	}
}

func TestSubagentsIgnoresMetaSidecars(t *testing.T) {
	dir := t.TempDir()
	transcriptPath := writeSubagentFixture(t, dir, "s1", "a1")

	got, err := Subagents(transcriptPath)
	if err != nil {
		t.Fatalf("Subagents() error = %v", err)
	}
	for _, s := range got {
		if filepath.Ext(s.Path) != ".jsonl" {
			t.Errorf("Subagents() included a non-.jsonl file: %s", s.Path)
		}
	}
}

func TestSubagentsNewOneAppearsOnReList(t *testing.T) {
	dir := t.TempDir()
	transcriptPath := writeSubagentFixture(t, dir, "s1", "a1")

	first, err := Subagents(transcriptPath)
	if err != nil {
		t.Fatalf("Subagents() error = %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("got %d subagents, want 1", len(first))
	}

	// A new subagent spawns mid-session.
	write(t, filepath.Join(dir, "s1", "subagents", "agent-a2.jsonl"), `{}`+"\n")

	second, err := Subagents(transcriptPath)
	if err != nil {
		t.Fatalf("second Subagents() error = %v", err)
	}
	if len(second) != 2 {
		t.Fatalf("got %d subagents after a new one appeared, want 2", len(second))
	}
}

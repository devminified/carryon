package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingReturnsZeroValue(t *testing.T) {
	dir := t.TempDir()

	p, err := Load(dir, "s1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if p != (Pointer{}) {
		t.Errorf("Load() = %+v, want the zero value", p)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := Pointer{Offset: 100, NextSeq: 3, FileSize: 100, HeadHash: "abc123"}

	if err := Save(dir, "s1", want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := Load(dir, "s1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != want {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

func TestSaveCreatesStateDir(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "state") // doesn't exist yet

	if err := Save(dir, "s1", Pointer{Offset: 1}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("state dir wasn't created: %v", err)
	}
}

func TestSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, "s1", Pointer{Offset: 1}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := Save(dir, "s1", Pointer{Offset: 2}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "s1.json.tmp")); !os.IsNotExist(err) {
		t.Errorf("stray .tmp file left behind after Save()")
	}
	got, err := Load(dir, "s1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Offset != 2 {
		t.Errorf("Offset = %d, want 2 (the latest write)", got.Offset)
	}
}

func TestSubagentKeyDistinctFromSessionKey(t *testing.T) {
	sessionKey := "s1"
	subagentKey := SubagentKey("s1", "a1")

	if subagentKey == sessionKey {
		t.Fatalf("SubagentKey collides with the session's own key: %q", subagentKey)
	}

	dir := t.TempDir()
	if err := Save(dir, sessionKey, Pointer{Offset: 1}); err != nil {
		t.Fatalf("Save(session) error = %v", err)
	}
	if err := Save(dir, subagentKey, Pointer{Offset: 2}); err != nil {
		t.Fatalf("Save(subagent) error = %v", err)
	}

	sessionP, err := Load(dir, sessionKey)
	if err != nil {
		t.Fatalf("Load(session) error = %v", err)
	}
	subagentP, err := Load(dir, subagentKey)
	if err != nil {
		t.Fatalf("Load(subagent) error = %v", err)
	}
	if sessionP.Offset != 1 || subagentP.Offset != 2 {
		t.Errorf("session and subagent pointers overwrote each other: session=%+v subagent=%+v", sessionP, subagentP)
	}
}

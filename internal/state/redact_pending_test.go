package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRedactPendingMissingReturnsEmptyMap(t *testing.T) {
	dir := t.TempDir()
	got, err := LoadRedactPending(dir, "s1")
	if err != nil {
		t.Fatalf("LoadRedactPending() error = %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("LoadRedactPending() = %#v, want an empty, non-nil map", got)
	}
}

func TestSaveRedactPendingThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := map[string]string{"toolu_1": ".env", "toolu_2": "id_rsa"}

	if err := SaveRedactPending(dir, "s1", want); err != nil {
		t.Fatalf("SaveRedactPending() error = %v", err)
	}
	got, err := LoadRedactPending(dir, "s1")
	if err != nil {
		t.Fatalf("LoadRedactPending() error = %v", err)
	}
	if len(got) != len(want) || got["toolu_1"] != ".env" || got["toolu_2"] != "id_rsa" {
		t.Errorf("LoadRedactPending() = %#v, want %#v", got, want)
	}
}

func TestSaveRedactPendingCreatesStateDir(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "state")

	if err := SaveRedactPending(dir, "s1", map[string]string{}); err != nil {
		t.Fatalf("SaveRedactPending() error = %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("state dir wasn't created: %v", err)
	}
}

func TestSaveRedactPendingIsAtomicAndClearsOnEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := SaveRedactPending(dir, "s1", map[string]string{"toolu_1": ".env"}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := SaveRedactPending(dir, "s1", map[string]string{}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "s1.redact.json.tmp")); !os.IsNotExist(err) {
		t.Errorf("stray .tmp file left behind after Save()")
	}
	got, err := LoadRedactPending(dir, "s1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("LoadRedactPending() = %#v, want empty (the resolved entry should be cleared)", got)
	}
}

func TestRedactPendingKeyDistinctFromPointerKey(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, "s1", Pointer{Offset: 42}); err != nil {
		t.Fatalf("Save(pointer) error = %v", err)
	}
	if err := SaveRedactPending(dir, "s1", map[string]string{"toolu_1": ".env"}); err != nil {
		t.Fatalf("SaveRedactPending() error = %v", err)
	}

	p, err := Load(dir, "s1")
	if err != nil {
		t.Fatalf("Load(pointer) error = %v", err)
	}
	if p.Offset != 42 {
		t.Errorf("Pointer.Offset = %d, want 42 (redact-pending write shouldn't disturb it)", p.Offset)
	}
	pending, err := LoadRedactPending(dir, "s1")
	if err != nil {
		t.Fatalf("LoadRedactPending() error = %v", err)
	}
	if pending["toolu_1"] != ".env" {
		t.Errorf("pending = %#v", pending)
	}
}

package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCursorRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor.json")

	if err := saveCursor(path, cursorState{Offset: 12345}); err != nil {
		t.Fatalf("saveCursor() error = %v", err)
	}

	got, err := loadCursor(path)
	if err != nil {
		t.Fatalf("loadCursor() error = %v", err)
	}
	if got.Offset != 12345 {
		t.Errorf("loadCursor() offset = %d, want 12345", got.Offset)
	}
}

func TestLoadCursorMissingFileReturnsZeroValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")

	got, err := loadCursor(path)
	if err != nil {
		t.Fatalf("loadCursor() error = %v", err)
	}
	if got.Offset != 0 {
		t.Errorf("loadCursor() offset = %d, want 0", got.Offset)
	}
}

func TestSaveCursorIsAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor.json")
	if err := saveCursor(path, cursorState{Offset: 1}); err != nil {
		t.Fatalf("saveCursor() error = %v", err)
	}
	if err := saveCursor(path, cursorState{Offset: 2}); err != nil {
		t.Fatalf("saveCursor() error = %v", err)
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("stray .tmp file left behind after saveCursor()")
	}
	got, err := loadCursor(path)
	if err != nil {
		t.Fatalf("loadCursor() error = %v", err)
	}
	if got.Offset != 2 {
		t.Errorf("loadCursor() offset = %d, want 2 (the latest write)", got.Offset)
	}
}

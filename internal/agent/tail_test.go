package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadNewLinesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")

	lines, offset, err := readNewLines(path, 0)
	if err != nil {
		t.Fatalf("readNewLines() error = %v", err)
	}
	if lines != nil || offset != 0 {
		t.Errorf("readNewLines() = %v, %d, want nil, 0", lines, offset)
	}
}

func TestReadNewLinesHoldsBackPartialTrailingLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	write(t, path, `{"session_id":"s1"}`+"\n"+`{"session_id":"s2"}`) // no trailing newline

	lines, offset, err := readNewLines(path, 0)
	if err != nil {
		t.Fatalf("readNewLines() error = %v", err)
	}
	if len(lines) != 1 || string(lines[0]) != `{"session_id":"s1"}` {
		t.Fatalf("readNewLines() lines = %v, want exactly the first complete line", lines)
	}

	append_(t, path, "\n")
	lines, newOffset, err := readNewLines(path, offset)
	if err != nil {
		t.Fatalf("readNewLines() error = %v", err)
	}
	if len(lines) != 1 || string(lines[0]) != `{"session_id":"s2"}` {
		t.Fatalf("readNewLines() second call lines = %v, want the now-complete second line", lines)
	}
	if newOffset <= offset {
		t.Errorf("offset didn't advance: %d -> %d", offset, newOffset)
	}
}

func TestReadNewLinesMultipleCompleteLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	write(t, path, `{"session_id":"s1"}`+"\n"+`{"session_id":"s2"}`+"\n"+`{"session_id":"s3"}`+"\n")

	lines, _, err := readNewLines(path, 0)
	if err != nil {
		t.Fatalf("readNewLines() error = %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
}

func TestReadNewLinesNoNewDataReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	write(t, path, `{"session_id":"s1"}`+"\n")

	_, offset, err := readNewLines(path, 0)
	if err != nil {
		t.Fatalf("readNewLines() error = %v", err)
	}

	lines, newOffset, err := readNewLines(path, offset)
	if err != nil {
		t.Fatalf("second readNewLines() error = %v", err)
	}
	if lines != nil {
		t.Errorf("got %d lines on a second read with no new data, want 0", len(lines))
	}
	if newOffset != offset {
		t.Errorf("offset changed with no new data: %d -> %d", offset, newOffset)
	}
}

func TestReadNewLinesResetsWhenFileShrinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	write(t, path, `{"session_id":"s1"}`+"\n"+`{"session_id":"s2"}`+"\n")

	_, offset, err := readNewLines(path, 0)
	if err != nil {
		t.Fatalf("readNewLines() error = %v", err)
	}

	// Simulate the file being rewritten smaller than our offset.
	write(t, path, `{"session_id":"s3"}`+"\n")

	lines, _, err := readNewLines(path, offset)
	if err != nil {
		t.Fatalf("readNewLines() after shrink error = %v", err)
	}
	if len(lines) != 1 || string(lines[0]) != `{"session_id":"s3"}` {
		t.Fatalf("readNewLines() after shrink = %v, want a reset re-read from the start", lines)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("write %s: %v", path, err)
	}
}

func append_(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304,G302 -- test fixture
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("append %s: %v", path, err)
	}
}

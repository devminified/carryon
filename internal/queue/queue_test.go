package queue

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestAppendCreatesAndWritesLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")

	if err := Append(path, []byte(`{"session_id":"s1"}`)); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != `{"session_id":"s1"}`+"\n" {
		t.Errorf("content = %q, want a trailing newline added", data)
	}
}

func TestAppendAppendsNotTruncates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")

	if err := Append(path, []byte(`{"session_id":"s1"}`)); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := Append(path, []byte(`{"session_id":"s2"}`)); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := `{"session_id":"s1"}` + "\n" + `{"session_id":"s2"}` + "\n"
	if string(data) != want {
		t.Errorf("content = %q, want %q", data, want)
	}
}

func TestAppend200ConcurrentCallsProduceIntactLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")

	const n = 200
	done := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			done <- Append(path, []byte(`{"session_id":"s`+strconv.Itoa(i)+`"}`))
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-done; err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	f, err := os.Open(path) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()

	count := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if sc.Text() != "" {
			count++
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if count != n {
		t.Errorf("got %d intact lines, want %d (interleaved or lost writes)", count, n)
	}
}

func TestReadNewMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")

	lines, offset, err := ReadNew(path, 0)
	if err != nil {
		t.Fatalf("ReadNew() error = %v", err)
	}
	if lines != nil || offset != 0 {
		t.Errorf("ReadNew() = %v, %d, want nil, 0", lines, offset)
	}
}

func TestReadNewHoldsBackPartialTrailingLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	writeFile(t, path, `{"session_id":"s1"}`+"\n"+`{"session_id":"s2"}`) // no trailing newline

	lines, offset, err := ReadNew(path, 0)
	if err != nil {
		t.Fatalf("ReadNew() error = %v", err)
	}
	if len(lines) != 1 || string(lines[0]) != `{"session_id":"s1"}` {
		t.Fatalf("ReadNew() lines = %v, want exactly the first complete line", lines)
	}

	// Append always adds its own trailing newline, so to test "the second
	// line completes later," append its missing newline directly instead.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304,G302 -- test fixture
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString("\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
	_ = f.Close()

	lines, newOffset, err := ReadNew(path, offset)
	if err != nil {
		t.Fatalf("ReadNew() error = %v", err)
	}
	if len(lines) != 1 || string(lines[0]) != `{"session_id":"s2"}` {
		t.Fatalf("ReadNew() second call lines = %v, want the now-complete second line", lines)
	}
	if newOffset <= offset {
		t.Errorf("offset didn't advance: %d -> %d", offset, newOffset)
	}
}

func TestReadNewMultipleCompleteLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	writeFile(t, path, `{"session_id":"s1"}`+"\n"+`{"session_id":"s2"}`+"\n"+`{"session_id":"s3"}`+"\n")

	lines, _, err := ReadNew(path, 0)
	if err != nil {
		t.Fatalf("ReadNew() error = %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
}

func TestReadNewNoNewDataReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	writeFile(t, path, `{"session_id":"s1"}`+"\n")

	_, offset, err := ReadNew(path, 0)
	if err != nil {
		t.Fatalf("ReadNew() error = %v", err)
	}

	lines, newOffset, err := ReadNew(path, offset)
	if err != nil {
		t.Fatalf("second ReadNew() error = %v", err)
	}
	if lines != nil {
		t.Errorf("got %d lines on a second read with no new data, want 0", len(lines))
	}
	if newOffset != offset {
		t.Errorf("offset changed with no new data: %d -> %d", offset, newOffset)
	}
}

func TestReadNewResetsWhenFileShrinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	writeFile(t, path, `{"session_id":"s1"}`+"\n"+`{"session_id":"s2"}`+"\n")

	_, offset, err := ReadNew(path, 0)
	if err != nil {
		t.Fatalf("ReadNew() error = %v", err)
	}

	writeFile(t, path, `{"session_id":"s3"}`+"\n") // simulate the file being rewritten smaller

	lines, _, err := ReadNew(path, offset)
	if err != nil {
		t.Fatalf("ReadNew() after shrink error = %v", err)
	}
	if len(lines) != 1 || string(lines[0]) != `{"session_id":"s3"}` {
		t.Fatalf("ReadNew() after shrink = %v, want a reset re-read from the start", lines)
	}
}

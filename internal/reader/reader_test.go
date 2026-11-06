package reader

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/devminified/carryon/internal/state"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("write %s: %v", path, err)
	}
}

func appendTo(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304,G302 -- test fixture
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("append %s: %v", path, err)
	}
}

func TestReadNewMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")

	data, offset, err := ReadNew(path, 0)
	if err != nil {
		t.Fatalf("ReadNew() error = %v", err)
	}
	if data != nil || offset != 0 {
		t.Errorf("ReadNew() = %v, %d, want nil, 0", data, offset)
	}
}

func TestReadNewHoldsBackPartialTrailingLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path, `{"a":1}`) // no trailing newline — a half-written line

	data, offset, err := ReadNew(path, 0)
	if err != nil {
		t.Fatalf("ReadNew() error = %v", err)
	}
	if data != nil {
		t.Fatalf("ReadNew() returned %q for a half-written line, want nothing", data)
	}
	if offset != 0 {
		t.Errorf("offset = %d, want 0 (nothing consumed)", offset)
	}

	appendTo(t, path, "\n") // the rest of the line arrives
	data, newOffset, err := ReadNew(path, offset)
	if err != nil {
		t.Fatalf("second ReadNew() error = %v", err)
	}
	if string(data) != `{"a":1}`+"\n" {
		t.Fatalf("second ReadNew() = %q, want the now-complete line", data)
	}
	if newOffset <= offset {
		t.Errorf("offset didn't advance: %d -> %d", offset, newOffset)
	}
}

func TestReadNewReturnsRawBytesThroughLastNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path, `{"a":1}`+"\n"+`{"a":2}`+"\n"+`{"a":3}`) // last line incomplete

	data, _, err := ReadNew(path, 0)
	if err != nil {
		t.Fatalf("ReadNew() error = %v", err)
	}
	want := `{"a":1}` + "\n" + `{"a":2}` + "\n"
	if string(data) != want {
		t.Errorf("ReadNew() = %q, want %q", data, want)
	}
}

func TestReadDetectsNoRewriteOnFirstRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path, `{"a":1}`+"\n")

	_, next, rewritten, err := Read(path, state.Pointer{})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if rewritten {
		t.Error("Read() reported rewritten=true on the very first read, want false")
	}
	if next.Offset == 0 {
		t.Error("next.Offset = 0, want it to have advanced past the one line")
	}
}

func TestReadDetectsTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path, `{"a":1}`+"\n"+`{"a":2}`+"\n")

	_, prior, _, err := Read(path, state.Pointer{})
	if err != nil {
		t.Fatalf("first Read() error = %v", err)
	}

	// Simulate the file being truncated/rewritten to something shorter.
	write(t, path, `{"a":9}`+"\n")

	data, next, rewritten, err := Read(path, prior)
	if err != nil {
		t.Fatalf("second Read() error = %v", err)
	}
	if !rewritten {
		t.Error("Read() did not detect truncation, want rewritten=true")
	}
	if string(data) != `{"a":9}`+"\n" {
		t.Errorf("Read() after truncation = %q, want the new content read from the start", data)
	}
	if next.NextSeq != prior.NextSeq {
		t.Errorf("NextSeq changed on rewrite detection (%d -> %d), want Read to never touch it", prior.NextSeq, next.NextSeq)
	}
}

func TestReadDetectsRewriteSameSizeDifferentContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	original := `{"a":"aaaa"}` + "\n"
	write(t, path, original)

	_, prior, _, err := Read(path, state.Pointer{})
	if err != nil {
		t.Fatalf("first Read() error = %v", err)
	}

	// Same length, different content — a shrink check alone wouldn't catch this.
	rewrittenContent := `{"a":"bbbb"}` + "\n"
	if len(rewrittenContent) != len(original) {
		t.Fatalf("test fixture bug: lengths differ (%d vs %d)", len(rewrittenContent), len(original))
	}
	write(t, path, rewrittenContent)

	data, _, rewritten, err := Read(path, prior)
	if err != nil {
		t.Fatalf("second Read() error = %v", err)
	}
	if !rewritten {
		t.Error("Read() did not detect a same-size content change, want rewritten=true")
	}
	if string(data) != rewrittenContent {
		t.Errorf("Read() after rewrite = %q, want %q", data, rewrittenContent)
	}
}

func TestReadNoChangeIsNotARewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path, `{"a":1}`+"\n")

	_, prior, _, err := Read(path, state.Pointer{})
	if err != nil {
		t.Fatalf("first Read() error = %v", err)
	}

	data, next, rewritten, err := Read(path, prior)
	if err != nil {
		t.Fatalf("second Read() error = %v", err)
	}
	if rewritten {
		t.Error("Read() reported rewritten=true with no change to the file")
	}
	if data != nil {
		t.Errorf("Read() returned %q with nothing new, want nil", data)
	}
	if next != prior {
		t.Errorf("next = %+v, want it unchanged from prior = %+v", next, prior)
	}
}

// TestCrashBetweenUploadAndStateWriteReplaysSameRange is CO-109's
// crash-safety testing point: a caller must only persist the pointer Read
// returns *after* confirming whatever it did with the data succeeded. If
// that confirmation never happens (simulating a crash), retrying Read
// with the same (un-persisted) prior pointer must return the identical
// range again — never skip it, never return something different.
func TestCrashBetweenUploadAndStateWriteReplaysSameRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path, `{"a":1}`+"\n")

	dir := t.TempDir()
	prior, err := state.Load(dir, "s1")
	if err != nil {
		t.Fatalf("state.Load() error = %v", err)
	}

	data1, next1, _, err := Read(path, prior)
	if err != nil {
		t.Fatalf("Read() #1 error = %v", err)
	}
	// Simulate a crash here: next1 is never persisted via state.Save.

	data2, next2, _, err := Read(path, prior) // retried with the same, still-unpersisted prior
	if err != nil {
		t.Fatalf("Read() #2 (retry after \"crash\") error = %v", err)
	}

	if string(data1) != string(data2) {
		t.Errorf("retried Read() returned different data: %q vs %q, want identical", data1, data2)
	}
	if next1 != next2 {
		t.Errorf("retried Read() produced a different next pointer: %+v vs %+v, want identical", next1, next2)
	}
}

// TestWriterAndReaderConcurrently10000Lines is CO-109's concurrency
// testing point: a writer appending lines (mirroring Claude Code's own
// append-only transcript writes) races against a reader tailing the same
// file, 10,000 lines, under the race detector. Every line must appear
// exactly once, intact, in order, in the concatenated read output.
func TestWriterAndReaderConcurrently10000Lines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path, "")

	const n = 10000
	want := expectedContent(n)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			appendTo(t, path, fmt.Sprintf(`{"i":%d}`+"\n", i))
		}
	}()

	var got []byte
	var offset int64
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		deadline := time.Now().Add(30 * time.Second)
		for string(got) != want && time.Now().Before(deadline) {
			data, newOffset, err := ReadNew(path, offset)
			if err != nil {
				t.Errorf("ReadNew() error = %v", err)
				return
			}
			got = append(got, data...)
			offset = newOffset
			if len(data) == 0 {
				time.Sleep(time.Millisecond)
			}
		}
	}()

	wg.Wait()
	<-readDone

	if string(got) != want {
		t.Fatalf("got %d bytes, want %d bytes of intact, ordered content", len(got), len(want))
	}
}

func expectedContent(n int) string {
	var b []byte
	for i := 0; i < n; i++ {
		b = append(b, []byte(fmt.Sprintf(`{"i":%d}`+"\n", i))...)
	}
	return string(b)
}

func TestHeadHashStableForSameContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path, `{"a":1}`+"\n")

	h1, err := headHash(path)
	if err != nil {
		t.Fatalf("headHash() error = %v", err)
	}
	h2, err := headHash(path)
	if err != nil {
		t.Fatalf("headHash() error = %v", err)
	}
	if h1 != h2 {
		t.Errorf("headHash() not stable across calls: %q vs %q", h1, h2)
	}

	want := sha256.Sum256([]byte(`{"a":1}` + "\n"))
	if h1 != hex.EncodeToString(want[:]) {
		t.Errorf("headHash() = %q, want the sha256 of the file's content", h1)
	}
}

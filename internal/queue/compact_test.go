package queue

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCompactDropsEverythingBeforeKeepFrom(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	content := `{"session_id":"s1"}` + "\n" + `{"session_id":"s2"}` + "\n" + `{"session_id":"s3"}` + "\n"
	writeFile(t, path, content)
	keepFrom := int64(strings.Index(content, `{"session_id":"s2"}`))

	removed, err := Compact(path, keepFrom)
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if removed != keepFrom {
		t.Errorf("removed = %d, want %d", removed, keepFrom)
	}

	got, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := `{"session_id":"s2"}` + "\n" + `{"session_id":"s3"}` + "\n"
	if string(got) != want {
		t.Errorf("queue.jsonl after compact = %q, want %q", got, want)
	}
}

func TestCompactZeroKeepFromIsANoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	content := `{"session_id":"s1"}` + "\n"
	writeFile(t, path, content)

	removed, err := Compact(path, 0)
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0", removed)
	}
	got, _ := os.ReadFile(path) // #nosec G304
	if string(got) != content {
		t.Errorf("queue.jsonl was modified by a zero-keepFrom compact: %q", got)
	}
}

func TestCompactMissingFileIsSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")

	removed, err := Compact(path, 10)
	if err != nil {
		t.Fatalf("Compact() on a missing file error = %v", err)
	}
	if removed != 0 {
		t.Errorf("removed = %d, want 0", removed)
	}
}

// TestCompactNeverLosesAWriteConcurrentWithItself is the key safety
// property: a write that happens strictly *after* Compact's keepFrom
// snapshot was taken — i.e., concurrently with Compact's own internal
// steal-reset-retry loop — must never be lost, even though Compact is
// simultaneously dropping everything *before* that snapshot. (Dropping
// the pre-snapshot content isn't data loss — that's compaction's entire
// job; see Compact's doc comment for the one, pathological, documented
// exception for writes concurrent with Compact that this test doesn't
// close.)
//
// Writers use Append (not a hand-rolled os.OpenFile) specifically so this
// exercises the real, shared, platform-specific open path — this is what
// caught a genuine Windows-only bug in CI (Go's os.OpenFile never grants
// FILE_SHARE_DELETE there, which Compact's rename-based approach depends
// on) that a POSIX-only test run never would have.
func TestCompactNeverLosesAWriteConcurrentWithItself(t *testing.T) {
	const trials = 30
	for trial := 0; trial < trials; trial++ {
		path := filepath.Join(t.TempDir(), "queue.jsonl")
		writeFile(t, path, "line-before\n")
		keepFrom := int64(len("line-before\n"))

		const concurrentWriters = 6
		var wg sync.WaitGroup
		for w := 0; w < concurrentWriters; w++ {
			wg.Add(1)
			go func(writer int) {
				defer wg.Done()
				line := fmt.Sprintf(`{"session_id":"during-%d"}`, writer)
				if err := Append(path, []byte(line)); err != nil {
					t.Errorf("writer %d: Append: %v", writer, err)
				}
			}(w)
		}

		if _, err := Compact(path, keepFrom); err != nil {
			t.Fatalf("trial %d: Compact() error = %v", trial, err)
		}
		wg.Wait()

		// A straggler writer could still be mid-open/write when Compact
		// returns; give it a moment, matching the grace period Compact
		// itself uses between steals.
		time.Sleep(compactGracePeriod)

		data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
		if err != nil {
			t.Fatalf("trial %d: read final queue.jsonl: %v", trial, err)
		}
		got := string(data)

		if strings.Contains(got, "line-before") {
			t.Errorf("trial %d: \"line-before\" (before keepFrom) survived compaction, want it dropped", trial)
		}
		for w := 0; w < concurrentWriters; w++ {
			want := fmt.Sprintf(`{"session_id":"during-%d"}`, w)
			if !strings.Contains(got, want) {
				t.Errorf("trial %d: %q (written concurrently with Compact) was lost — final content: %q", trial, want, got)
			}
		}
	}
}

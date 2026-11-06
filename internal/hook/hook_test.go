package hook

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// withFakeHome points config.Dir() at a temp directory, matching
// internal/config's own test helper.
func withFakeHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}

func queuePath(home string) string {
	return filepath.Join(home, ".carryon", "queue.jsonl")
}

func readLines(t *testing.T, path string) []entry {
	t.Helper()
	f, err := os.Open(path) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	var entries []entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var e entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("line %q is not valid json: %v", sc.Text(), err)
		}
		entries = append(entries, e)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanning %s: %v", path, err)
	}
	return entries
}

func TestRunAppendsAnEntry(t *testing.T) {
	home := withFakeHome(t)

	stdin := `{"session_id":"s1","transcript_path":"/tmp/t.jsonl","cwd":"/repo","hook_event_name":"Stop"}`
	Run(strings.NewReader(stdin), false)

	entries := readLines(t, queuePath(home))
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	got := entries[0]
	if got.SessionID != "s1" || got.TranscriptPath != "/tmp/t.jsonl" || got.Cwd != "/repo" || got.Event != "Stop" {
		t.Errorf("entry = %+v, want session_id=s1 transcript_path=/tmp/t.jsonl cwd=/repo event=Stop", got)
	}
	if _, err := time.Parse(time.RFC3339Nano, got.Timestamp); err != nil {
		t.Errorf("ts = %q is not RFC3339Nano: %v", got.Timestamp, err)
	}
}

func TestRunFinalForcesSessionEndRegardlessOfStdin(t *testing.T) {
	home := withFakeHome(t)

	stdin := `{"session_id":"s1","hook_event_name":"Stop"}`
	Run(strings.NewReader(stdin), true)

	entries := readLines(t, queuePath(home))
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Event != "SessionEnd" {
		t.Errorf("event = %q, want SessionEnd", entries[0].Event)
	}
}

func TestRunMalformedJSONExitsCleanlyWithoutAppending(t *testing.T) {
	home := withFakeHome(t)

	Run(strings.NewReader(`{not json`), false)

	if _, err := os.Stat(queuePath(home)); !os.IsNotExist(err) {
		t.Errorf("queue.jsonl exists after malformed input, want no file")
	}
	errLog := filepath.Join(home, ".carryon", "hook-errors.log")
	if _, err := os.Stat(errLog); err != nil {
		t.Errorf("hook-errors.log not written: %v", err)
	}
}

func TestRunEmptyStdinExitsCleanlyWithoutAppending(t *testing.T) {
	home := withFakeHome(t)

	Run(strings.NewReader(""), false)

	if _, err := os.Stat(queuePath(home)); !os.IsNotExist(err) {
		t.Errorf("queue.jsonl exists after empty stdin, want no file")
	}
}

func TestRunMissingSessionIDExitsCleanlyWithoutAppending(t *testing.T) {
	home := withFakeHome(t)

	Run(strings.NewReader(`{"cwd":"/repo"}`), false)

	if _, err := os.Stat(queuePath(home)); !os.IsNotExist(err) {
		t.Errorf("queue.jsonl exists after input with no session_id, want no file")
	}
}

func TestRunUnwritableHomeExitsCleanlyWithoutPanicking(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root ignores permission bits")
	}
	home := withFakeHome(t)
	if err := os.Chmod(home, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o700) })

	// Must not panic; Run has no error return, so "clean exit" is simply
	// that this call completes.
	Run(strings.NewReader(`{"session_id":"s1"}`), false)
}

func TestRunTruncatesOversizedFieldsSoLinesStayUnder4KB(t *testing.T) {
	home := withFakeHome(t)

	huge := strings.Repeat("x", 10*1024)
	payload, err := json.Marshal(input{
		SessionID:      "s1",
		TranscriptPath: huge,
		Cwd:            huge,
		HookEventName:  "Stop",
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	Run(strings.NewReader(string(payload)), false)

	data, err := os.ReadFile(queuePath(home)) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read queue: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if n := len(lines[0]); n >= 4096 {
		t.Errorf("line is %d bytes, want under 4096", n)
	}
}

func TestRun200ConcurrentInvocationsProduceIntactLines(t *testing.T) {
	home := withFakeHome(t)

	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			stdin := `{"session_id":"s` + strconv.Itoa(i) + `","cwd":"/repo","hook_event_name":"Stop"}`
			Run(strings.NewReader(stdin), false)
		}(i)
	}
	wg.Wait()

	entries := readLines(t, queuePath(home))
	if len(entries) != n {
		t.Fatalf("got %d intact lines, want %d (interleaved or lost writes)", len(entries), n)
	}

	seen := make(map[string]bool, n)
	for _, e := range entries {
		seen[e.SessionID] = true
	}
	if len(seen) != n {
		t.Errorf("got %d distinct session_ids, want %d", len(seen), n)
	}
}

// TestRunP99LatencyUnder50ms covers CO-105's benchmark testing point: the
// hook path must hit p99 under 50ms (NFR-01). This exercises Run()
// in-process (parse + one append syscall) — the same cost the installed
// `carryon hook` subcommand pays per invocation, minus process startup.
func TestRunP99LatencyUnder50ms(t *testing.T) {
	withFakeHome(t)

	const n = 300
	durations := make([]time.Duration, n)
	for i := 0; i < n; i++ {
		stdin := `{"session_id":"s` + strconv.Itoa(i) + `","transcript_path":"/tmp/t.jsonl","cwd":"/repo","hook_event_name":"Stop"}`
		start := time.Now()
		Run(strings.NewReader(stdin), false)
		durations[i] = time.Since(start)
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p99 := durations[int(float64(n)*0.99)]
	if p99 > 50*time.Millisecond {
		t.Errorf("p99 latency = %v, want under 50ms", p99)
	}
}

func TestTruncateRespectsUTF8Boundaries(t *testing.T) {
	s := strings.Repeat("é", 100) // 2 bytes per rune
	got := truncate(s, 101)       // odd cap forces a boundary decision

	if len(got) > 101 {
		t.Fatalf("truncate() returned %d bytes, want <= 101", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("truncate() produced invalid UTF-8: %q", got)
	}
}

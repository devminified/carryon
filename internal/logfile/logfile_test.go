package logfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestWriter(t *testing.T, dir string, start time.Time) (*Writer, *fakeClock) {
	t.Helper()
	clock := &fakeClock{now: start}
	w, err := New(dir, "agent")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	w.nowFunc = clock.Now
	w.day = clock.Now().Format(dayFormat) // re-derive using the fake clock, not the real one New() used
	t.Cleanup(func() { _ = w.Close() })
	return w, clock
}

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

func TestWriterAppendsToTheCurrentFile(t *testing.T) {
	dir := t.TempDir()
	w, _ := newTestWriter(t, dir, time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))

	if _, err := w.Write([]byte("line one\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := w.Write([]byte("line two\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "agent.log"))
	if err != nil {
		t.Fatalf("reading agent.log: %v", err)
	}
	if string(data) != "line one\nline two\n" {
		t.Errorf("agent.log = %q, want both lines appended", data)
	}
}

func TestWriterRotatesOnANewCalendarDay(t *testing.T) {
	dir := t.TempDir()
	day1 := time.Date(2026, 1, 1, 23, 59, 0, 0, time.UTC)
	w, clock := newTestWriter(t, dir, day1)

	if _, err := w.Write([]byte("day one\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	clock.now = time.Date(2026, 1, 2, 0, 1, 0, 0, time.UTC)
	if _, err := w.Write([]byte("day two\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	rotated, err := os.ReadFile(filepath.Join(dir, "agent-2026-01-01.log"))
	if err != nil {
		t.Fatalf("reading rotated file: %v", err)
	}
	if string(rotated) != "day one\n" {
		t.Errorf("rotated file = %q, want %q", rotated, "day one\n")
	}

	current, err := os.ReadFile(filepath.Join(dir, "agent.log"))
	if err != nil {
		t.Fatalf("reading current file: %v", err)
	}
	if string(current) != "day two\n" {
		t.Errorf("current file = %q, want %q (fresh, not the old content)", current, "day two\n")
	}
}

func TestWriterKeepsOnlyTheMostRecentSevenRotatedFiles(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	w, clock := newTestWriter(t, dir, start)

	// 10 days of writing, one rotation per day boundary crossed — 9
	// rotated files total by the end (the 10th day's file is still
	// current, never rotated).
	for i := 0; i < 10; i++ {
		if _, err := w.Write([]byte("entry\n")); err != nil {
			t.Fatalf("Write() #%d error = %v", i, err)
		}
		clock.now = clock.now.AddDate(0, 0, 1)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	var rotatedCount int
	for _, e := range entries {
		if e.Name() != "agent.log" {
			rotatedCount++
		}
	}
	if rotatedCount != keepDays {
		t.Errorf("rotated file count = %d, want %d (keepDays)", rotatedCount, keepDays)
	}

	// The oldest rotated file (day 1) must be gone; the most recent
	// (day 9, the last rotation before the loop ended) must remain.
	if _, err := os.Stat(filepath.Join(dir, "agent-2026-01-01.log")); !os.IsNotExist(err) {
		t.Error("oldest rotated file still exists, want it pruned")
	}
	if _, err := os.Stat(filepath.Join(dir, "agent-2026-01-09.log")); err != nil {
		t.Errorf("most recent rotated file missing: %v", err)
	}
}

func TestNewReopensAnExistingFileWithoutTruncating(t *testing.T) {
	dir := t.TempDir()
	w1, _ := newTestWriter(t, dir, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if _, err := w1.Write([]byte("before restart\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	_ = w1.Close()

	w2, err := New(dir, "agent")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = w2.Close() }()
	if _, err := w2.Write([]byte("after restart\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "agent.log"))
	if err != nil {
		t.Fatalf("reading agent.log: %v", err)
	}
	if string(data) != "before restart\nafter restart\n" {
		t.Errorf("agent.log = %q, want both lines (no truncation on reopen)", data)
	}
}

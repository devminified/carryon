// Package logfile implements CO-113's daily-rotating log file: the
// active file is always <dir>/<name>.log, so a tail (carryon logs -f)
// always points at one stable path; a Write on a later calendar day than
// the one the current file was opened for rotates it to
// <dir>/<name>-YYYY-MM-DD.log first, then prunes rotated files beyond
// the last 7.
package logfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// keepDays is CO-113's "daily rotation (7 kept)".
const keepDays = 7

const dayFormat = "2006-01-02"

// Writer is an io.Writer safe for concurrent use (slog handlers call
// Write from whatever goroutine logged).
type Writer struct {
	mu      sync.Mutex
	dir     string
	name    string
	f       *os.File
	day     string // YYYY-MM-DD the current file was opened/rotated for
	nowFunc func() time.Time
}

// New opens (creating if needed) <dir>/<name>.log for appending.
func New(dir, name string) (*Writer, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}
	w := &Writer{dir: dir, name: name, nowFunc: time.Now}
	if err := w.openCurrent(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) path() string {
	return filepath.Join(w.dir, w.name+".log")
}

func (w *Writer) openCurrent() error {
	f, err := os.OpenFile(w.path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304,G302 -- path is our own computed ~/.carryon/logs/<name>.log, or a test-controlled path
	if err != nil {
		return fmt.Errorf("opening %s: %w", w.path(), err)
	}
	w.f = f
	w.day = w.nowFunc().Format(dayFormat)
	return nil
}

// Write implements io.Writer, rotating first if today's date differs
// from the day the current file was opened/last rotated for.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	today := w.nowFunc().Format(dayFormat)
	if today != w.day {
		if err := w.rotate(today); err != nil {
			return 0, err
		}
	}
	return w.f.Write(p)
}

func (w *Writer) rotate(today string) error {
	if err := w.f.Close(); err != nil {
		return fmt.Errorf("closing current log before rotating: %w", err)
	}

	rotated := filepath.Join(w.dir, fmt.Sprintf("%s-%s.log", w.name, w.day))
	if err := os.Rename(w.path(), rotated); err != nil {
		return fmt.Errorf("rotating log to %s: %w", rotated, err)
	}

	if err := w.openCurrent(); err != nil {
		return err
	}
	w.prune()
	return nil
}

// prune removes rotated files beyond the most recent keepDays — best
// effort, same as the rest of this project's housekeeping (compaction,
// cursor persistence): a failure here never blocks logging itself.
func (w *Writer) prune() {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}

	prefix := w.name + "-"
	var rotatedFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".log") {
			rotatedFiles = append(rotatedFiles, e.Name())
		}
	}
	sort.Strings(rotatedFiles) // date-named (name-YYYY-MM-DD.log), so lexical order is chronological order

	if len(rotatedFiles) <= keepDays {
		return
	}
	for _, name := range rotatedFiles[:len(rotatedFiles)-keepDays] {
		_ = os.Remove(filepath.Join(w.dir, name))
	}
}

// Close closes the current underlying file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}

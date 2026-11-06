package agent

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

// queueEvent is one line of ~/.carryon/queue.jsonl, written by
// internal/hook.
type queueEvent struct {
	SessionID string `json:"session_id"`
	Event     string `json:"event"`
}

// readNewLines opens path, reads from offset to EOF, and returns every
// complete line plus the new offset — a partial trailing line (internal/hook
// hasn't finished its single write yet, or we read mid-write) is held back.
// A missing file is treated as "nothing new yet," not an error.
func readNewLines(path string, offset int64) (lines [][]byte, newOffset int64, err error) {
	f, err := os.Open(path) // #nosec G304 -- path is our own computed ~/.carryon/queue.jsonl, or a test-controlled path
	if errors.Is(err, os.ErrNotExist) {
		return nil, offset, nil
	}
	if err != nil {
		return nil, offset, fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, offset, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Size() < offset {
		// The file is smaller than our offset — it was rewritten out from
		// under us. internal/hook only ever appends, so this shouldn't
		// happen to queue.jsonl in normal operation, but don't trust that
		// blindly: reset and re-read from the start rather than erroring.
		offset = 0
	}
	if info.Size() == offset {
		return nil, offset, nil
	}

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, fmt.Errorf("seeking %s: %w", path, err)
	}
	buf := make([]byte, info.Size()-offset)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, offset, fmt.Errorf("reading %s: %w", path, err)
	}
	buf = buf[:n]

	lastNewline := bytes.LastIndexByte(buf, '\n')
	if lastNewline < 0 {
		return nil, offset, nil // no complete line yet
	}

	for _, line := range bytes.Split(buf[:lastNewline], []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		lines = append(lines, line)
	}
	return lines, offset + int64(lastNewline) + 1, nil
}

// Package hook implements the Claude Code hook event handler (CO-105): it
// reads one JSON event from stdin and appends a single line to the local
// queue file, ~/.carryon/queue.jsonl.
//
// This path is latency-critical and must never block the developer's
// Claude Code session (I1, NFR-01, go-conventions.md): no network calls,
// no git, no locks beyond the append itself. Every error is logged to
// ~/.carryon/hook-errors.log and swallowed — Run never surfaces an error
// the caller should act on, so the command it backs always exits 0.
package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/devminified/carryon/internal/config"
)

// maxStdinBytes caps how much of stdin we read. Claude Code's hook
// payloads are a few hundred bytes; this is generous headroom against a
// runaway payload, not an expected size.
const maxStdinBytes = 64 * 1024

// Field caps keep an appended line comfortably under 4KB (the testing
// point's atomicity budget — see appendLine) even under JSON-escaping
// worst case (every byte needing a 2-byte escape), regardless of how long
// an attacker- or environment-controlled cwd/transcript_path is.
const (
	maxSessionIDBytes = 128
	maxPathBytes      = 512
	maxEventBytes     = 64
)

// hookErrorLogMaxBytes caps ~/.carryon/hook-errors.log so a laptop that's
// been erroring for weeks doesn't slowly fill the disk. This log is
// diagnostic-only (not queue data — see I2), so truncating it on overflow
// is fine.
const hookErrorLogMaxBytes = 1 << 20 // 1 MB

// input is the subset of Claude Code's hook stdin payload this command
// reads. Unknown fields are ignored.
type input struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
}

// entry is one line appended to queue.jsonl.
type entry struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	Event          string `json:"event"`
	Timestamp      string `json:"ts"`
}

// Run reads one hook event from r and appends it to the local queue. final
// is true when invoked as `carryon hook --final` (wired to Claude Code's
// SessionEnd hook, per CO-106); it forces the recorded event to
// "SessionEnd" regardless of what stdin says, so the agent (CO-107) can
// detect session end reliably without depending on hook_event_name's exact
// spelling matching across Claude Code versions.
func Run(r io.Reader, final bool) {
	in, err := parse(r)
	if err != nil {
		logError(fmt.Errorf("parsing hook input: %w", err))
		return
	}
	if in.SessionID == "" {
		logError(fmt.Errorf("hook input missing session_id"))
		return
	}

	event := in.HookEventName
	if final {
		event = "SessionEnd"
	}

	e := entry{
		SessionID:      truncate(in.SessionID, maxSessionIDBytes),
		TranscriptPath: truncate(in.TranscriptPath, maxPathBytes),
		Cwd:            truncate(in.Cwd, maxPathBytes),
		Event:          truncate(event, maxEventBytes),
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
	}

	if err := appendLine(e); err != nil {
		logError(fmt.Errorf("appending to queue: %w", err))
	}
}

func parse(r io.Reader) (input, error) {
	var in input
	data, err := io.ReadAll(io.LimitReader(r, maxStdinBytes))
	if err != nil {
		return in, fmt.Errorf("reading stdin: %w", err)
	}
	if len(data) == 0 {
		return in, fmt.Errorf("empty stdin")
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return in, fmt.Errorf("decoding json: %w", err)
	}
	return in, nil
}

// appendLine writes one JSON line to ~/.carryon/queue.jsonl with a single
// O_APPEND write call. A single write under PIPE_BUF (4096 bytes on
// Linux; Windows' FILE_APPEND_DATA gives the equivalent guarantee for one
// WriteFile call) is atomic across concurrent writers with no extra
// locking — this, plus the field caps above keeping every line under that
// budget, is what lets concurrent `carryon hook` invocations never
// interleave (CO-105's testing point).
func appendLine(e entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshaling queue entry: %w", err)
	}
	line = append(line, '\n')

	dir, err := config.Dir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "queue.jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- path is our own computed ~/.carryon/queue.jsonl
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func logError(err error) {
	dir, dirErr := config.Dir()
	if dirErr != nil {
		return // nowhere to log to; the hook path must not block or panic
	}
	path := filepath.Join(dir, "hook-errors.log")

	rotateIfOversize(path)

	f, openErr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- path is our own computed ~/.carryon/hook-errors.log
	if openErr != nil {
		return
	}
	defer func() { _ = f.Close() }()

	line := fmt.Sprintf("%s %v\n", time.Now().UTC().Format(time.RFC3339), err)
	_, _ = f.WriteString(line)
}

// rotateIfOversize truncates the error log once it crosses
// hookErrorLogMaxBytes.
func rotateIfOversize(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < hookErrorLogMaxBytes {
		return
	}
	_ = os.Truncate(path, 0)
}

// truncate shortens s to at most max bytes, cutting on a UTF-8 rune
// boundary so it never produces invalid UTF-8.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	b := []byte(s)[:max]
	for len(b) > 0 {
		r, size := utf8.DecodeLastRune(b)
		if r != utf8.RuneError || size != 1 {
			break
		}
		b = b[:len(b)-1]
	}
	return string(b)
}

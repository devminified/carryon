// Package queue implements the shared primitives for Carryon's local
// event queue, ~/.carryon/queue.jsonl: appending a line (internal/hook,
// CO-105), tailing new lines and compacting already-processed ones
// (internal/agent, CO-107).
//
// All three live in one package specifically so the writer and the
// reader/compactor agree on the same Windows file-sharing semantics —
// see open_windows.go's doc comment for why that's load-bearing, not
// cosmetic: without it, Compact's rename-based approach fails outright on
// Windows the instant a writer has the file open.
package queue

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// Append writes line (without a trailing newline — Append adds it) with a
// single write call, after opening with platform-appropriate append
// semantics (see open_unix.go / open_windows.go). A single append-mode
// write under PIPE_BUF (4096 bytes on Linux; the Windows FILE_APPEND_DATA
// equivalent for one WriteFile call) is atomic across concurrent writers
// with no extra locking — callers that need that guarantee (internal/hook)
// must keep each line under that budget themselves.
func Append(path string, line []byte) error {
	f, err := openAppend(path)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 0, len(line)+1)
	buf = append(buf, line...)
	buf = append(buf, '\n')

	if _, err := f.Write(buf); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// ReadNew opens path, reads from offset to EOF, and returns every
// complete line plus the new offset — a partial trailing line (a writer
// hasn't finished its single write yet, or we read mid-write) is held
// back. A missing file is treated as "nothing new yet," not an error.
func ReadNew(path string, offset int64) (lines [][]byte, newOffset int64, err error) {
	f, err := openRead(path)
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
		// under us. Append only ever appends, so this shouldn't happen in
		// normal operation, but don't trust that blindly: reset and
		// re-read from the start rather than erroring.
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

// compactGracePeriod is a brief pause used in two places: between
// successive steals in Compact's retry loop (so writers that open and
// write in quick succession after a steal are more likely to be captured
// in one iteration, instead of bouncing the loop), and inside
// stealAndReset itself, before it discards a renamed-away file, to catch
// a writer whose write landed just after the rename — see stealAndReset's
// doc comment for why that specific case needs its own recheck, and
// Compact's doc comment for the narrower gap neither fully closes.
const compactGracePeriod = 50 * time.Millisecond

// compactionMaxAttempts bounds Compact's retry loop. Compaction is
// disk-hygiene, not a correctness requirement, so giving up for this round
// beats forcing convergence against a file being written unusually fast.
const compactionMaxAttempts = 10

// Compact rewrites path to drop everything before offset keepFrom.
//
// Append's writers must never take a lock (I1, go-conventions.md) —
// Compact can't coordinate with them via one either. Instead it uses a
// rename-steal loop: renaming path atomically "steals" every byte written
// to it so far (any writer's call to Append made *after* the rename
// starts a fresh file there, per rename semantics) and the loop repeats
// until a steal comes back empty (quiescent), then publishes the
// compacted result.
//
// Known residual edge case: a writer whose open call (binding its write
// to the current inode/file) lands an instant before a steal's rename,
// and is then preempted by the OS scheduler before it issues its one
// write, can still lose that write — the rename already moved its target
// out from under it. stealAndReset re-checks the renamed-away file once
// more before discarding it, which closes the common case (a writer
// invocation is a single open+write+close completing in low milliseconds
// — see internal/hook's own benchmark); it doesn't make the gap
// mathematically impossible, since no bound on OS scheduling delay can be
// proven without a lock Append also takes, which I1 rules out. The cost
// if it does happen is one delayed dirty-session notification, caught by
// that session's next event — not a lost transcript byte (queue.jsonl
// only ever carries "check session X", never session content). A
// deliberate tradeoff, not an oversight.
//
// Returns the number of bytes removed from the front, so the caller
// rebases its own in-memory offsets by subtracting it. Returns 0 (with
// path left exactly as it was) on error or on failing to reach
// quiescence within compactionMaxAttempts.
func Compact(path string, keepFrom int64) (removed int64, err error) {
	if keepFrom <= 0 {
		return 0, nil
	}

	kept, err := stealAndReset(path)
	if err != nil {
		return 0, err
	}
	if int64(len(kept)) < keepFrom {
		// Shouldn't happen — keepFrom came from data already read from
		// this file — but never compact on an inconsistency.
		return 0, restoreEverything(path, kept)
	}
	kept = kept[keepFrom:]

	for attempt := 0; attempt < compactionMaxAttempts; attempt++ {
		time.Sleep(compactGracePeriod)
		more, stealErr := stealAndReset(path)
		if stealErr != nil {
			return 0, restoreEverything(path, append(kept, more...))
		}
		if len(more) == 0 {
			if err := republish(path, kept); err != nil {
				return 0, err
			}
			return keepFrom, nil
		}
		kept = append(kept, more...)
	}

	// Failed to reach quiescence within compactionMaxAttempts.
	// restoreEverything (not a bare republish(path, kept)) matters here:
	// without it, whatever a writer created at path since our very last
	// steal would be silently clobbered by this bailout's own rename — a
	// writer isn't racing Compact's retry loop, it's racing Compact's
	// *final* write, no matter how many attempts came before.
	return 0, restoreEverything(path, kept)
}

// restoreEverything republishes data plus whatever is currently sitting at
// path (anything written since the caller's last steal), so that bailing
// out of Compact never drops data — "no compaction happened this round"
// is the only acceptable outcome on a bailout path, never data loss.
func restoreEverything(path string, data []byte) error {
	current, err := stealAndReset(path)
	if err != nil {
		// Can't even check what's currently there — republishing data
		// alone is the best we can do; see Compact's doc comment for why
		// this specific residual gap is an accepted tradeoff.
		return republish(path, data)
	}
	return republish(path, append(data, current...))
}

// stealAndReset atomically renames path out of the way — so the next
// writer that calls Append starts a fresh file — and returns everything
// that had been written to it. A missing file steals as empty, not an
// error.
//
// A writer whose open() landed on this same inode an instant before the
// rename, but hadn't issued its write() yet, is still writing to what's
// now tmp, not path — a plain immediate read-then-remove of tmp would
// lose that write. stealAndReset re-checks tmp once more after a short
// grace period before removing it, which is what actually keeps this
// practically safe: a writer invocation is a single open+write+close
// completing in low milliseconds (see internal/hook's own benchmark), so
// this window is normally enough. It doesn't make the gap mathematically
// impossible — an arbitrarily long OS scheduling delay between a writer's
// open() and write() still could, in principle, land after this recheck
// too — only a lock on both sides would close that fully, which I1 rules
// out on the writer's side. The cost if it ever happens is one delayed
// dirty-session notification (caught by that session's next event), not a
// lost transcript byte — queue.jsonl only ever carries "check session X."
func stealAndReset(path string) ([]byte, error) {
	tmp := path + ".compact-steal"
	if err := os.Rename(path, tmp); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("renaming %s to %s: %w", path, tmp, err)
	}
	data, err := os.ReadFile(tmp) // #nosec G304 -- tmp is path (caller-controlled) with a fixed suffix appended
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", tmp, err)
	}

	time.Sleep(compactGracePeriod)
	if more, rereadErr := os.ReadFile(tmp); rereadErr == nil && len(more) > len(data) { // #nosec G304 -- tmp is path (caller-controlled) with a fixed suffix appended
		data = more
	}

	if err := os.Remove(tmp); err != nil {
		return nil, fmt.Errorf("removing %s: %w", tmp, err)
	}
	return data, nil
}

// republish atomically (re)creates path with exactly data — used both to
// publish a successfully compacted result and to restore the original
// content when Compact bails out, so a failed attempt never leaves path
// missing or truncated.
func republish(path string, data []byte) error {
	tmp := path + ".compact-tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil { // #nosec G304,G306 -- path is caller-controlled, fixed suffix appended
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming %s to %s: %w", tmp, path, err)
	}
	return nil
}

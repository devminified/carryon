package agent

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// compactGracePeriod is a brief pause between successive steals (see
// compact's doc comment) so hooks that open and write queue.jsonl in quick
// succession after a steal are more likely to be captured in one
// iteration, instead of bouncing the loop. It does not — and cannot, with
// no lock on either side — protect against a hook that opened the file an
// instant before a steal and is then descheduled before completing its
// write; see compact's doc comment for why that residual gap is an
// accepted, low-probability, low-severity tradeoff.
const compactGracePeriod = 50 * time.Millisecond

// compactionMaxAttempts bounds compact's retry loop. Compaction is
// disk-hygiene, not a correctness requirement, so giving up for this round
// (there's another attempt at the next compaction interval) beats forcing
// convergence against a queue.jsonl that's being written unusually fast.
const compactionMaxAttempts = 10

// compact rewrites queuePath to drop everything before offset keepFrom.
//
// internal/hook's append path must never take a lock (I1, go-conventions.md)
// — compaction can't coordinate with it via one either. Instead it uses a
// rename-steal loop: renaming queuePath atomically "steals" every byte
// written to it so far (any in-flight os.OpenFile(path, O_APPEND) call made
// *after* the rename creates a fresh file there, per POSIX rename
// semantics) and the loop repeats until a steal comes back empty
// (quiescent), then publishes the compacted result.
//
// Known residual edge case: a hook process whose os.OpenFile call (binding
// its write to the current inode) lands an instant before a steal's
// rename, and is then preempted by the OS scheduler before it issues its
// one write, can still lose that write — the rename already moved its
// target inode out from under it, and no later steal of the (now
// different) file at queuePath can ever observe it. Closing this fully
// would need a lock internal/hook also takes, which I1 rules out. In
// practice a hook invocation is a single open+write+close completing in
// low milliseconds (see internal/hook's own benchmark), leaving very
// little room for this; the cost if it does happen is one delayed
// dirty-session notification, caught by that session's next event — not a
// lost transcript byte (queue.jsonl only ever carries "check session X",
// never session content; see internal/reader, CO-109). A deliberate
// tradeoff, not an oversight.
//
// Returns the number of bytes removed from the front, so the caller
// rebases its own in-memory offsets by subtracting it. Returns 0 (with
// queuePath left exactly as it was) on error or on failing to reach
// quiescence within compactionMaxAttempts.
func compact(queuePath string, keepFrom int64) (removed int64, err error) {
	if keepFrom <= 0 {
		return 0, nil
	}

	kept, err := stealAndReset(queuePath)
	if err != nil {
		return 0, err
	}
	if int64(len(kept)) < keepFrom {
		// Shouldn't happen — keepFrom came from data already read from
		// this file — but never compact on an inconsistency.
		return 0, restoreEverything(queuePath, kept)
	}
	kept = kept[keepFrom:]

	for attempt := 0; attempt < compactionMaxAttempts; attempt++ {
		time.Sleep(compactGracePeriod)
		more, stealErr := stealAndReset(queuePath)
		if stealErr != nil {
			return 0, restoreEverything(queuePath, append(kept, more...))
		}
		if len(more) == 0 {
			if err := republish(queuePath, kept); err != nil {
				return 0, err
			}
			return keepFrom, nil
		}
		kept = append(kept, more...)
	}

	// Failed to reach quiescence within compactionMaxAttempts.
	// restoreEverything (not a bare republish(queuePath, kept)) matters
	// here: without it, whatever a writer created at queuePath since our
	// very last steal would be silently clobbered by this bailout's own
	// rename — a writer isn't racing compact's retry loop, it's racing
	// compact's *final* write, no matter how many attempts came before.
	return 0, restoreEverything(queuePath, kept)
}

// restoreEverything republishes data plus whatever is currently sitting at
// path (anything written since the caller's last steal), so that bailing
// out of compact never drops data — "no compaction happened this round"
// is the only acceptable outcome on a bailout path, never data loss.
func restoreEverything(path string, data []byte) error {
	current, err := stealAndReset(path)
	if err != nil {
		// Can't even check what's currently there — republishing data
		// alone is the best we can do; see compact's doc comment for why
		// this specific residual gap is an accepted tradeoff.
		return republish(path, data)
	}
	return republish(path, append(data, current...))
}

// stealAndReset atomically renames path out of the way — so the next
// writer that opens path starts a fresh file — and returns everything
// that had been written to it. A missing file steals as empty, not an
// error.
func stealAndReset(path string) ([]byte, error) {
	tmp := path + ".compact-steal"
	if err := os.Rename(path, tmp); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("renaming %s to %s: %w", path, tmp, err)
	}
	data, err := os.ReadFile(tmp) // #nosec G304 -- tmp is path (caller-controlled, our own computed ~/.carryon/queue.jsonl or a test path) with a fixed suffix appended
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", tmp, err)
	}
	if err := os.Remove(tmp); err != nil {
		return nil, fmt.Errorf("removing %s: %w", tmp, err)
	}
	return data, nil
}

// republish atomically (re)creates path with exactly data — used both to
// publish a successfully compacted result and to restore the original
// content when compact bails out, so a failed attempt never leaves
// queuePath missing or truncated.
func republish(path string, data []byte) error {
	tmp := path + ".compact-tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil { // #nosec G304,G306,G703 -- path is caller-controlled (our own computed ~/.carryon/queue.jsonl or a test path), fixed suffix appended
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming %s to %s: %w", tmp, path, err)
	}
	return nil
}

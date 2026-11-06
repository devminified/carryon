// Package agent implements CO-107: the capture agent's scheduling engine.
// It tails ~/.carryon/queue.jsonl (written by internal/hook), debounces
// per-session upload dispatch (at most once per UploadInterval, plus
// immediately on SessionEnd), bounds concurrency with a worker pool, and
// compacts the queue file as it catches up.
//
// This package deliberately knows nothing about what an "upload" actually
// does — that's the Uploader interface, implemented for real once CO-108
// (project filter), CO-109 (incremental reader) and CO-110 (chunk upload
// pipeline) land. Until then, cmd/carryon wires in a placeholder.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Uploader uploads whatever's pending for one session. Upload must
// respect ctx cancellation (go-conventions.md) and must be safe to call
// again for the same session if a previous call failed or raced with a
// newer event — retries are safe, writes are idempotent (I3).
type Uploader interface {
	Upload(ctx context.Context, sessionID string) error
}

// Config configures an Agent. QueuePath, CursorPath and Uploader are
// required; every other field defaults as documented below if left zero,
// so tests can override just what they need.
type Config struct {
	// QueuePath is internal/hook's append target (~/.carryon/queue.jsonl
	// in production).
	QueuePath string
	// CursorPath persists the tailing watermark
	// (~/.carryon/agent-cursor.json in production).
	CursorPath string
	// Uploader is called at most once per session per UploadInterval,
	// plus immediately on that session's SessionEnd event.
	Uploader Uploader
	// UploadInterval is the per-session debounce window. Default 60s.
	UploadInterval time.Duration
	// PollInterval is how often the agent checks queue.jsonl for new
	// lines and for sessions whose debounce window has elapsed. Default
	// 1s — comfortably under the 5-second SessionEnd budget.
	PollInterval time.Duration
	// CompactInterval is how often the agent attempts to compact
	// queue.jsonl. Default 10 minutes.
	CompactInterval time.Duration
	// WorkerPoolSize bounds concurrent uploads. Default 2.
	WorkerPoolSize int

	// now is overridable so tests can control debounce timing
	// deterministically without real sleeps.
	now func() time.Time
}

func (c *Config) applyDefaults() {
	if c.UploadInterval <= 0 {
		c.UploadInterval = 60 * time.Second
	}
	if c.PollInterval <= 0 {
		c.PollInterval = time.Second
	}
	if c.CompactInterval <= 0 {
		c.CompactInterval = 10 * time.Minute
	}
	if c.WorkerPoolSize <= 0 {
		c.WorkerPoolSize = 2
	}
	if c.now == nil {
		c.now = time.Now
	}
}

// sessionSchedule is the per-session debounce/watermark state.
//
// lastSeenPos and finalAtPos are approximate on purpose: both are set to
// the queue.jsonl offset *after the whole batch* readNew() just processed,
// not this specific line's own offset. That's a deliberate simplification
// — it makes the watermark (confirmedThrough) converge slightly more
// conservatively than the exact minimum would, never unsafely.
type sessionSchedule struct {
	lastSeenPos      int64     // queue.jsonl offset as of the most recent event seen for this session
	confirmedThrough int64     // offset through which this session's events are known covered by a completed upload
	finalAtPos       int64     // lastSeenPos when a SessionEnd was observed, 0 if none outstanding
	final            bool      // a SessionEnd has been seen and not yet confirmed covered
	uploading        bool      // an upload is currently in flight for this session
	dueAt            time.Time // zero means "no upload currently scheduled"
}

type uploadResult struct {
	sessionID         string
	dispatchedThrough int64
	err               error
}

// Agent is the capture agent's scheduling loop. Not safe for concurrent
// use — Run owns all of its state for the duration of one call.
type Agent struct {
	cfg      Config
	readPos  int64
	sessions map[string]*sessionSchedule
	sem      chan struct{}
	results  chan uploadResult
}

// New validates cfg and returns an Agent ready for Run.
func New(cfg Config) (*Agent, error) {
	if cfg.QueuePath == "" {
		return nil, fmt.Errorf("agent: QueuePath is required")
	}
	if cfg.CursorPath == "" {
		return nil, fmt.Errorf("agent: CursorPath is required")
	}
	if cfg.Uploader == nil {
		return nil, fmt.Errorf("agent: Uploader is required")
	}
	cfg.applyDefaults()
	return &Agent{cfg: cfg}, nil
}

// Run tails the queue, dispatches debounced uploads through a bounded
// worker pool, and compacts the queue file, until ctx is cancelled. It
// then waits for any in-flight uploads to finish (they must respect ctx
// themselves) before returning nil.
//
// On restart after a crash, Run resumes from the last persisted cursor.
// If a crash happened between a successful compaction and persisting the
// rebased cursor, the stale (larger) cursor offset is simply past the
// now-smaller file's end or otherwise inconsistent; readNewLines resets to
// offset 0 in that case, which just re-marks some already-caught-up
// sessions dirty again — a redundant, harmless re-dispatch (I3), not lost
// data.
func (a *Agent) Run(ctx context.Context) error {
	lowerPriority()

	cursor, err := loadCursor(a.cfg.CursorPath)
	if err != nil {
		return err
	}
	a.readPos = cursor.Offset
	a.sessions = map[string]*sessionSchedule{}
	a.sem = make(chan struct{}, a.cfg.WorkerPoolSize)
	a.results = make(chan uploadResult)

	var wg sync.WaitGroup

	pollTicker := time.NewTicker(a.cfg.PollInterval)
	defer pollTicker.Stop()
	compactTicker := time.NewTicker(a.cfg.CompactInterval)
	defer compactTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil

		case <-pollTicker.C:
			_ = a.readNew() // best-effort; readPos only advances on success, so nothing is lost on a transient error
			a.dispatchDue(ctx, &wg)

		case res := <-a.results:
			a.handleResult(res)
			_ = a.persistCursor() // best-effort; in-memory state stays correct either way

		case <-compactTicker.C:
			a.runCompaction()
		}
	}
}

func (a *Agent) readNew() error {
	lines, newOffset, err := readNewLines(a.cfg.QueuePath, a.readPos)
	if err != nil {
		return err
	}
	a.readPos = newOffset

	for _, line := range lines {
		var ev queueEvent
		if err := json.Unmarshal(line, &ev); err != nil || ev.SessionID == "" {
			continue // malformed or incomplete line — skip, don't fail the batch
		}

		s, ok := a.sessions[ev.SessionID]
		if !ok {
			s = &sessionSchedule{}
			a.sessions[ev.SessionID] = s
		}
		s.lastSeenPos = a.readPos

		if ev.Event == "SessionEnd" {
			s.final = true
			s.finalAtPos = a.readPos
			s.dueAt = a.cfg.now() // immediate
		} else if !s.uploading && s.dueAt.IsZero() {
			// Starts the debounce window on this session's first new
			// event; later events before it fires don't push it back —
			// "at most one upload per session every UploadInterval" means
			// a chatty session must not starve its own upload.
			s.dueAt = a.cfg.now().Add(a.cfg.UploadInterval)
		}
	}
	return nil
}

func (a *Agent) dispatchDue(ctx context.Context, wg *sync.WaitGroup) {
	now := a.cfg.now()
	for id, s := range a.sessions {
		if s.uploading || s.dueAt.IsZero() || now.Before(s.dueAt) {
			continue
		}

		select {
		case a.sem <- struct{}{}:
		default:
			continue // pool is full; this session gets picked up again next tick
		}

		s.uploading = true
		through := s.lastSeenPos

		wg.Add(1)
		go func(sessionID string, dispatchedThrough int64) {
			defer wg.Done()
			defer func() { <-a.sem }()

			err := a.cfg.Uploader.Upload(ctx, sessionID)
			select {
			case a.results <- uploadResult{sessionID: sessionID, dispatchedThrough: dispatchedThrough, err: err}:
			case <-ctx.Done():
			}
		}(id, through)
	}
}

func (a *Agent) handleResult(res uploadResult) {
	s, ok := a.sessions[res.sessionID]
	if !ok {
		return
	}
	s.uploading = false

	if res.err != nil {
		// CO-112 owns real retry/backoff policy; for now just try again
		// after one more debounce window instead of spinning.
		s.dueAt = a.cfg.now().Add(a.cfg.UploadInterval)
		return
	}

	if res.dispatchedThrough > s.confirmedThrough {
		s.confirmedThrough = res.dispatchedThrough
	}
	if s.finalAtPos != 0 && s.confirmedThrough >= s.finalAtPos {
		s.final = false
		s.finalAtPos = 0
	}

	if s.lastSeenPos > s.confirmedThrough {
		// New events arrived while this upload was in flight.
		if s.final {
			s.dueAt = a.cfg.now()
		} else {
			s.dueAt = a.cfg.now().Add(a.cfg.UploadInterval)
		}
	} else {
		s.dueAt = time.Time{} // caught up; the next new event starts a fresh window
	}
}

// watermark is the offset safe to persist as the cursor and to compact up
// to: the earliest confirmedThrough among sessions that still have
// unconfirmed events, or the full read position if every session is
// caught up.
func (a *Agent) watermark() int64 {
	w := a.readPos
	for _, s := range a.sessions {
		if s.lastSeenPos > s.confirmedThrough && s.confirmedThrough < w {
			w = s.confirmedThrough
		}
	}
	return w
}

func (a *Agent) persistCursor() error {
	return saveCursor(a.cfg.CursorPath, cursorState{Offset: a.watermark()})
}

func (a *Agent) runCompaction() {
	removed, err := compact(a.cfg.QueuePath, a.watermark())
	if err != nil || removed == 0 {
		return
	}

	a.readPos -= removed
	for _, s := range a.sessions {
		s.lastSeenPos -= removed
		s.confirmedThrough -= removed
		if s.finalAtPos != 0 {
			s.finalAtPos -= removed
		}
	}
	_ = a.persistCursor()
}

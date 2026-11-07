// Package upload implements CO-110: the real agent.Uploader that
// compresses, hashes, chunks, and uploads a session's new transcript
// bytes — the main stream and any subagent child streams (CO-109) — via
// register-chunk, a PUT to a signed Storage URL, and confirm-chunk,
// advancing persisted pointer state (CO-109) only once a chunk is
// confirmed durable.
package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/devminified/carryon/internal/agent"
	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/reader"
	"github.com/devminified/carryon/internal/state"
)

// maxUncompressedChunkBytes is CO-110's 8MB split threshold: no single
// chunk request — register-chunk, the PUT, or confirm-chunk — is ever
// large, regardless of how much new data a read produces at once.
const maxUncompressedChunkBytes = 8 * 1024 * 1024

// mainStreamID is the stream_id used for a session's own transcript, as
// opposed to one of its subagent children (identified by their own
// agent_id) — see supabase/functions/_shared/chunks.ts.
const mainStreamID = "main"

// TokenSource returns a valid access token for the caller.
// *auth.Provider.AccessToken matches this signature.
type TokenSource func(ctx context.Context) (string, error)

// ClientSource returns the Edge Function client to use. A plain
// func() (*api.Client, error) { return client, nil } is enough once a
// Client is available up front; it's a function (not a bare *api.Client)
// so a caller whose Client depends on config that might not exist yet
// (cmd/carryon's agent command — see its lazyAPI) can resolve it lazily,
// retried on every call, instead of Uploader construction itself failing
// before the agent service even starts.
type ClientSource func() (*api.Client, error)

// Snapshotter takes a non-destructive git snapshot of a session's
// working tree (CO-201) after its chunk upload succeeds.
// *snapshot.Snapshotter satisfies this.
type Snapshotter interface {
	MaybeSnapshot(ctx context.Context, sessionID, cwd string, final bool) error
}

// Config configures an Uploader. ClientSource, TokenSource and StateDir
// are required; Snapshotter is optional (CO-201's snapshot step is
// skipped entirely if nil, matching this package's own ClientSource/
// TokenSource lazy-resolution pattern rather than requiring every
// caller to wire one up).
type Config struct {
	ClientSource ClientSource
	TokenSource  TokenSource
	// StateDir is ~/.carryon/state — internal/state's pointer files live
	// here, one per stream.
	StateDir string
	// Snapshotter, if set, is asked to take a working-tree snapshot
	// after the main transcript stream uploads successfully.
	Snapshotter Snapshotter
	// HTTPClient is used for the PUT to each chunk's signed upload URL.
	// Defaults to a 60-second-timeout client.
	HTTPClient *http.Client
}

// Uploader implements agent.Uploader (CO-107) for real.
type Uploader struct {
	clientSource ClientSource
	tokenSource  TokenSource
	stateDir     string
	snapshotter  Snapshotter
	httpClient   *http.Client
}

// New validates cfg and returns an Uploader.
func New(cfg Config) (*Uploader, error) {
	if cfg.ClientSource == nil {
		return nil, fmt.Errorf("upload: ClientSource is required")
	}
	if cfg.TokenSource == nil {
		return nil, fmt.Errorf("upload: TokenSource is required")
	}
	if cfg.StateDir == "" {
		return nil, fmt.Errorf("upload: StateDir is required")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &Uploader{
		clientSource: cfg.ClientSource,
		tokenSource:  cfg.TokenSource,
		stateDir:     cfg.StateDir,
		snapshotter:  cfg.Snapshotter,
		httpClient:   httpClient,
	}, nil
}

// Upload implements agent.Uploader. session.ProjectID must already be set
// — only internal/filter.Filter should ever call an Uploader that isn't a
// no-op placeholder, and Filter sets it from its own match-project lookup
// before calling through.
//
// If session.Final is set (the agent dispatched this call to cover a
// SessionEnd event), Upload calls end-session (CO-111) once every stream
// has uploaded successfully — never before, so a session is only marked
// ended once its last bytes are actually confirmed durable.
//
// Every returned error is run through agent.ClassifyError (CO-112)
// before it reaches the caller, so Agent's retry/backoff policy sees a
// consistent fatal-vs-retryable split regardless of which call inside
// Upload actually failed.
func (u *Uploader) Upload(ctx context.Context, session agent.Session) error {
	if session.ProjectID == "" {
		return fmt.Errorf("upload: session %s has no ProjectID — must be called through filter.Filter", session.ID)
	}

	meta := gatherMetadata(ctx, session.Cwd)

	if err := u.uploadStream(ctx, session, mainStreamID, session.ID, session.TranscriptPath, meta); err != nil {
		return agent.ClassifyError(fmt.Errorf("uploading main transcript: %w", err))
	}

	if u.snapshotter != nil {
		if err := u.snapshotter.MaybeSnapshot(ctx, session.ID, session.Cwd, session.Final); err != nil {
			// Best-effort (CO-201): a snapshot failure — git push
			// rejected, a network hiccup to GitHub — must never
			// undermine the transcript capture that already succeeded
			// above. I1's "never blocks the developer" extends to
			// this too, so it's logged, not returned as Upload's own
			// error.
			slog.Warn(fmt.Sprintf("snapshot for session %s failed: %v", session.ID, err))
		}
	}

	subagents, err := reader.Subagents(session.TranscriptPath)
	if err != nil {
		return fmt.Errorf("listing subagents: %w", err)
	}
	for _, sa := range subagents {
		stateKey := state.SubagentKey(session.ID, sa.AgentID)
		if err := u.uploadStream(ctx, session, sa.AgentID, stateKey, sa.Path, meta); err != nil {
			return agent.ClassifyError(fmt.Errorf("uploading subagent %s: %w", sa.AgentID, err))
		}
	}

	if session.Final {
		token, err := u.tokenSource(ctx)
		if err != nil {
			return agent.ClassifyError(fmt.Errorf("getting access token for end-session: %w", err))
		}
		client, err := u.clientSource()
		if err != nil {
			return fmt.Errorf("getting api client for end-session: %w", err)
		}
		if err := client.EndSession(ctx, token, session.ID); err != nil {
			return agent.ClassifyError(fmt.Errorf("ending session: %w", err))
		}
	}
	return nil
}

// uploadStream reads whatever's new for one stream (the main transcript,
// or one subagent) and uploads it as one or more chunks, persisting state
// after each confirmed chunk so a crash mid-stream only re-uploads what
// wasn't yet confirmed (I3) — never the whole thing again.
func (u *Uploader) uploadStream(ctx context.Context, session agent.Session, streamID, stateKey, path string, meta sessionMetadata) error {
	prior, err := state.Load(u.stateDir, stateKey)
	if err != nil {
		return fmt.Errorf("loading state for %s: %w", stateKey, err)
	}

	data, next, _, err := reader.Read(path, prior)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if len(data) == 0 {
		return nil
	}

	// next.Offset - len(data) is where data actually starts in path's
	// current byte coordinates — not prior.Offset, which no longer applies
	// once reader.Read has reset to 0 after detecting a rewrite.
	pos := next.Offset - int64(len(data))
	seq := prior.NextSeq

	for len(data) > 0 {
		n := len(data)
		if n > maxUncompressedChunkBytes {
			n = maxUncompressedChunkBytes
		}
		piece := data[:n]
		data = data[n:]

		byteStart := pos
		byteEnd := pos + int64(n)
		pos = byteEnd

		if err := u.uploadChunk(ctx, session, streamID, seq, byteStart, byteEnd, piece, meta); err != nil {
			return fmt.Errorf("chunk seq %d: %w", seq, err)
		}
		seq++

		confirmed := state.Pointer{
			Offset:   byteEnd,
			NextSeq:  seq,
			FileSize: next.FileSize,
			HeadHash: next.HeadHash,
		}
		if err := state.Save(u.stateDir, stateKey, confirmed); err != nil {
			return fmt.Errorf("saving state after chunk seq %d: %w", seq-1, err)
		}
	}
	return nil
}

// uploadChunk compresses, hashes, registers, PUTs, and confirms one
// chunk. If the PUT fails, it re-registers for a fresh signed URL and
// retries once — CO-110's "an expired signed URL is re-requested
// automatically" testing point. Broader retry/backoff policy belongs to
// CO-112, not here.
func (u *Uploader) uploadChunk(ctx context.Context, session agent.Session, streamID string, seq int, byteStart, byteEnd int64, data []byte, meta sessionMetadata) error {
	sum := sha256.Sum256(data)
	sha256Hex := hex.EncodeToString(sum[:])

	compressed, err := compress(data)
	if err != nil {
		return fmt.Errorf("compressing: %w", err)
	}

	token, err := u.tokenSource(ctx)
	if err != nil {
		return fmt.Errorf("getting access token: %w", err)
	}
	client, err := u.clientSource()
	if err != nil {
		return fmt.Errorf("getting api client: %w", err)
	}

	regInput := api.RegisterChunkInput{
		SessionID:         session.ID,
		ProjectID:         session.ProjectID,
		OriginalCwd:       session.Cwd,
		StreamID:          streamID,
		Machine:           meta.Machine,
		Branch:            meta.Branch,
		HeadSHA:           meta.HeadSHA,
		ClaudeCodeVersion: meta.ClaudeCodeVersion,
		Seq:               seq,
		ByteStart:         byteStart,
		ByteEnd:           byteEnd,
		SHA256:            sha256Hex,
		Size:              byteEnd - byteStart,
	}
	reg, err := client.RegisterChunk(ctx, token, regInput)
	if err != nil {
		return fmt.Errorf("registering chunk: %w", err)
	}

	if err := u.put(ctx, client.StorageUploadURL(reg.ObjectKey, reg.UploadToken), compressed); err != nil {
		reg, err = client.RegisterChunk(ctx, token, regInput)
		if err != nil {
			return fmt.Errorf("re-registering after a failed upload: %w", err)
		}
		if err := u.put(ctx, client.StorageUploadURL(reg.ObjectKey, reg.UploadToken), compressed); err != nil {
			return fmt.Errorf("uploading (after re-registering for a fresh URL): %w", err)
		}
	}

	if err := client.ConfirmChunk(ctx, token, api.ConfirmChunkInput{
		SessionID: session.ID,
		StreamID:  streamID,
		Seq:       seq,
		ByteStart: byteStart,
		ByteEnd:   byteEnd,
		SHA256:    sha256Hex,
	}); err != nil {
		return fmt.Errorf("confirming: %w", err)
	}
	return nil
}

func (u *Uploader) put(ctx context.Context, url string, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("building PUT request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(data))

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("PUT returned HTTP %d", resp.StatusCode)
	}
	return nil
}

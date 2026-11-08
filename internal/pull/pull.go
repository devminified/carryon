// Package pull implements CO-205's session rebuild: given a CO-203
// manifest, download every chunk through a bounded worker pool,
// decompress and verify each against its SHA-256, and concatenate them
// in seq order into session.jsonl (the "main" stream) and one file per
// subagent stream under subagents/<streamID>.jsonl (CO-109's child
// transcripts). Fetching the manifest itself (Client.GetManifest) and
// everything after rebuild — rewrite, place, launch — belong to later
// tickets (CO-206 onward); this package only gets the bytes down and
// verified.
package pull

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/devminified/carryon/internal/api"
)

// mainStreamID matches internal/upload's own constant — the session's
// own transcript, as opposed to one of its subagent children.
const mainStreamID = "main"

// defaultWorkers is CO-205's "download all chunks in parallel (4 at a
// time)".
const defaultWorkers = 4

// defaultMaxRetries is CO-205's "retry it twice" — 2 retries, 3 attempts
// total, before a chunk is treated as a permanent failure.
const defaultMaxRetries = 2

// Config configures Rebuild. Use DefaultConfig and override only the
// fields a caller actually needs to change — Workers <= 0 is treated as
// 1 (a 0-sized semaphore would deadlock), but MaxRetries and RetryDelay
// are taken literally: 0 retries and 0 delay are both meaningful,
// deliberate choices, not "unset."
type Config struct {
	HTTPClient *http.Client
	Workers    int
	MaxRetries int
	RetryDelay time.Duration
}

// DefaultConfig returns CO-205's defaults: 4 workers, 2 retries, no
// delay between retries.
func DefaultConfig() Config {
	return Config{
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
		Workers:    defaultWorkers,
		MaxRetries: defaultMaxRetries,
	}
}

// Result is what Rebuild produced.
type Result struct {
	// MainFile is the rebuilt path for the "main" stream (session.jsonl),
	// or empty if the manifest had no "main" chunks.
	MainFile string
	// SubagentFiles maps each non-main stream ID to its rebuilt path
	// under subagents/ (CO-109).
	SubagentFiles map[string]string
	// TotalBytes is the sum of every rebuilt stream's size.
	TotalBytes int64
}

// GapError means a stream's chunks have a gap in the manifest itself —
// a missing seq, or a byte range that doesn't connect to the previous
// chunk's — detected before any network call. Per CO-205's testing
// point, a gap in the middle is always reported, never silently skipped.
type GapError struct {
	StreamID string
	// AfterSeq is the last seq that validated cleanly (-1 if the very
	// first chunk, seq 0, is itself missing or malformed).
	AfterSeq int
	Reason   string
}

func (e *GapError) Error() string {
	return fmt.Sprintf("stream %q has a gap after seq %d: %s", e.StreamID, e.AfterSeq, e.Reason)
}

// ChunkError means one chunk failed to download, decompress, or verify
// after every retry. Err is the last attempt's failure.
type ChunkError struct {
	StreamID string
	Seq      int
	Err      error
}

func (e *ChunkError) Error() string {
	return fmt.Sprintf("stream %q seq %d: %v", e.StreamID, e.Seq, e.Err)
}

func (e *ChunkError) Unwrap() error { return e.Err }

// Rebuild downloads, verifies and assembles every chunk in manifest
// into destDir/session.jsonl (the "main" stream) and
// destDir/subagents/<streamID>.jsonl for every other stream. client is
// used only to build each chunk's signed download URL
// (Client.StorageDownloadURL) — no access token is needed here, since
// the signed token itself already grants access.
func Rebuild(ctx context.Context, client *api.Client, manifest *api.Manifest, destDir string, cfg Config) (*Result, error) {
	streams, order, err := groupAndValidate(manifest.Chunks)
	if err != nil {
		return nil, err
	}

	downloaded, err := downloadAll(ctx, client, cfg, manifest.Chunks)
	if err != nil {
		return nil, err
	}

	result := &Result{SubagentFiles: map[string]string{}}
	for _, streamID := range order {
		chunks := streams[streamID]

		var path string
		if streamID == mainStreamID {
			path = filepath.Join(destDir, "session.jsonl")
		} else {
			path = filepath.Join(destDir, "subagents", streamID+".jsonl")
		}

		n, err := writeStream(path, chunks, downloaded)
		if err != nil {
			return nil, err
		}

		result.TotalBytes += n
		if streamID == mainStreamID {
			result.MainFile = path
		} else {
			result.SubagentFiles[streamID] = path
		}
	}
	return result, nil
}

// groupAndValidate groups chunks by stream, sorts each group by seq,
// and verifies every chunk's seq is contiguous from 0 and every byte
// range connects to the previous chunk's. order preserves each stream's
// first-seen position in chunks, so Rebuild's output (and any error) is
// deterministic regardless of Go's map iteration order.
func groupAndValidate(chunks []api.ManifestChunk) (streams map[string][]api.ManifestChunk, order []string, err error) {
	streams = make(map[string][]api.ManifestChunk)
	for _, c := range chunks {
		if _, seen := streams[c.StreamID]; !seen {
			order = append(order, c.StreamID)
		}
		streams[c.StreamID] = append(streams[c.StreamID], c)
	}

	for _, streamID := range order {
		group := streams[streamID]
		sort.Slice(group, func(i, j int) bool { return group[i].Seq < group[j].Seq })

		var wantStart int64
		for i, c := range group {
			if c.Seq != i {
				return nil, nil, &GapError{
					StreamID: streamID, AfterSeq: i - 1,
					Reason: fmt.Sprintf("expected seq %d, got seq %d", i, c.Seq),
				}
			}
			if c.ByteStart != wantStart {
				return nil, nil, &GapError{
					StreamID: streamID, AfterSeq: i - 1,
					Reason: fmt.Sprintf("seq %d starts at byte %d, want %d (not contiguous with the previous chunk)", c.Seq, c.ByteStart, wantStart),
				}
			}
			if c.ByteEnd <= c.ByteStart {
				return nil, nil, &GapError{
					StreamID: streamID, AfterSeq: i - 1,
					Reason: fmt.Sprintf("seq %d has an empty or invalid byte range [%d, %d)", c.Seq, c.ByteStart, c.ByteEnd),
				}
			}
			wantStart = c.ByteEnd
		}
		streams[streamID] = group
	}
	return streams, order, nil
}

// chunkKey identifies one chunk across streams — seq numbers reset to 0
// in every stream, so seq alone isn't unique.
type chunkKey struct {
	streamID string
	seq      int
}

// downloadAll downloads every chunk (across every stream, as one flat
// bounded pool — CO-205's "download all chunks in parallel (4 at a
// time)" doesn't distinguish streams) and returns each one's verified,
// decompressed bytes. On the first chunk that exhausts its retries,
// every other in-flight download is cancelled and that chunk's
// *ChunkError is returned.
func downloadAll(ctx context.Context, client *api.Client, cfg Config, chunks []api.ManifestChunk) (map[chunkKey][]byte, error) {
	workers := cfg.Workers
	if workers <= 0 {
		workers = 1
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, workers)
	results := make(map[chunkKey][]byte, len(chunks))
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once

	for _, chunk := range chunks {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			// wg.Wait() lets any goroutine already in flight finish
			// setting firstErr via errOnce before we check it — but if
			// ctx was already done before we ever dispatched anything
			// (e.g. the caller's own context was cancelled going in),
			// no goroutine ever ran and firstErr is still nil; report
			// ctx.Err() itself rather than a false (nil, nil) success.
			wg.Wait()
			if firstErr != nil {
				return nil, firstErr
			}
			return nil, ctx.Err()
		}

		wg.Add(1)
		go func(chunk api.ManifestChunk) {
			defer wg.Done()
			defer func() { <-sem }()

			data, err := downloadChunkWithRetry(ctx, client, cfg, chunk)
			if err != nil {
				errOnce.Do(func() {
					firstErr = err
					cancel()
				})
				return
			}
			mu.Lock()
			results[chunkKey{chunk.StreamID, chunk.Seq}] = data
			mu.Unlock()
		}(chunk)
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

func downloadChunkWithRetry(ctx context.Context, client *api.Client, cfg Config, chunk api.ManifestChunk) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			if cfg.RetryDelay > 0 {
				select {
				case <-time.After(cfg.RetryDelay):
				case <-ctx.Done():
					return nil, &ChunkError{StreamID: chunk.StreamID, Seq: chunk.Seq, Err: ctx.Err()}
				}
			}
			if ctx.Err() != nil {
				return nil, &ChunkError{StreamID: chunk.StreamID, Seq: chunk.Seq, Err: ctx.Err()}
			}
		}

		data, err := downloadOneChunk(ctx, client, cfg, chunk)
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	return nil, &ChunkError{StreamID: chunk.StreamID, Seq: chunk.Seq, Err: lastErr}
}

func downloadOneChunk(ctx context.Context, client *api.Client, cfg Config, chunk api.ManifestChunk) ([]byte, error) {
	url := client.StorageDownloadURL(chunk.Bucket, chunk.ObjectKey, chunk.DownloadToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building download request: %w", err)
	}

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	compressed, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("creating zstd decoder: %w", err)
	}
	defer dec.Close()

	data, err := dec.DecodeAll(compressed, nil)
	if err != nil {
		return nil, fmt.Errorf("decompressing: %w", err)
	}

	sum := sha256.Sum256(data)
	if gotHash := hex.EncodeToString(sum[:]); gotHash != chunk.SHA256 {
		return nil, fmt.Errorf("sha256 mismatch: got %s, want %s", gotHash, chunk.SHA256)
	}

	// There used to be a check here that decompressed length equals
	// chunk.ByteEnd-chunk.ByteStart. That held before CO-301:
	// byteStart/byteEnd describe the ORIGINAL file's byte positions
	// (per internal/api.ConfirmChunkInput's own doc comment), but
	// redaction can both shrink a matched secret down to a shorter
	// "[REDACTED:id]" placeholder and, for a short match against a
	// longer rule ID, grow it — so the two lengths now legitimately
	// diverge in either direction. The SHA256 check above is already
	// the real, sufficient integrity guarantee (I4): it verifies these
	// are exactly the bytes that were uploaded, whatever their length.
	return data, nil
}

// writeStream writes chunks' bytes (already verified and in seq order
// by groupAndValidate) to path via a temp file + fsync + atomic rename
// (I2/I8 — Claude Code, or anything else, never sees a partial file).
func writeStream(path string, chunks []api.ManifestChunk, downloaded map[chunkKey][]byte) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, fmt.Errorf("creating directory for %s: %w", path, err)
	}

	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G304 -- tmp is derived from our own destDir, not external input
	if err != nil {
		return 0, fmt.Errorf("creating %s: %w", tmp, err)
	}

	var total int64
	for _, c := range chunks {
		data := downloaded[chunkKey{c.StreamID, c.Seq}]
		n, err := f.Write(data)
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return 0, fmt.Errorf("writing %s: %w", path, err)
		}
		total += int64(n)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("fsyncing %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("closing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return 0, fmt.Errorf("renaming %s to %s: %w", tmp, path, err)
	}
	return total, nil
}

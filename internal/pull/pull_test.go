package pull

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/devminified/carryon/internal/api"
)

func zstdCompress(t *testing.T, data []byte) []byte {
	t.Helper()
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(3)))
	if err != nil {
		t.Fatalf("zstd.NewWriter: %v", err)
	}
	defer func() { _ = enc.Close() }()
	return enc.EncodeAll(data, nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// splitChunks splits data into chunkSize-byte pieces for one stream,
// building the *api.ManifestChunk list Rebuild expects. Every piece's
// ObjectKey doubles as its DownloadToken in these tests — the fake
// server just looks bytes up by the path, same as it would via a real
// signed token in production.
func splitChunks(streamID string, data []byte, chunkSize int) []api.ManifestChunk {
	var chunks []api.ManifestChunk
	for start, seq := 0, 0; start < len(data); start, seq = start+chunkSize, seq+1 {
		end := start + chunkSize
		if end > len(data) {
			end = len(data)
		}
		piece := data[start:end]
		key := fmt.Sprintf("%s-%d", streamID, seq)
		chunks = append(chunks, api.ManifestChunk{
			StreamID: streamID, Seq: seq, ByteStart: int64(start), ByteEnd: int64(end),
			SHA256: sha256Hex(piece), ObjectKey: key, DownloadToken: key,
		})
	}
	return chunks
}

// fakeChunkServer serves every chunk registered via set, compressed,
// at the same URL shape Client.StorageDownloadURL builds. failFirst
// lets a test make a given object key fail N times before succeeding,
// to exercise the retry path; fail404 makes a key 404 forever
// (simulating a genuinely missing chunk, as opposed to a transient
// failure).
type fakeChunkServer struct {
	t           *testing.T
	srv         *httptest.Server
	objects     map[string][]byte // objectKey -> UNCOMPRESSED bytes (compressed on the fly, so a corrupt test can swap just the compressed bytes via corrupt)
	corrupt     map[string][]byte // objectKey -> literal bytes to serve instead of the real compression
	failFirst   map[string]*int32 // objectKey -> remaining failures before success
	fail404     map[string]bool
	requests    int32
	maxInFlight int32
	inFlight    int32
}

func newFakeChunkServer(t *testing.T) *fakeChunkServer {
	t.Helper()
	s := &fakeChunkServer{
		t: t, objects: map[string][]byte{}, corrupt: map[string][]byte{},
		failFirst: map[string]*int32{}, fail404: map[string]bool{},
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *fakeChunkServer) set(objectKey string, uncompressed []byte) {
	s.objects[objectKey] = uncompressed
}

func (s *fakeChunkServer) setCorrupt(objectKey string, rawBytes []byte) {
	s.corrupt[objectKey] = rawBytes
}

func (s *fakeChunkServer) setFail404(objectKey string) {
	s.fail404[objectKey] = true
}

func (s *fakeChunkServer) setFailFirst(objectKey string, n int32) {
	v := n
	s.failFirst[objectKey] = &v
}

func (s *fakeChunkServer) handle(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt32(&s.requests, 1)
	inFlight := atomic.AddInt32(&s.inFlight, 1)
	defer atomic.AddInt32(&s.inFlight, -1)
	for {
		max := atomic.LoadInt32(&s.maxInFlight)
		if inFlight <= max || atomic.CompareAndSwapInt32(&s.maxInFlight, max, inFlight) {
			break
		}
	}
	// A small sleep makes concurrency actually observable instead of
	// every request completing before the next is even dispatched.
	time.Sleep(5 * time.Millisecond)

	const prefix = "/storage/v1/object/sign/carryon-sessions/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(w, r)
		return
	}
	objectKey := strings.TrimPrefix(r.URL.Path, prefix)

	if s.fail404[objectKey] {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if remaining, ok := s.failFirst[objectKey]; ok && atomic.LoadInt32(remaining) > 0 {
		atomic.AddInt32(remaining, -1)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if raw, ok := s.corrupt[objectKey]; ok {
		_, _ = w.Write(raw)
		return
	}
	data, ok := s.objects[objectKey]
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(zstdCompress(s.t, data))
}

func (s *fakeChunkServer) client() *api.Client {
	return api.NewClient(s.srv.URL, "anon-key")
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return b
}

func registerStream(server *fakeChunkServer, streamID string, data []byte, chunkSize int) []api.ManifestChunk {
	chunks := splitChunks(streamID, data, chunkSize)
	for _, c := range chunks {
		server.set(c.ObjectKey, data[c.ByteStart:c.ByteEnd])
	}
	return chunks
}

func TestRebuildByteIdentical1KB(t *testing.T) {
	testRebuildByteIdentical(t, 1024, 200)
}

func TestRebuildByteIdentical5MB(t *testing.T) {
	testRebuildByteIdentical(t, 5*1024*1024, 256*1024)
}

func testRebuildByteIdentical(t *testing.T, totalSize, chunkSize int) {
	server := newFakeChunkServer(t)
	data := randomBytes(t, totalSize)
	chunks := registerStream(server, mainStreamID, data, chunkSize)

	manifest := &api.Manifest{Chunks: chunks}
	destDir := t.TempDir()

	cfg := DefaultConfig()
	result, err := Rebuild(context.Background(), server.client(), manifest, destDir, cfg)
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}

	got, err := os.ReadFile(result.MainFile)
	if err != nil {
		t.Fatalf("reading rebuilt file: %v", err)
	}
	if len(got) != len(data) {
		t.Fatalf("rebuilt file is %d bytes, want %d", len(got), len(data))
	}
	for i := range data {
		if got[i] != data[i] {
			t.Fatalf("rebuilt file differs from source at byte %d", i)
		}
	}
	if result.TotalBytes != int64(len(data)) {
		t.Errorf("TotalBytes = %d, want %d", result.TotalBytes, len(data))
	}

	if _, err := os.Stat(result.MainFile + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("a .tmp file was left behind: %v", err)
	}
}

// TestRebuildAcceptsAChunkWhoseDecompressedLengthDiffersFromItsByteRange
// is CO-301's regression test: byteStart/byteEnd describe the ORIGINAL
// (pre-redaction) file's positions, while the actual uploaded/
// downloaded bytes are the redacted content — a "[REDACTED:id]"
// placeholder is rarely the same length as the secret it replaced, so
// the two numbers legitimately diverge. Rebuild must still succeed as
// long as the SHA256 matches; it must not re-derive or check a
// decompressed-length-equals-byte-range invariant that no longer holds.
func TestRebuildAcceptsAChunkWhoseDecompressedLengthDiffersFromItsByteRange(t *testing.T) {
	server := newFakeChunkServer(t)
	redacted := []byte(`{"content":"[REDACTED:aws-access-key-id]"}` + "\n")
	server.set("main-0", redacted)

	chunks := []api.ManifestChunk{{
		StreamID: mainStreamID, Seq: 0,
		// The original line (before AKIA...EXAMPLE got replaced by the
		// shorter placeholder) was longer than the redacted bytes
		// actually stored — a real, expected mismatch now, not a bug.
		ByteStart: 0, ByteEnd: int64(len(redacted)) + 20,
		SHA256: sha256Hex(redacted), ObjectKey: "main-0", DownloadToken: "main-0",
	}}

	manifest := &api.Manifest{Chunks: chunks}
	destDir := t.TempDir()

	result, err := Rebuild(context.Background(), server.client(), manifest, destDir, DefaultConfig())
	if err != nil {
		t.Fatalf("Rebuild() error = %v, want success (SHA256 matches; byte-range length mismatch is expected post-redaction)", err)
	}

	got, err := os.ReadFile(result.MainFile)
	if err != nil {
		t.Fatalf("reading rebuilt file: %v", err)
	}
	if string(got) != string(redacted) {
		t.Fatalf("rebuilt file = %q, want %q", got, redacted)
	}
}

func TestRebuildWritesSubagentStreamsUnderSubagentsDir(t *testing.T) {
	server := newFakeChunkServer(t)
	mainData := randomBytes(t, 2048)
	subData := randomBytes(t, 1024)

	mainChunks := registerStream(server, mainStreamID, mainData, 512)
	subChunks := registerStream(server, "agent-42", subData, 512)

	manifest := &api.Manifest{Chunks: append(mainChunks, subChunks...)}
	destDir := t.TempDir()

	result, err := Rebuild(context.Background(), server.client(), manifest, destDir, DefaultConfig())
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}

	if result.MainFile != filepath.Join(destDir, "session.jsonl") {
		t.Errorf("MainFile = %q", result.MainFile)
	}
	subPath, ok := result.SubagentFiles["agent-42"]
	if !ok {
		t.Fatalf("SubagentFiles missing agent-42: %+v", result.SubagentFiles)
	}
	if want := filepath.Join(destDir, "subagents", "agent-42.jsonl"); subPath != want {
		t.Errorf("SubagentFiles[agent-42] = %q, want %q", subPath, want)
	}

	got, err := os.ReadFile(subPath)
	if err != nil {
		t.Fatalf("reading subagent file: %v", err)
	}
	if string(got) != string(subData) {
		t.Error("subagent file content doesn't match the source")
	}
}

func TestRebuildEmptyManifestProducesNoFiles(t *testing.T) {
	server := newFakeChunkServer(t)
	manifest := &api.Manifest{Chunks: nil}
	destDir := t.TempDir()

	result, err := Rebuild(context.Background(), server.client(), manifest, destDir, DefaultConfig())
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	if result.MainFile != "" {
		t.Errorf("MainFile = %q, want empty", result.MainFile)
	}
	if len(result.SubagentFiles) != 0 {
		t.Errorf("SubagentFiles = %+v, want empty", result.SubagentFiles)
	}
	if atomic.LoadInt32(&server.requests) != 0 {
		t.Errorf("server got %d requests, want 0 for an empty manifest", server.requests)
	}
}

func TestRebuildDetectsAMissingChunkInTheMiddleAsAGap(t *testing.T) {
	server := newFakeChunkServer(t)
	chunks := []api.ManifestChunk{
		{StreamID: mainStreamID, Seq: 0, ByteStart: 0, ByteEnd: 10, SHA256: "a", ObjectKey: "k0", DownloadToken: "k0"},
		{StreamID: mainStreamID, Seq: 1, ByteStart: 10, ByteEnd: 20, SHA256: "b", ObjectKey: "k1", DownloadToken: "k1"},
		// seq 2 is missing entirely — the manifest jumps straight to 3.
		{StreamID: mainStreamID, Seq: 3, ByteStart: 30, ByteEnd: 40, SHA256: "d", ObjectKey: "k3", DownloadToken: "k3"},
	}
	manifest := &api.Manifest{Chunks: chunks}

	_, err := Rebuild(context.Background(), server.client(), manifest, t.TempDir(), DefaultConfig())
	if err == nil {
		t.Fatal("Rebuild() error = nil, want a gap error")
	}
	var gapErr *GapError
	if !errors.As(err, &gapErr) {
		t.Fatalf("Rebuild() error = %v, want *GapError", err)
	}
	if gapErr.StreamID != mainStreamID || gapErr.AfterSeq != 1 {
		t.Errorf("GapError = %+v, want StreamID=main AfterSeq=1", gapErr)
	}
	if atomic.LoadInt32(&server.requests) != 0 {
		t.Errorf("server got %d requests, want 0 — a gap must be caught before any download", server.requests)
	}
}

func TestRebuildDetectsANonContiguousByteRangeAsAGap(t *testing.T) {
	server := newFakeChunkServer(t)
	chunks := []api.ManifestChunk{
		{StreamID: mainStreamID, Seq: 0, ByteStart: 0, ByteEnd: 100, SHA256: "a", ObjectKey: "k0", DownloadToken: "k0"},
		// seq 1 claims to start at byte 105, but seq 0 ended at byte 100.
		{StreamID: mainStreamID, Seq: 1, ByteStart: 105, ByteEnd: 200, SHA256: "b", ObjectKey: "k1", DownloadToken: "k1"},
	}
	manifest := &api.Manifest{Chunks: chunks}

	_, err := Rebuild(context.Background(), server.client(), manifest, t.TempDir(), DefaultConfig())
	var gapErr *GapError
	if !errors.As(err, &gapErr) {
		t.Fatalf("Rebuild() error = %v, want *GapError", err)
	}
	if !strings.Contains(gapErr.Reason, "not contiguous") {
		t.Errorf("GapError.Reason = %q, want it to mention the byte ranges aren't contiguous", gapErr.Reason)
	}
}

func TestRebuildDetectsACorruptedChunkAndNamesIt(t *testing.T) {
	server := newFakeChunkServer(t)
	data := randomBytes(t, 300)
	chunks := registerStream(server, mainStreamID, data, 100) // 3 chunks: seq 0,1,2

	// Corrupt seq 1: serve a different payload's compression, so it
	// decompresses fine but its SHA-256 doesn't match the manifest's —
	// exactly what a flipped byte in storage would produce.
	server.setCorrupt("main-1", zstdCompress(t, randomBytes(t, 100)))

	manifest := &api.Manifest{Chunks: chunks}
	cfg := DefaultConfig()
	cfg.MaxRetries = 0 // fail fast, this isn't testing retries

	_, err := Rebuild(context.Background(), server.client(), manifest, t.TempDir(), cfg)
	if err == nil {
		t.Fatal("Rebuild() error = nil, want a chunk error")
	}
	var chunkErr *ChunkError
	if !errors.As(err, &chunkErr) {
		t.Fatalf("Rebuild() error = %v, want *ChunkError", err)
	}
	if chunkErr.StreamID != mainStreamID || chunkErr.Seq != 1 {
		t.Errorf("ChunkError = %+v, want StreamID=main Seq=1", chunkErr)
	}
	if !strings.Contains(chunkErr.Error(), "sha256 mismatch") {
		t.Errorf("error = %q, want it to mention a sha256 mismatch", chunkErr.Error())
	}
}

func TestRebuildDetectsAnUndecompressableChunk(t *testing.T) {
	server := newFakeChunkServer(t)
	data := randomBytes(t, 100)
	chunks := registerStream(server, mainStreamID, data, 100)

	server.setCorrupt("main-0", []byte("this is not valid zstd data at all"))

	manifest := &api.Manifest{Chunks: chunks}
	cfg := DefaultConfig()
	cfg.MaxRetries = 0

	_, err := Rebuild(context.Background(), server.client(), manifest, t.TempDir(), cfg)
	var chunkErr *ChunkError
	if !errors.As(err, &chunkErr) {
		t.Fatalf("Rebuild() error = %v, want *ChunkError", err)
	}
	if !strings.Contains(chunkErr.Error(), "decompressing") {
		t.Errorf("error = %q, want it to mention decompression failed", chunkErr.Error())
	}
}

func TestRebuildReturnsTheFailingSeqAfterExhaustingRetries(t *testing.T) {
	server := newFakeChunkServer(t)
	data := randomBytes(t, 200)
	chunks := registerStream(server, mainStreamID, data, 100) // seq 0, 1
	server.setFail404("main-1")                               // seq 1 never exists

	manifest := &api.Manifest{Chunks: chunks}
	cfg := DefaultConfig()
	cfg.MaxRetries = 2

	_, err := Rebuild(context.Background(), server.client(), manifest, t.TempDir(), cfg)
	var chunkErr *ChunkError
	if !errors.As(err, &chunkErr) {
		t.Fatalf("Rebuild() error = %v, want *ChunkError", err)
	}
	if chunkErr.Seq != 1 {
		t.Errorf("ChunkError.Seq = %d, want 1", chunkErr.Seq)
	}
}

func TestRebuildRetriesATransientFailureAndSucceeds(t *testing.T) {
	server := newFakeChunkServer(t)
	data := randomBytes(t, 100)
	chunks := registerStream(server, mainStreamID, data, 100)
	server.setFailFirst("main-0", 2) // fails twice, then succeeds on the 3rd attempt

	manifest := &api.Manifest{Chunks: chunks}
	cfg := DefaultConfig()
	cfg.MaxRetries = 2

	result, err := Rebuild(context.Background(), server.client(), manifest, t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("Rebuild() error = %v, want success after retries", err)
	}
	got, err := os.ReadFile(result.MainFile)
	if err != nil {
		t.Fatalf("reading rebuilt file: %v", err)
	}
	if string(got) != string(data) {
		t.Error("rebuilt file content doesn't match the source after a retried download")
	}
}

func TestRebuildDoesNotWriteAFileWhenAChunkFailsForGood(t *testing.T) {
	server := newFakeChunkServer(t)
	data := randomBytes(t, 200)
	chunks := registerStream(server, mainStreamID, data, 100)
	server.setFail404("main-1")

	manifest := &api.Manifest{Chunks: chunks}
	destDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.MaxRetries = 0

	if _, err := Rebuild(context.Background(), server.client(), manifest, destDir, cfg); err == nil {
		t.Fatal("Rebuild() error = nil, want an error")
	}
	if _, err := os.Stat(filepath.Join(destDir, "session.jsonl")); !os.IsNotExist(err) {
		t.Errorf("session.jsonl should not exist after a failed rebuild, stat error = %v", err)
	}
}

func TestRebuildRespectsTheWorkerPoolBound(t *testing.T) {
	server := newFakeChunkServer(t)
	data := randomBytes(t, 12*64*1024)
	chunks := registerStream(server, mainStreamID, data, 64*1024) // 12 chunks

	manifest := &api.Manifest{Chunks: chunks}
	cfg := DefaultConfig()
	cfg.Workers = 3

	if _, err := Rebuild(context.Background(), server.client(), manifest, t.TempDir(), cfg); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	if max := atomic.LoadInt32(&server.maxInFlight); max > int32(cfg.Workers) {
		t.Errorf("max in-flight requests = %d, want <= %d (Workers)", max, cfg.Workers)
	}
	if max := atomic.LoadInt32(&server.maxInFlight); max < 2 {
		t.Errorf("max in-flight requests = %d, want some real concurrency (> 1) to prove this isn't accidentally sequential", max)
	}
}

func TestRebuildCancelsOnContextDone(t *testing.T) {
	server := newFakeChunkServer(t)
	data := randomBytes(t, 20*64*1024)
	chunks := registerStream(server, mainStreamID, data, 64*1024)

	manifest := &api.Manifest{Chunks: chunks}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before Rebuild even starts

	cfg := DefaultConfig()
	cfg.MaxRetries = 0
	_, err := Rebuild(ctx, server.client(), manifest, t.TempDir(), cfg)
	if err == nil {
		t.Fatal("Rebuild() with an already-cancelled context = nil error, want an error")
	}
}

func TestGroupAndValidateOrderIsDeterministic(t *testing.T) {
	data := randomBytes(t, 10)
	chunks := []api.ManifestChunk{
		{StreamID: "agent-b", Seq: 0, ByteStart: 0, ByteEnd: 5, SHA256: sha256Hex(data[:5])},
		{StreamID: mainStreamID, Seq: 0, ByteStart: 0, ByteEnd: 5, SHA256: sha256Hex(data[:5])},
		{StreamID: "agent-a", Seq: 0, ByteStart: 0, ByteEnd: 5, SHA256: sha256Hex(data[:5])},
	}
	_, order, err := groupAndValidate(chunks)
	if err != nil {
		t.Fatalf("groupAndValidate() error = %v", err)
	}
	want := []string{"agent-b", mainStreamID, "agent-a"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("order[%d] = %q, want %q", i, order[i], want[i])
		}
	}
}

func TestGroupAndValidateDetectsAnEmptyByteRange(t *testing.T) {
	chunks := []api.ManifestChunk{
		{StreamID: mainStreamID, Seq: 0, ByteStart: 0, ByteEnd: 0, SHA256: "a"},
	}
	_, _, err := groupAndValidate(chunks)
	var gapErr *GapError
	if !errors.As(err, &gapErr) {
		t.Fatalf("groupAndValidate() error = %v, want *GapError", err)
	}
	if !strings.Contains(gapErr.Reason, "empty or invalid byte range") {
		t.Errorf("GapError.Reason = %q", gapErr.Reason)
	}
}

func TestGapErrorMessageNamesTheStreamAndSeq(t *testing.T) {
	err := &GapError{StreamID: "main", AfterSeq: 2, Reason: "something broke"}
	msg := err.Error()
	if !strings.Contains(msg, "main") || !strings.Contains(msg, "2") || !strings.Contains(msg, "something broke") {
		t.Errorf("GapError.Error() = %q, missing stream/seq/reason", msg)
	}
}

func TestChunkErrorUnwrapsToTheUnderlyingError(t *testing.T) {
	underlying := errors.New("boom")
	err := &ChunkError{StreamID: "main", Seq: 3, Err: underlying}
	if !errors.Is(err, underlying) {
		t.Errorf("errors.Is(ChunkError, underlying) = false, want true (Unwrap should expose it)")
	}
}

func TestRebuildFailsCleanlyWhenTheDestinationPathIsBlocked(t *testing.T) {
	server := newFakeChunkServer(t)
	data := randomBytes(t, 50)
	chunks := registerStream(server, mainStreamID, data, 50)
	manifest := &api.Manifest{Chunks: chunks}

	// destDir is a regular FILE, not a directory — os.MkdirAll(destDir)
	// for session.jsonl's parent must fail cleanly rather than panic or
	// silently succeed.
	destDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(destDir, []byte("x"), 0o600); err != nil {
		t.Fatalf("writing blocking file: %v", err)
	}

	if _, err := Rebuild(context.Background(), server.client(), manifest, destDir, DefaultConfig()); err == nil {
		t.Fatal("Rebuild() error = nil, want an error when destDir is blocked by a file")
	}
}

func TestRebuildCancelsDuringARetryDelay(t *testing.T) {
	server := newFakeChunkServer(t)
	data := randomBytes(t, 50)
	chunks := registerStream(server, mainStreamID, data, 50)
	server.setFail404(chunks[0].ObjectKey) // always fails, forcing a retry wait

	manifest := &api.Manifest{Chunks: chunks}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	cfg := DefaultConfig()
	cfg.MaxRetries = 5
	cfg.RetryDelay = 500 * time.Millisecond // long enough that the ctx timeout wins

	start := time.Now()
	_, err := Rebuild(ctx, server.client(), manifest, t.TempDir(), cfg)
	if err == nil {
		t.Fatal("Rebuild() error = nil, want an error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Rebuild() took %v, want it to stop quickly once ctx was done during the retry delay", elapsed)
	}
}

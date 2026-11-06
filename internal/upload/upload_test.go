package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/devminified/carryon/internal/agent"
	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/state"
)

// fakeBackend simulates register-chunk, confirm-chunk, and the signed
// Storage PUT target, all on one httptest server — close enough to the
// real contract (verified against a live local Supabase instance when
// designing register-chunk/confirm-chunk) to exercise upload.Uploader's
// actual HTTP calls, not just its internal logic.
type fakeBackend struct {
	mu sync.Mutex

	registerCalls []map[string]any
	confirmCalls  []map[string]any
	objects       map[string][]byte // objectKey -> uploaded (compressed) bytes
	putAttempts   map[string]int

	// failFirstPutFor, if set to an objectKey, makes the first PUT to it
	// fail (simulating an expired/invalid signed URL); any retry succeeds.
	failFirstPutFor string
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		objects:     map[string][]byte{},
		putAttempts: map[string]int{},
	}
}

// storagePathPrefix mirrors the real path api.Client.StorageUploadURL
// builds (storage.go/chunk.go: /storage/v1/object/upload/sign/<bucket>/)
// — this fake must match it exactly, since the real client is what
// constructs the PUT URL, not anything this fake's register-chunk
// response returns (see RegisterChunkResult's doc comment for why).
const storagePathPrefix = "/storage/v1/object/upload/sign/carryon-sessions/"

func (b *fakeBackend) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/functions/v1/register-chunk", b.handleRegister)
	mux.HandleFunc("/functions/v1/confirm-chunk", b.handleConfirm)
	mux.HandleFunc(storagePathPrefix, b.handlePut)
	return httptest.NewServer(mux)
}

func (b *fakeBackend) handleRegister(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)

	b.mu.Lock()
	b.registerCalls = append(b.registerCalls, body)
	b.mu.Unlock()

	objectKey := fmt.Sprintf("%v/%v/%v/chunk-%v.jsonl.zst", body["projectId"], body["streamId"], body["sessionId"], body["seq"])
	_ = json.NewEncoder(w).Encode(map[string]string{
		"uploadToken": "fake-token-for-" + objectKey,
		"objectKey":   objectKey,
	})
}

func (b *fakeBackend) handleConfirm(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)

	b.mu.Lock()
	b.confirmCalls = append(b.confirmCalls, body)
	b.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (b *fakeBackend) handlePut(w http.ResponseWriter, r *http.Request) {
	objectKey := strings.TrimPrefix(r.URL.Path, storagePathPrefix)

	b.mu.Lock()
	b.putAttempts[objectKey]++
	shouldFail := b.failFirstPutFor == objectKey && b.putAttempts[objectKey] == 1
	b.mu.Unlock()

	if shouldFail {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	b.mu.Lock()
	b.objects[objectKey] = data
	b.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (b *fakeBackend) registerCallCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.registerCalls)
}

func (b *fakeBackend) confirmCallCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.confirmCalls)
}

func fakeTokenSource(_ context.Context) (string, error) {
	return "fake-token", nil
}

func newTestUploader(t *testing.T, srv *httptest.Server) *Uploader {
	t.Helper()
	client := api.NewClient(srv.URL, "anon-key")
	u, err := New(Config{
		ClientSource: func() (*api.Client, error) { return client, nil },
		TokenSource:  fakeTokenSource,
		StateDir:     filepath.Join(t.TempDir(), "state"),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return u
}

func writeTranscript(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("write %s: %v", path, err)
	}
}

func decompress(t *testing.T, data []byte) []byte {
	t.Helper()
	dec, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatalf("zstd.NewReader: %v", err)
	}
	defer dec.Close()
	out, err := dec.DecodeAll(data, nil)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	return out
}

func TestUploadRequiresProjectID(t *testing.T) {
	backend := newFakeBackend()
	srv := backend.server()
	defer srv.Close()
	u := newTestUploader(t, srv)

	err := u.Upload(context.Background(), agent.Session{ID: "s1"}) // no ProjectID
	if err == nil {
		t.Fatal("Upload() with no ProjectID = nil error, want an error")
	}
}

func TestUploadOneChunkEndToEnd(t *testing.T) {
	backend := newFakeBackend()
	srv := backend.server()
	defer srv.Close()
	u := newTestUploader(t, srv)

	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "s1.jsonl")
	content := `{"line":1}` + "\n" + `{"line":2}` + "\n"
	writeTranscript(t, transcriptPath, content)

	session := agent.Session{ID: "s1", Cwd: "/repo", TranscriptPath: transcriptPath, ProjectID: "p1"}
	if err := u.Upload(context.Background(), session); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	if backend.registerCallCount() != 1 {
		t.Fatalf("register-chunk called %d times, want 1", backend.registerCallCount())
	}
	if backend.confirmCallCount() != 1 {
		t.Fatalf("confirm-chunk called %d times, want 1", backend.confirmCallCount())
	}

	reg := backend.registerCalls[0]
	if reg["sessionId"] != "s1" || reg["projectId"] != "p1" || reg["streamId"] != "main" {
		t.Errorf("register-chunk request = %+v", reg)
	}
	if reg["seq"] != float64(0) {
		t.Errorf("seq = %v, want 0", reg["seq"])
	}

	// Exactly one object was uploaded, and it decompresses back to the
	// original transcript content, byte-identical.
	if len(backend.objects) != 1 {
		t.Fatalf("got %d uploaded objects, want 1", len(backend.objects))
	}
	for _, compressed := range backend.objects {
		got := decompress(t, compressed)
		if string(got) != content {
			t.Errorf("rebuilt content = %q, want %q", got, content)
		}
		sum := sha256.Sum256(got)
		if hex.EncodeToString(sum[:]) != reg["sha256"] {
			t.Errorf("uploaded content's sha256 doesn't match what register-chunk was told")
		}
	}

	st, err := state.Load(u.stateDir, "s1")
	if err != nil {
		t.Fatalf("state.Load() error = %v", err)
	}
	if st.Offset != int64(len(content)) {
		t.Errorf("state.Offset = %d, want %d", st.Offset, len(content))
	}
	if st.NextSeq != 1 {
		t.Errorf("state.NextSeq = %d, want 1", st.NextSeq)
	}
}

func TestUploadSecondCallWithNoNewDataIsANoOp(t *testing.T) {
	backend := newFakeBackend()
	srv := backend.server()
	defer srv.Close()
	u := newTestUploader(t, srv)

	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "s1.jsonl")
	writeTranscript(t, transcriptPath, `{"line":1}`+"\n")

	session := agent.Session{ID: "s1", TranscriptPath: transcriptPath, ProjectID: "p1"}
	if err := u.Upload(context.Background(), session); err != nil {
		t.Fatalf("first Upload() error = %v", err)
	}
	if err := u.Upload(context.Background(), session); err != nil {
		t.Fatalf("second Upload() error = %v", err)
	}

	if backend.registerCallCount() != 1 {
		t.Errorf("register-chunk called %d times across two uploads with no new data, want 1", backend.registerCallCount())
	}
}

func TestUploadContinuesFromPriorSeqOnNewData(t *testing.T) {
	backend := newFakeBackend()
	srv := backend.server()
	defer srv.Close()
	u := newTestUploader(t, srv)

	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "s1.jsonl")
	writeTranscript(t, transcriptPath, `{"line":1}`+"\n")

	session := agent.Session{ID: "s1", TranscriptPath: transcriptPath, ProjectID: "p1"}
	if err := u.Upload(context.Background(), session); err != nil {
		t.Fatalf("first Upload() error = %v", err)
	}

	f, err := os.OpenFile(transcriptPath, os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304,G302 -- test fixture
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString(`{"line":2}` + "\n"); err != nil {
		t.Fatalf("append: %v", err)
	}
	_ = f.Close()

	if err := u.Upload(context.Background(), session); err != nil {
		t.Fatalf("second Upload() error = %v", err)
	}

	if backend.registerCallCount() != 2 {
		t.Fatalf("register-chunk called %d times, want 2", backend.registerCallCount())
	}
	if backend.registerCalls[1]["seq"] != float64(1) {
		t.Errorf("second chunk's seq = %v, want 1", backend.registerCalls[1]["seq"])
	}
}

func TestUploadSplitsLargeSliceIntoContiguous8MBChunks(t *testing.T) {
	backend := newFakeBackend()
	srv := backend.server()
	defer srv.Close()
	u := newTestUploader(t, srv)

	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "s1.jsonl")

	// 40MB of content, well above the 8MB split threshold, as whole lines.
	line := `{"pad":"` + strings.Repeat("x", 998) + `"}` + "\n" // 1010 bytes/line
	var sb strings.Builder
	for sb.Len() < 40*1024*1024 {
		sb.WriteString(line)
	}
	content := sb.String()
	writeTranscript(t, transcriptPath, content)

	session := agent.Session{ID: "s1", TranscriptPath: transcriptPath, ProjectID: "p1"}
	if err := u.Upload(context.Background(), session); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	wantChunks := (len(content) + maxUncompressedChunkBytes - 1) / maxUncompressedChunkBytes
	if backend.registerCallCount() != wantChunks {
		t.Fatalf("got %d chunks, want %d", backend.registerCallCount(), wantChunks)
	}

	var prevEnd int64
	var rebuilt bytes.Buffer
	for i, reg := range backend.registerCalls {
		start := int64(reg["byteStart"].(float64))
		end := int64(reg["byteEnd"].(float64))
		if start != prevEnd {
			t.Errorf("chunk %d: byteStart = %d, want contiguous with previous end %d", i, start, prevEnd)
		}
		if size := end - start; size > maxUncompressedChunkBytes {
			t.Errorf("chunk %d: size %d exceeds the %d-byte split threshold", i, size, maxUncompressedChunkBytes)
		}
		prevEnd = end

		objectKey := reg["projectId"].(string) + "/" + reg["streamId"].(string) + "/" + reg["sessionId"].(string) + "/chunk-" + fmt.Sprint(int(reg["seq"].(float64))) + ".jsonl.zst"
		rebuilt.Write(decompress(t, backend.objects[objectKey]))
	}

	if rebuilt.String() != content {
		t.Error("rebuilding all chunks in seq order didn't reproduce the original content byte-for-byte")
	}
}

func TestUploadRetriesOnceWithFreshURLWhenPutFails(t *testing.T) {
	backend := newFakeBackend()
	srv := backend.server()
	defer srv.Close()
	u := newTestUploader(t, srv)

	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "s1.jsonl")
	writeTranscript(t, transcriptPath, `{"line":1}`+"\n")

	// The object key is deterministic (session/project/stream/seq), so we
	// can target the first PUT attempt for failure before it happens.
	backend.failFirstPutFor = "p1/main/s1/chunk-0.jsonl.zst"

	session := agent.Session{ID: "s1", TranscriptPath: transcriptPath, ProjectID: "p1"}
	if err := u.Upload(context.Background(), session); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	if backend.registerCallCount() != 2 {
		t.Errorf("register-chunk called %d times, want 2 (one re-registration after the failed PUT)", backend.registerCallCount())
	}
	if backend.confirmCallCount() != 1 {
		t.Errorf("confirm-chunk called %d times, want 1", backend.confirmCallCount())
	}
}

func TestUploadIncludesSubagentStreams(t *testing.T) {
	backend := newFakeBackend()
	srv := backend.server()
	defer srv.Close()
	u := newTestUploader(t, srv)

	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "s1.jsonl")
	writeTranscript(t, transcriptPath, `{"main":true}`+"\n")

	subagentDir := filepath.Join(dir, "s1", "subagents")
	if err := os.MkdirAll(subagentDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeTranscript(t, filepath.Join(subagentDir, "agent-a1.jsonl"), `{"sub":true}`+"\n")

	session := agent.Session{ID: "s1", TranscriptPath: transcriptPath, ProjectID: "p1"}
	if err := u.Upload(context.Background(), session); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	if backend.registerCallCount() != 2 {
		t.Fatalf("register-chunk called %d times, want 2 (main + 1 subagent)", backend.registerCallCount())
	}

	var streams []string
	for _, reg := range backend.registerCalls {
		streams = append(streams, reg["streamId"].(string))
	}
	hasMain, hasSub := false, false
	for _, s := range streams {
		if s == "main" {
			hasMain = true
		}
		if s == "a1" {
			hasSub = true
		}
	}
	if !hasMain || !hasSub {
		t.Errorf("stream IDs = %v, want both \"main\" and \"a1\"", streams)
	}

	// The subagent's state is tracked separately from the main session's.
	mainState, err := state.Load(u.stateDir, "s1")
	if err != nil {
		t.Fatalf("state.Load(main) error = %v", err)
	}
	subState, err := state.Load(u.stateDir, state.SubagentKey("s1", "a1"))
	if err != nil {
		t.Fatalf("state.Load(subagent) error = %v", err)
	}
	if mainState.NextSeq != 1 || subState.NextSeq != 1 {
		t.Errorf("main.NextSeq=%d subagent.NextSeq=%d, want both 1 (independent counters)", mainState.NextSeq, subState.NextSeq)
	}
}

func TestNewValidatesRequiredFields(t *testing.T) {
	validClientSource := func() (*api.Client, error) { return api.NewClient("http://local", "key"), nil }

	if _, err := New(Config{}); err == nil {
		t.Error("New(Config{}) = nil error, want an error (missing required fields)")
	}
	if _, err := New(Config{ClientSource: validClientSource, TokenSource: fakeTokenSource}); err == nil {
		t.Error("New() without StateDir = nil error, want an error")
	}
	if _, err := New(Config{TokenSource: fakeTokenSource, StateDir: t.TempDir()}); err == nil {
		t.Error("New() without ClientSource = nil error, want an error")
	}
}

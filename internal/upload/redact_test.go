package upload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devminified/carryon/internal/agent"
)

func TestUploadRedactsASecretBeforeUploadingAndOffsetsStayOriginal(t *testing.T) {
	backend := newFakeBackend()
	srv := backend.server()
	defer srv.Close()
	u := newTestUploader(t, srv)

	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "s1.jsonl")
	line := `{"type":"user","message":{"content":"my key is sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789"}}` + "\n"
	writeTranscript(t, transcriptPath, line)

	session := agent.Session{ID: "s1", TranscriptPath: transcriptPath, ProjectID: "p1"}
	if err := u.Upload(context.Background(), session); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	if backend.registerCallCount() != 1 {
		t.Fatalf("got %d register calls, want 1", backend.registerCallCount())
	}

	// byteStart/byteEnd describe the *original* file's range — the
	// whole (unredacted) line — even though what was actually uploaded
	// is shorter once the secret's replaced with a placeholder.
	reg := backend.registerCalls[0]
	byteStart := int64(reg["byteStart"].(float64))
	byteEnd := int64(reg["byteEnd"].(float64))
	if byteStart != 0 || byteEnd != int64(len(line)) {
		t.Errorf("byteStart/byteEnd = %d/%d, want 0/%d (the original line's own length)", byteStart, byteEnd, len(line))
	}

	var uploadedKey string
	for k := range backend.objects {
		uploadedKey = k
	}
	uploaded := decompress(t, backend.objects[uploadedKey])

	if strings.Contains(string(uploaded), "sk-ant-api03") {
		t.Errorf("uploaded bytes still contain the secret: %s", uploaded)
	}
	if !strings.Contains(string(uploaded), "[REDACTED:anthropic-api-key]") {
		t.Errorf("uploaded bytes missing the redaction placeholder: %s", uploaded)
	}

	if backend.confirmCallCount() != 1 {
		t.Fatalf("got %d confirm calls, want 1", backend.confirmCallCount())
	}
	confirm := backend.confirmCalls[0]
	if got := confirm["redactions"].(float64); got != 1 {
		t.Errorf("confirm-chunk redactions = %v, want 1", got)
	}

	// SHA256 and size (on the register call) must describe the
	// *uploaded* (redacted) bytes, not the original line.
	sum := sha256.Sum256(uploaded)
	wantSHA := hex.EncodeToString(sum[:])
	if confirm["sha256"].(string) != wantSHA {
		t.Errorf("confirm-chunk sha256 = %v, want %v (hash of the actually-uploaded bytes)", confirm["sha256"], wantSHA)
	}
	if gotSize := int64(reg["size"].(float64)); gotSize != int64(len(uploaded)) {
		t.Errorf("register-chunk size = %d, want %d (the uploaded, redacted length)", gotSize, len(uploaded))
	}
}

func TestUploadPersistsRedactPendingAcrossSeparateUploadCalls(t *testing.T) {
	backend := newFakeBackend()
	srv := backend.server()
	defer srv.Close()
	u := newTestUploader(t, srv)

	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "s1.jsonl")
	toolUse := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"/Users/ali/project/.env"}}]}}` + "\n"
	writeTranscript(t, transcriptPath, toolUse)

	session := agent.Session{ID: "s1", TranscriptPath: transcriptPath, ProjectID: "p1"}
	if err := u.Upload(context.Background(), session); err != nil {
		t.Fatalf("first Upload() error = %v", err)
	}

	// The tool_use's own content never matches a secret rule — nothing
	// should be redacted in this first call.
	first := backend.confirmCalls[0]
	if got := first["redactions"].(float64); got != 0 {
		t.Errorf("first confirm redactions = %v, want 0", got)
	}

	// Now the tool_result arrives — a *separate* Upload() call (a real
	// agent would do this on the next poll tick, possibly after a
	// restart), which must still remember the earlier tool_use's
	// denylisted path via the persisted pending state
	// (state.SaveRedactPending/LoadRedactPending), not just in-memory.
	toolResult := `{"type":"user","message":{"content":[{"tool_use_id":"toolu_1","type":"tool_result","content":"SECRET_KEY=abc123"}]}}` + "\n"
	appendToFile(t, transcriptPath, toolResult)

	if err := u.Upload(context.Background(), session); err != nil {
		t.Fatalf("second Upload() error = %v", err)
	}

	if backend.registerCallCount() != 2 {
		t.Fatalf("got %d register calls, want 2", backend.registerCallCount())
	}
	// Find the second chunk's uploaded bytes (seq 1).
	var secondUploaded []byte
	for k, v := range backend.objects {
		if strings.Contains(k, "chunk-1.") {
			secondUploaded = decompress(t, v)
		}
	}
	if secondUploaded == nil {
		t.Fatal("couldn't find the second chunk's uploaded object")
	}
	if strings.Contains(string(secondUploaded), "SECRET_KEY") || strings.Contains(string(secondUploaded), "abc123") {
		t.Errorf("denylisted content leaked across separate Upload() calls: %s", secondUploaded)
	}
	if !strings.Contains(string(secondUploaded), "[REDACTED:denylisted-file .env]") {
		t.Errorf("missing denylist placeholder: %s", secondUploaded)
	}
}

func appendToFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- test fixture
	if err != nil {
		t.Fatalf("opening %s to append: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("appending to %s: %v", path, err)
	}
}

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegisterChunkSuccess(t *testing.T) {
	var gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(RegisterChunkResult{
			UploadToken: "sometoken",
			ObjectKey:   "p1/u1/s1/chunk-0.jsonl.zst",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	got, err := c.RegisterChunk(context.Background(), "access-token", RegisterChunkInput{
		SessionID:         "s1",
		ProjectID:         "p1",
		OriginalCwd:       "/repo",
		Machine:           "alis-macbook",
		Branch:            "main",
		HeadSHA:           "deadbeef",
		ClaudeCodeVersion: "1.2.3",
		Seq:               0,
		ByteStart:         0,
		ByteEnd:           100,
		SHA256:            "abc123",
		Size:              100,
	})
	if err != nil {
		t.Fatalf("RegisterChunk() error = %v", err)
	}

	if gotPath != "/functions/v1/register-chunk" {
		t.Errorf("path = %q, want /functions/v1/register-chunk", gotPath)
	}
	if gotBody["sessionId"] != "s1" || gotBody["projectId"] != "p1" {
		t.Errorf("request body = %+v", gotBody)
	}
	if gotBody["machine"] != "alis-macbook" || gotBody["branch"] != "main" ||
		gotBody["headSha"] != "deadbeef" || gotBody["claudeCodeVersion"] != "1.2.3" {
		t.Errorf("request body missing CO-111 metadata fields: %+v", gotBody)
	}
	if got.UploadToken != "sometoken" {
		t.Errorf("UploadToken = %q", got.UploadToken)
	}
	if got.ObjectKey != "p1/u1/s1/chunk-0.jsonl.zst" {
		t.Errorf("ObjectKey = %q", got.ObjectKey)
	}
}

func TestRegisterChunkOmitsEmptyMetadataFields(t *testing.T) {
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(RegisterChunkResult{})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	_, err := c.RegisterChunk(context.Background(), "access-token", RegisterChunkInput{
		SessionID: "s1",
		ProjectID: "p1",
	})
	if err != nil {
		t.Fatalf("RegisterChunk() error = %v", err)
	}

	for _, field := range []string{"machine", "branch", "headSha", "claudeCodeVersion"} {
		if _, present := gotBody[field]; present {
			t.Errorf("request body has %q = %v, want it omitted when gathering failed", field, gotBody[field])
		}
	}
}

func TestRegisterChunkForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "forbidden"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	_, err := c.RegisterChunk(context.Background(), "access-token", RegisterChunkInput{SessionID: "s1", ProjectID: "p1"})
	if !IsForbidden(err) {
		t.Errorf("IsForbidden(err) = false for %v, want true", err)
	}
}

func TestConfirmChunkSuccess(t *testing.T) {
	var gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	err := c.ConfirmChunk(context.Background(), "access-token", ConfirmChunkInput{
		SessionID: "s1",
		Seq:       0,
		ByteStart: 0,
		ByteEnd:   100,
		SHA256:    "abc123",
	})
	if err != nil {
		t.Fatalf("ConfirmChunk() error = %v", err)
	}

	if gotPath != "/functions/v1/confirm-chunk" {
		t.Errorf("path = %q, want /functions/v1/confirm-chunk", gotPath)
	}
	if gotBody["sessionId"] != "s1" {
		t.Errorf("request body = %+v", gotBody)
	}
}

func TestConfirmChunkObjectMissingIsConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "object_missing", Message: "not found in storage"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	err := c.ConfirmChunk(context.Background(), "access-token", ConfirmChunkInput{SessionID: "s1", Seq: 0})
	if !IsConflict(err) {
		t.Errorf("IsConflict(err) = false for %v, want true", err)
	}
}

func TestStorageUploadURLUsesTheClientsOwnBaseURL(t *testing.T) {
	// The whole point: never trust a host embedded in an Edge Function's
	// own signed-URL string (it can be Docker-internal in local dev) —
	// always build from the client's own, already-correct base URL.
	c := NewClient("http://127.0.0.1:54421", "anon-key")
	got := c.StorageUploadURL("p1/main/s1/chunk-0.jsonl.zst", "tok123")
	want := "http://127.0.0.1:54421/storage/v1/object/upload/sign/carryon-sessions/p1/main/s1/chunk-0.jsonl.zst?token=tok123"
	if got != want {
		t.Errorf("StorageUploadURL() = %q, want %q", got, want)
	}
}

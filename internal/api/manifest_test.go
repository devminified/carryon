package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetManifestParsesARefSnapshot(t *testing.T) {
	var gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session": map[string]any{
				"id": "s1", "projectId": "p1", "projectName": "acme-api",
				"gitRemote": "github.com/acme/acme-api", "ownerLogin": "ali",
				"originalCwd": "/Users/ali/acme-api", "branch": "main",
				"headSha": "a1", "claudeCodeVersion": "2.1.0", "status": "live",
				"lastActivityAt": "2026-01-01T18:42:00Z",
			},
			"chunks": []map[string]any{
				{"streamId": "main", "seq": 0, "byteStart": 0, "byteEnd": 100, "sha256": "c1", "objectKey": "ok1", "downloadToken": "dt1"},
			},
			"snapshot": map[string]any{
				"seq": 0, "headSha": "a1", "treeSha": "t1", "wipRef": "refs/carryon/ali/s1",
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	got, err := c.GetManifest(context.Background(), "access-token", "s1")
	if err != nil {
		t.Fatalf("GetManifest() error = %v", err)
	}

	if gotPath != "/functions/v1/get-manifest" {
		t.Errorf("path = %q, want /functions/v1/get-manifest", gotPath)
	}
	if gotBody["sessionId"] != "s1" {
		t.Errorf("request body = %+v", gotBody)
	}

	if got.Session.OwnerLogin != "ali" || got.Session.Branch != "main" {
		t.Errorf("Session = %+v", got.Session)
	}
	if got.Session.ProjectName != "acme-api" || got.Session.GitRemote != "github.com/acme/acme-api" {
		t.Errorf("Session = %+v, want ProjectName/GitRemote populated", got.Session)
	}
	wantLastActivity := time.Date(2026, 1, 1, 18, 42, 0, 0, time.UTC)
	if !got.Session.LastActivityAt.Equal(wantLastActivity) {
		t.Errorf("Session.LastActivityAt = %v, want %v", got.Session.LastActivityAt, wantLastActivity)
	}
	if len(got.Chunks) != 1 || got.Chunks[0].ObjectKey != "ok1" || got.Chunks[0].DownloadToken != "dt1" {
		t.Errorf("Chunks = %+v", got.Chunks)
	}
	if got.Snapshot == nil || got.Snapshot.WipRef != "refs/carryon/ali/s1" || got.Snapshot.ObjectKey != "" {
		t.Errorf("Snapshot = %+v", got.Snapshot)
	}
}

func TestGetManifestParsesABundleSnapshot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session": map[string]any{"id": "s1"},
			"chunks":  []map[string]any{},
			"snapshot": map[string]any{
				"seq": 1, "headSha": "a1", "treeSha": "t1",
				"objectKey": "p1/u1/s1/code-1.bundle", "downloadToken": "dt2",
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	got, err := c.GetManifest(context.Background(), "access-token", "s1")
	if err != nil {
		t.Fatalf("GetManifest() error = %v", err)
	}
	if got.Snapshot == nil || got.Snapshot.WipRef != "" || got.Snapshot.ObjectKey != "p1/u1/s1/code-1.bundle" {
		t.Errorf("Snapshot = %+v", got.Snapshot)
	}
}

func TestGetManifestParsesANilSnapshot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session": map[string]any{"id": "s1"}, "chunks": []map[string]any{}, "snapshot": nil,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	got, err := c.GetManifest(context.Background(), "access-token", "s1")
	if err != nil {
		t.Fatalf("GetManifest() error = %v", err)
	}
	if got.Snapshot != nil {
		t.Errorf("Snapshot = %+v, want nil", got.Snapshot)
	}
}

func TestGetManifestNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "not_found"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	_, err := c.GetManifest(context.Background(), "access-token", "missing")
	if !IsNotFound(err) {
		t.Errorf("IsNotFound(err) = false for %v, want true", err)
	}
}

func TestGetManifestForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "forbidden"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	_, err := c.GetManifest(context.Background(), "access-token", "s1")
	if !IsForbidden(err) {
		t.Errorf("IsForbidden(err) = false for %v, want true", err)
	}
}

func TestStorageDownloadURL(t *testing.T) {
	c := NewClient("https://xyz.supabase.co", "anon-key")
	got := c.StorageDownloadURL("carryon-sessions", "p1/u1/s1/main/chunk-0.jsonl.zst", "tok with spaces")
	want := "https://xyz.supabase.co/storage/v1/object/sign/carryon-sessions/p1/u1/s1/main/chunk-0.jsonl.zst?token=tok+with+spaces"
	if got != want {
		t.Errorf("StorageDownloadURL() = %q, want %q", got, want)
	}
}

func TestStorageDownloadURLUsesAProjectsDedicatedBucket(t *testing.T) {
	c := NewClient("https://xyz.supabase.co", "anon-key")
	got := c.StorageDownloadURL("client-x-bucket", "p1/u1/s1/main/chunk-0.jsonl.zst", "tok")
	want := "https://xyz.supabase.co/storage/v1/object/sign/client-x-bucket/p1/u1/s1/main/chunk-0.jsonl.zst?token=tok"
	if got != want {
		t.Errorf("StorageDownloadURL() = %q, want %q", got, want)
	}
}

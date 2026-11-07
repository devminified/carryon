package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegisterSnapshotSuccess(t *testing.T) {
	var gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	err := c.RegisterSnapshot(context.Background(), "access-token", RegisterSnapshotInput{
		SessionID: "s1",
		Seq:       2,
		HeadSHA:   "deadbeef",
		WipRef:    "refs/carryon/ali/s1",
		TreeSHA:   "cafef00d",
	})
	if err != nil {
		t.Fatalf("RegisterSnapshot() error = %v", err)
	}

	if gotPath != "/functions/v1/register-snapshot" {
		t.Errorf("path = %q, want /functions/v1/register-snapshot", gotPath)
	}
	if gotBody["sessionId"] != "s1" || gotBody["headSha"] != "deadbeef" || gotBody["wipRef"] != "refs/carryon/ali/s1" || gotBody["treeSha"] != "cafef00d" {
		t.Errorf("request body = %+v", gotBody)
	}
}

func TestRegisterSnapshotOmitsWipRefOnTheBundlePath(t *testing.T) {
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	err := c.RegisterSnapshot(context.Background(), "access-token", RegisterSnapshotInput{
		SessionID: "s1",
		Seq:       0,
		HeadSHA:   "deadbeef",
		TreeSHA:   "cafef00d",
		// WipRef deliberately left empty — the bundle fallback path.
	})
	if err != nil {
		t.Fatalf("RegisterSnapshot() error = %v", err)
	}

	if _, present := gotBody["wipRef"]; present {
		t.Errorf("request body has wipRef = %v, want it omitted entirely", gotBody["wipRef"])
	}
}

func TestSignSnapshotBundleSuccess(t *testing.T) {
	var gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(SignSnapshotBundleResult{
			UploadToken: "sometoken",
			ObjectKey:   "p1/u1/s1/code-0.bundle",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	got, err := c.SignSnapshotBundle(context.Background(), "access-token", SignSnapshotBundleInput{
		SessionID: "s1",
		Seq:       0,
	})
	if err != nil {
		t.Fatalf("SignSnapshotBundle() error = %v", err)
	}

	if gotPath != "/functions/v1/sign-snapshot-bundle" {
		t.Errorf("path = %q, want /functions/v1/sign-snapshot-bundle", gotPath)
	}
	if gotBody["sessionId"] != "s1" {
		t.Errorf("request body = %+v", gotBody)
	}
	if got.UploadToken != "sometoken" || got.ObjectKey != "p1/u1/s1/code-0.bundle" {
		t.Errorf("result = %+v", got)
	}
}

func TestSignSnapshotBundleForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "forbidden"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	_, err := c.SignSnapshotBundle(context.Background(), "access-token", SignSnapshotBundleInput{SessionID: "s1"})
	if !IsForbidden(err) {
		t.Errorf("IsForbidden(err) = false for %v, want true", err)
	}
}

func TestRegisterSnapshotForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "forbidden"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	err := c.RegisterSnapshot(context.Background(), "access-token", RegisterSnapshotInput{SessionID: "s1"})
	if !IsForbidden(err) {
		t.Errorf("IsForbidden(err) = false for %v, want true", err)
	}
}

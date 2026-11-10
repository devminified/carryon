package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHeartbeatSuccess(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	lastUpload := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	c := NewClient(srv.URL, "anon-key")
	err := c.Heartbeat(context.Background(), "access-token", HeartbeatInput{
		MachineID:         "machine-1",
		Hostname:          "alis-laptop",
		Version:           "1.2.3",
		OS:                "darwin",
		BlockedSessions:   4,
		BytesBehind:       1536,
		LastUploadAt:      &lastUpload,
		UploadAttempts:    10,
		UploadErrors:      1,
		RedactionsApplied: 3,
	})
	if err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}

	if gotPath != "/functions/v1/heartbeat" {
		t.Errorf("path = %q, want /functions/v1/heartbeat", gotPath)
	}
	if gotAuth != "Bearer access-token" {
		t.Errorf("Authorization header = %q", gotAuth)
	}
	if gotBody["machineId"] != "machine-1" || gotBody["hostname"] != "alis-laptop" ||
		gotBody["version"] != "1.2.3" || gotBody["blockedSessions"] != float64(4) {
		t.Errorf("request body = %+v", gotBody)
	}
	if gotBody["os"] != "darwin" {
		t.Errorf("os = %v, want darwin", gotBody["os"])
	}
	if gotBody["bytesBehind"] != float64(1536) {
		t.Errorf("bytesBehind = %v, want 1536", gotBody["bytesBehind"])
	}
	if gotBody["lastUploadAt"] != "2026-11-10T12:00:00Z" {
		t.Errorf("lastUploadAt = %v, want 2026-11-10T12:00:00Z", gotBody["lastUploadAt"])
	}
	if gotBody["uploadAttempts"] != float64(10) || gotBody["uploadErrors"] != float64(1) || gotBody["redactionsApplied"] != float64(3) {
		t.Errorf("counts in request body = %+v", gotBody)
	}
}

func TestHeartbeatOmitsLastUploadAtWhenNil(t *testing.T) {
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	err := c.Heartbeat(context.Background(), "access-token", HeartbeatInput{
		MachineID: "machine-1",
		Hostname:  "host",
		Version:   "1.0.0",
		OS:        "linux",
	})
	if err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if _, ok := gotBody["lastUploadAt"]; ok {
		t.Errorf("request body has lastUploadAt = %v, want it omitted when nil", gotBody["lastUploadAt"])
	}
}

func TestHeartbeatSurfacesAnErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "unauthorized"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	err := c.Heartbeat(context.Background(), "bad-token", HeartbeatInput{MachineID: "machine-1", Hostname: "host", Version: "1.0.0", OS: "linux"})
	if err == nil {
		t.Fatal("Heartbeat() with a rejected token = nil error, want an error")
	}
}

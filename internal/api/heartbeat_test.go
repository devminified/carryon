package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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

	c := NewClient(srv.URL, "anon-key")
	err := c.Heartbeat(context.Background(), "access-token", "machine-1", "alis-laptop", "1.2.3", 4)
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
}

func TestHeartbeatSurfacesAnErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "unauthorized"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	err := c.Heartbeat(context.Background(), "bad-token", "machine-1", "host", "1.0.0", 0)
	if err == nil {
		t.Fatal("Heartbeat() with a rejected token = nil error, want an error")
	}
}

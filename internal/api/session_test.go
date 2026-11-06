package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEndSessionSuccess(t *testing.T) {
	var gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	err := c.EndSession(context.Background(), "access-token", "s1")
	if err != nil {
		t.Fatalf("EndSession() error = %v", err)
	}

	if gotPath != "/functions/v1/end-session" {
		t.Errorf("path = %q, want /functions/v1/end-session", gotPath)
	}
	if gotBody["sessionId"] != "s1" {
		t.Errorf("request body = %+v", gotBody)
	}
}

func TestEndSessionForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "forbidden"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	err := c.EndSession(context.Background(), "access-token", "s1")
	if !IsForbidden(err) {
		t.Errorf("IsForbidden(err) = false for %v, want true", err)
	}
}

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegisterForkSuccess(t *testing.T) {
	var gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]string{"sessionId": "new-id"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	got, err := c.RegisterFork(context.Background(), "access-token", RegisterForkInput{
		ParentSessionID: "parent-id",
		NewSessionID:    "new-id",
	})
	if err != nil {
		t.Fatalf("RegisterFork() error = %v", err)
	}

	if gotPath != "/functions/v1/register-fork" {
		t.Errorf("path = %q, want /functions/v1/register-fork", gotPath)
	}
	if gotBody["parentSessionId"] != "parent-id" || gotBody["newSessionId"] != "new-id" {
		t.Errorf("request body = %+v", gotBody)
	}
	if gotBody["puller"] != nil {
		t.Errorf("request body has a puller field = %v, want it never sent", gotBody["puller"])
	}
	if got.SessionID != "new-id" {
		t.Errorf("SessionID = %q, want new-id", got.SessionID)
	}
}

func TestRegisterForkNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "not_found"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	_, err := c.RegisterFork(context.Background(), "access-token", RegisterForkInput{
		ParentSessionID: "missing",
		NewSessionID:    "new-id",
	})
	if !IsNotFound(err) {
		t.Errorf("IsNotFound(err) = false for %v, want true", err)
	}
}

func TestRegisterForkForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "forbidden"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	_, err := c.RegisterFork(context.Background(), "access-token", RegisterForkInput{
		ParentSessionID: "parent-id",
		NewSessionID:    "new-id",
	})
	if !IsForbidden(err) {
		t.Errorf("IsForbidden(err) = false for %v, want true", err)
	}
}

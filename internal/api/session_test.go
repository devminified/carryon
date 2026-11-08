package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestConfirmPullSuccess(t *testing.T) {
	var gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	err := c.ConfirmPull(context.Background(), "access-token", "s1")
	if err != nil {
		t.Fatalf("ConfirmPull() error = %v", err)
	}

	if gotPath != "/functions/v1/confirm-pull" {
		t.Errorf("path = %q, want /functions/v1/confirm-pull", gotPath)
	}
	if gotBody["sessionId"] != "s1" {
		t.Errorf("request body = %+v", gotBody)
	}
}

func TestConfirmPullForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "forbidden"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	err := c.ConfirmPull(context.Background(), "access-token", "s1")
	if !IsForbidden(err) {
		t.Errorf("IsForbidden(err) = false for %v, want true", err)
	}
}

func TestDeleteSessionSuccess(t *testing.T) {
	var gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	err := c.DeleteSession(context.Background(), "access-token", "s1", "developer left the team")
	if err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}

	if gotPath != "/functions/v1/delete-session" {
		t.Errorf("path = %q, want /functions/v1/delete-session", gotPath)
	}
	if gotBody["sessionId"] != "s1" || gotBody["reason"] != "developer left the team" {
		t.Errorf("request body = %+v", gotBody)
	}
}

func TestDeleteSessionForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "forbidden"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	err := c.DeleteSession(context.Background(), "access-token", "s1", "reason")
	if !IsForbidden(err) {
		t.Errorf("IsForbidden(err) = false for %v, want true", err)
	}
}

func TestDeleteSessionNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "not_found"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	err := c.DeleteSession(context.Background(), "access-token", "missing", "reason")
	if !IsNotFound(err) {
		t.Errorf("IsNotFound(err) = false for %v, want true", err)
	}
}

func TestDeleteSessionMissingReasonIsBadRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "validation_failed", Message: "reason: too small"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	err := c.DeleteSession(context.Background(), "access-token", "s1", "")
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 400 {
		t.Errorf("DeleteSession() error = %v, want a 400 *Error", err)
	}
}

func TestListSessionsSendsOnlyNonZeroFilters(t *testing.T) {
	var gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"sessions": []any{}})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	_, err := c.ListSessions(context.Background(), "access-token", ListSessionsInput{})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}

	if gotPath != "/functions/v1/list-sessions" {
		t.Errorf("path = %q, want /functions/v1/list-sessions", gotPath)
	}
	for _, key := range []string{"project", "dev", "status", "since", "limit"} {
		if _, ok := gotBody[key]; ok {
			t.Errorf("request body has unset field %q = %v, want it omitted", key, gotBody[key])
		}
	}
}

func TestListSessionsSendsEveryGivenFilter(t *testing.T) {
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"sessions": []any{}})
	}))
	defer srv.Close()

	since := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	c := NewClient(srv.URL, "anon-key")
	_, err := c.ListSessions(context.Background(), "access-token", ListSessionsInput{
		Project: "acme-api",
		Dev:     "ali",
		Status:  "live",
		Since:   &since,
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}

	if gotBody["project"] != "acme-api" {
		t.Errorf("project = %v, want acme-api", gotBody["project"])
	}
	if gotBody["dev"] != "ali" {
		t.Errorf("dev = %v, want ali", gotBody["dev"])
	}
	if gotBody["status"] != "live" {
		t.Errorf("status = %v, want live", gotBody["status"])
	}
	if gotBody["since"] != "2026-11-01T00:00:00Z" {
		t.Errorf("since = %v, want 2026-11-01T00:00:00Z", gotBody["since"])
	}
	if gotBody["limit"] != float64(10) {
		t.Errorf("limit = %v, want 10", gotBody["limit"])
	}
}

func TestListSessionsParsesTheResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sessions": []map[string]any{
				{
					"id":             "s1",
					"projectId":      "p1",
					"projectName":    "acme-api",
					"ownerLogin":     "ali",
					"branch":         "main",
					"title":          "fix payments retry",
					"lastActivityAt": "2026-11-05T10:00:00Z",
					"status":         "live",
				},
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	got, err := c.ListSessions(context.Background(), "access-token", ListSessionsInput{})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	want := SessionSummary{
		ID: "s1", ProjectID: "p1", ProjectName: "acme-api", OwnerLogin: "ali",
		Branch: "main", Title: "fix payments retry",
		LastActivityAt: time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC),
		Status:         "live",
	}
	if got[0] != want {
		t.Errorf("got[0] = %+v, want %+v", got[0], want)
	}
}

func TestListSessionsForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "forbidden"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	_, err := c.ListSessions(context.Background(), "access-token", ListSessionsInput{})
	if !IsForbidden(err) {
		t.Errorf("IsForbidden(err) = false for %v, want true", err)
	}
}

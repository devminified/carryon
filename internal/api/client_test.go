package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegisterProjectSuccess(t *testing.T) {
	var gotPath, gotAuth, gotAPIKey string
	var gotBody map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("apikey")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Project{ID: "p1", Name: "acme-api", GitRemote: "github.com/devminified/acme-api"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	got, err := c.RegisterProject(context.Background(), "access-token", "acme-api", "github.com/devminified/acme-api")
	if err != nil {
		t.Fatalf("RegisterProject() error = %v", err)
	}

	if gotPath != "/functions/v1/register-project" {
		t.Errorf("path = %q, want /functions/v1/register-project", gotPath)
	}
	if gotAuth != "Bearer access-token" {
		t.Errorf("Authorization header = %q", gotAuth)
	}
	if gotAPIKey != "anon-key" {
		t.Errorf("apikey header = %q", gotAPIKey)
	}
	if gotBody["name"] != "acme-api" || gotBody["gitRemote"] != "github.com/devminified/acme-api" {
		t.Errorf("request body = %+v", gotBody)
	}
	if got.ID != "p1" {
		t.Errorf("Project.ID = %q, want p1", got.ID)
	}
}

func TestRegisterProjectConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "conflict", Message: "already registered"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	_, err := c.RegisterProject(context.Background(), "access-token", "acme-api", "github.com/devminified/acme-api")
	if err == nil {
		t.Fatal("RegisterProject() with a duplicate remote = nil error, want an error")
	}
	var apiErr *Error
	if ok := errors.As(err, &apiErr); !ok || apiErr.StatusCode != http.StatusConflict {
		t.Errorf("error = %v, want *Error with StatusCode 409", err)
	}
}

func TestMatchProjectNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "not_found"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	_, err := c.MatchProject(context.Background(), "access-token", "github.com/devminified/unregistered")
	if !IsNotFound(err) {
		t.Errorf("IsNotFound(err) = false for %v, want true", err)
	}
	if IsForbidden(err) {
		t.Error("IsForbidden(err) = true, want false")
	}
}

func TestMatchProjectForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "forbidden"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	_, err := c.MatchProject(context.Background(), "access-token", "github.com/devminified/acme-api")
	if !IsForbidden(err) {
		t.Errorf("IsForbidden(err) = false for %v, want true", err)
	}
}

func TestMatchProjectSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(MatchProjectResult{ProjectID: "p1", Role: "admin"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	got, err := c.MatchProject(context.Background(), "access-token", "github.com/devminified/acme-api")
	if err != nil {
		t.Fatalf("MatchProject() error = %v", err)
	}
	if got.ProjectID != "p1" || got.Role != "admin" {
		t.Errorf("MatchProject() = %+v", got)
	}
}

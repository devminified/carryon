package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetConfigParsesTheResponse(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"disabledRuleIds":   []string{"generic-api-key"},
			"denylistAdditions": []string{"*.secret"},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	got, err := c.GetConfig(context.Background(), "access-token")
	if err != nil {
		t.Fatalf("GetConfig() error = %v", err)
	}
	if gotPath != "/functions/v1/get-config" {
		t.Errorf("path = %q, want /functions/v1/get-config", gotPath)
	}
	if len(got.DisabledRuleIDs) != 1 || got.DisabledRuleIDs[0] != "generic-api-key" {
		t.Errorf("DisabledRuleIDs = %+v", got.DisabledRuleIDs)
	}
	if len(got.DenylistAdditions) != 1 || got.DenylistAdditions[0] != "*.secret" {
		t.Errorf("DenylistAdditions = %+v", got.DenylistAdditions)
	}
}

func TestGetConfigUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(errorResponse{Error: "unauthorized"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "anon-key")
	_, err := c.GetConfig(context.Background(), "bad-token")
	if err == nil {
		t.Fatal("GetConfig() error = nil, want an error")
	}
}

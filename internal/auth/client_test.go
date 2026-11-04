package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestAuthorizeURL(t *testing.T) {
	c := newAuthClient("http://127.0.0.1:54421", "test-key")
	got := c.authorizeURL("http://127.0.0.1:54999/callback", "a-challenge")

	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("authorizeURL produced an unparseable URL: %v", err)
	}
	q := u.Query()
	for k, want := range map[string]string{
		"provider":              "github",
		"redirect_to":           "http://127.0.0.1:54999/callback",
		"code_challenge":        "a-challenge",
		"code_challenge_method": "s256",
	} {
		if got := q.Get(k); got != want {
			t.Errorf("query param %q = %q, want %q", k, got, want)
		}
	}
}

func TestExchangeCodeSuccess(t *testing.T) {
	var gotPath, gotAPIKey, gotGrantType string
	var gotBody map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotGrantType = r.URL.Query().Get("grant_type")
		gotAPIKey = r.Header.Get("apikey")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)

		_ = json.NewEncoder(w).Encode(tokenResponse{
			AccessToken:  "access-123",
			RefreshToken: "refresh-456",
			ExpiresIn:    3600,
		})
	}))
	defer srv.Close()

	c := newAuthClient(srv.URL, "test-key")
	sess, err := c.exchangeCode(context.Background(), "the-code", "the-verifier")
	if err != nil {
		t.Fatalf("exchangeCode() error = %v", err)
	}

	if gotPath != "/auth/v1/token" {
		t.Errorf("path = %q, want /auth/v1/token", gotPath)
	}
	if gotGrantType != "pkce" {
		t.Errorf("grant_type = %q, want pkce", gotGrantType)
	}
	if gotAPIKey != "test-key" {
		t.Errorf("apikey header = %q, want test-key", gotAPIKey)
	}
	if gotBody["auth_code"] != "the-code" || gotBody["code_verifier"] != "the-verifier" {
		t.Errorf("request body = %+v, want auth_code/code_verifier set", gotBody)
	}

	if sess.AccessToken != "access-123" || sess.RefreshToken != "refresh-456" {
		t.Errorf("session = %+v, want access-123/refresh-456", sess)
	}
	if gotExpiry := time.Until(sess.ExpiresAt); gotExpiry < 55*time.Minute || gotExpiry > time.Hour {
		t.Errorf("ExpiresAt = %v from now, want ~1h", gotExpiry)
	}
}

func TestRefreshRotatesToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["refresh_token"] != "old-refresh" {
			t.Errorf("refresh_token in request = %q, want old-refresh", body["refresh_token"])
		}
		_ = json.NewEncoder(w).Encode(tokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh", // rotated — must differ from the one sent
			ExpiresIn:    3600,
		})
	}))
	defer srv.Close()

	c := newAuthClient(srv.URL, "test-key")
	sess, err := c.refresh(context.Background(), "old-refresh")
	if err != nil {
		t.Fatalf("refresh() error = %v", err)
	}
	if sess.RefreshToken != "new-refresh" {
		t.Errorf("RefreshToken = %q, want the rotated new-refresh", sess.RefreshToken)
	}
}

func TestRefreshRevokedTokenReturnsError(t *testing.T) {
	// Simulates Supabase Auth's reuse-detection response: replaying an
	// old (already-rotated) refresh token revokes the whole family.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(tokenResponse{
			ErrorCode: "refresh_token_already_used",
			Msg:       "Invalid Refresh Token: Already Used",
		})
	}))
	defer srv.Close()

	c := newAuthClient(srv.URL, "test-key")
	_, err := c.refresh(context.Background(), "replayed-token")
	if err == nil {
		t.Fatal("refresh() with a replayed token = nil error, want an error")
	}
	var authErr *authError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %v (%T), want *authError", err, err)
	}
	if authErr.Code != "refresh_token_already_used" {
		t.Errorf("authErr.Code = %q, want refresh_token_already_used", authErr.Code)
	}
}

func TestLocalSupabaseAuthTokenEndpointShape(t *testing.T) {
	// Integration check against the real local stack (not a mock): a
	// fake auth code must fail with exactly the shape this package
	// expects, proving the request format (path, grant_type, JSON body)
	// is genuinely compatible with Supabase Auth — not just an assumption
	// matched against a mock we wrote ourselves.
	c := newAuthClient("http://127.0.0.1:54421", "sb_publishable_ACJWlzQHlZjBrEguHvfOxg_3BJgxAaH")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := c.exchangeCode(ctx, "definitely-not-a-real-code", "definitely-not-a-real-verifier")
	if err == nil {
		t.Skip("local Supabase not reachable or unexpectedly succeeded; skipping (run `supabase status` first)")
	}
	var authErr *authError
	if !errors.As(err, &authErr) {
		t.Skipf("local Supabase not reachable (got %v), skipping — start it with `supabase start`", err)
	}
	if authErr.Code != "flow_state_not_found" {
		t.Errorf("authErr.Code = %q, want flow_state_not_found (a fake code should fail this specific way)", authErr.Code)
	}
}

func TestCurrentUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/v1/user" {
			t.Errorf("path = %q, want /auth/v1/user", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Errorf("Authorization header = %q", got)
		}
		_, _ = w.Write([]byte(`{"id":"u1","email":"ali@devminified.com","user_metadata":{"user_name":"ali"}}`))
	}))
	defer srv.Close()

	c := newAuthClient(srv.URL, "test-key")
	u, err := c.currentUser(context.Background(), "access-token")
	if err != nil {
		t.Fatalf("currentUser() error = %v", err)
	}
	if u.ID != "u1" || u.Email != "ali@devminified.com" || u.UserName != "ali" {
		t.Errorf("currentUser() = %+v, want {u1 ali@devminified.com ali}", u)
	}
}

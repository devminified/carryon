package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestProvider(t *testing.T, handler http.HandlerFunc) (*Provider, *fakeBackend) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	primary := &fakeBackend{}
	p := &Provider{
		client: newAuthClient(srv.URL, "test-key"),
		store:  &store{primary: primary, fallback: &fakeBackend{}},
	}
	return p, primary
}

func TestAccessTokenNotLoggedIn(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no request should be made when nothing is stored")
	})

	_, err := p.AccessToken(context.Background())
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("AccessToken() error = %v, want ErrNotLoggedIn", err)
	}
}

func TestAccessTokenRefreshesWhenStale(t *testing.T) {
	calls := 0
	p, primary := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(tokenResponse{
			AccessToken:  "fresh-access",
			RefreshToken: "fresh-refresh",
			ExpiresIn:    3600,
		})
	})
	primary.hasValue, primary.value = true, "stored-refresh"

	token, err := p.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken() error = %v", err)
	}
	if token != "fresh-access" {
		t.Errorf("token = %q, want fresh-access", token)
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 refresh call, got %d", calls)
	}
	if primary.value != "fresh-refresh" {
		t.Errorf("stored refresh token = %q, want the rotated fresh-refresh", primary.value)
	}
}

func TestAccessTokenUsesCacheWithoutNetworkCall(t *testing.T) {
	p, primary := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no network call expected when the cached token is still fresh")
	})
	primary.hasValue, primary.value = true, "stored-refresh"
	p.cached = &session{AccessToken: "cached-access", ExpiresAt: time.Now().Add(time.Hour)}

	token, err := p.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken() error = %v", err)
	}
	if token != "cached-access" {
		t.Errorf("token = %q, want cached-access (no refresh should have happened)", token)
	}
}

func TestAccessTokenRefreshesNearExpiry(t *testing.T) {
	// Within refreshSkew of expiry counts as stale, even though it
	// hasn't technically expired yet — proves the safety margin is honored.
	calls := 0
	p, primary := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: "new", RefreshToken: "new-r", ExpiresIn: 3600})
	})
	primary.hasValue, primary.value = true, "stored-refresh"
	p.cached = &session{AccessToken: "about-to-expire", ExpiresAt: time.Now().Add(refreshSkew / 2)}

	if _, err := p.AccessToken(context.Background()); err != nil {
		t.Fatalf("AccessToken() error = %v", err)
	}
	if calls != 1 {
		t.Errorf("expected a refresh when inside the skew window, got %d calls", calls)
	}
}

func TestAccessTokenClearsRevokedRefreshToken(t *testing.T) {
	p, primary := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(tokenResponse{ErrorCode: "refresh_token_already_used", Msg: "revoked"})
	})
	primary.hasValue, primary.value = true, "replayed-refresh"

	_, err := p.AccessToken(context.Background())
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("AccessToken() with a revoked refresh token error = %v, want ErrNotLoggedIn", err)
	}
	if primary.hasValue {
		t.Error("the revoked refresh token should have been cleared from storage")
	}
}

func TestLoggedIn(t *testing.T) {
	p, primary := newTestProvider(t, nil)

	if p.LoggedIn() {
		t.Error("LoggedIn() = true before anything is stored")
	}

	primary.hasValue, primary.value = true, "a-refresh-token"
	if !p.LoggedIn() {
		t.Error("LoggedIn() = false with a stored refresh token")
	}
}

func TestLogoutClearsStoreAndCache(t *testing.T) {
	p, primary := newTestProvider(t, nil)
	primary.hasValue, primary.value = true, "a-refresh-token"
	p.cached = &session{AccessToken: "x", ExpiresAt: time.Now().Add(time.Hour)}

	if err := p.Logout(); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if primary.hasValue {
		t.Error("Logout() should delete the stored refresh token")
	}
	if p.cached != nil {
		t.Error("Logout() should clear the in-memory cache")
	}
	if p.LoggedIn() {
		t.Error("LoggedIn() should be false after Logout()")
	}
}

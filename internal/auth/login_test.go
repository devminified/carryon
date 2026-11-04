package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestLoopbackServerReceivesCode(t *testing.T) {
	srv, err := startLoopbackServer()
	if err != nil {
		t.Fatalf("startLoopbackServer() error = %v", err)
	}
	defer srv.close()

	go func() {
		_, _ = http.Get("http://" + srv.addr + "/callback?code=the-code")
	}()

	code, err := srv.wait(context.Background())
	if err != nil {
		t.Fatalf("wait() error = %v", err)
	}
	if code != "the-code" {
		t.Errorf("code = %q, want the-code", code)
	}
}

func TestLoopbackServerReceivesError(t *testing.T) {
	srv, err := startLoopbackServer()
	if err != nil {
		t.Fatalf("startLoopbackServer() error = %v", err)
	}
	defer srv.close()

	go func() {
		_, _ = http.Get("http://" + srv.addr + "/callback?error=access_denied&error_description=user+cancelled")
	}()

	_, err = srv.wait(context.Background())
	if err == nil {
		t.Fatal("wait() with an error callback = nil error, want an error")
	}
}

func TestLoopbackServerTimesOut(t *testing.T) {
	srv, err := startLoopbackServer()
	if err != nil {
		t.Fatalf("startLoopbackServer() error = %v", err)
	}
	defer srv.close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if _, err := srv.wait(ctx); err == nil {
		t.Error("wait() with nothing hitting the callback = nil error, want a context deadline error")
	}
}

// fakeBrowser simulates a user completing GitHub/Supabase OAuth in their
// browser: it reads redirect_to out of the authorize URL Login() builds,
// then hits that URL itself with the given query additions — exactly what
// the real browser round trip ultimately delivers to the loopback server.
func fakeBrowser(t *testing.T, extraQuery string) func(string) error {
	t.Helper()
	return func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		redirectTo := u.Query().Get("redirect_to")
		go func() {
			_, _ = http.Get(redirectTo + "?" + extraQuery)
		}()
		return nil
	}
}

func TestLoginSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(tokenResponse{
			AccessToken:  "login-access",
			RefreshToken: "login-refresh",
			ExpiresIn:    3600,
		})
	}))
	defer srv.Close()

	orig := openBrowserFunc
	openBrowserFunc = fakeBrowser(t, "code=the-auth-code")
	defer func() { openBrowserFunc = orig }()

	primary := &fakeBackend{}
	p := &Provider{
		client: newAuthClient(srv.URL, "test-key"),
		store:  &store{primary: primary, fallback: &fakeBackend{}},
	}

	if err := p.Login(context.Background()); err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if !p.LoggedIn() {
		t.Error("LoggedIn() = false after a successful Login()")
	}
	if primary.value != "login-refresh" {
		t.Errorf("stored refresh token = %q, want login-refresh", primary.value)
	}

	token, err := p.AccessToken(context.Background())
	if err != nil || token != "login-access" {
		t.Errorf("AccessToken() = (%q, %v), want (login-access, nil) from the cached session Login() set", token, err)
	}
}

func TestLoginPropagatesBrowserError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("exchangeCode should never be called when the browser reported an error")
	}))
	defer srv.Close()

	orig := openBrowserFunc
	openBrowserFunc = fakeBrowser(t, "error=access_denied&error_description=user+cancelled")
	defer func() { openBrowserFunc = orig }()

	p := &Provider{
		client: newAuthClient(srv.URL, "test-key"),
		store:  &store{primary: &fakeBackend{}, fallback: &fakeBackend{}},
	}

	if err := p.Login(context.Background()); err == nil {
		t.Fatal("Login() with a browser-side cancel = nil error, want an error")
	}
	if p.LoggedIn() {
		t.Error("LoggedIn() should stay false after a failed Login()")
	}
}

func TestLoginTimesOutWhenNothingApproves(t *testing.T) {
	orig := openBrowserFunc
	openBrowserFunc = func(string) error { return nil } // "opens" but nothing ever hits the callback
	defer func() { openBrowserFunc = orig }()

	p := &Provider{
		client: newAuthClient("http://unused.invalid", "test-key"),
		store:  &store{primary: &fakeBackend{}, fallback: &fakeBackend{}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if err := p.Login(ctx); err == nil {
		t.Error("Login() with nothing approving = nil error, want a timeout error")
	}
}

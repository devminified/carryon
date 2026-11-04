package auth

import (
	"context"
	"fmt"
	"time"
)

// loginTimeout bounds how long Login waits for the browser round trip.
const loginTimeout = 5 * time.Minute

// openBrowserFunc is swappable so tests can simulate "the user approved in
// their browser" without actually launching one.
var openBrowserFunc = openBrowser

// Login runs the loopback PKCE flow end to end: starts a local callback
// server, opens the browser to Supabase Auth's GitHub sign-in, waits for
// the redirect, exchanges the code for a session, and stores the refresh
// token. On success the Provider is immediately usable via AccessToken.
func (p *Provider) Login(ctx context.Context) error {
	pair, err := newPKCEPair()
	if err != nil {
		return err
	}

	srv, err := startLoopbackServer()
	if err != nil {
		return err
	}
	defer srv.close()

	redirectTo := "http://" + srv.addr + "/callback"
	authURL := p.client.authorizeURL(redirectTo, pair.challenge)

	fmt.Println("Opening your browser to sign in with GitHub…")
	fmt.Println(authURL)
	if err := openBrowserFunc(authURL); err != nil {
		fmt.Println("Could not open a browser automatically — visit the URL above to continue.")
	}

	waitCtx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()

	code, err := srv.wait(waitCtx)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}

	sess, err := p.client.exchangeCode(ctx, code, pair.verifier)
	if err != nil {
		return fmt.Errorf("exchanging code for session: %w", err)
	}

	if err := p.store.set(sess.RefreshToken); err != nil {
		return fmt.Errorf("storing refresh token: %w", err)
	}

	p.mu.Lock()
	p.cached = sess
	p.mu.Unlock()

	return nil
}

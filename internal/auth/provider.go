// Package auth is the one token provider the CLI and the agent share
// (CO-103): it returns a valid Supabase access token, refreshing from the
// stored refresh token as needed, or a typed ErrNotLoggedIn.
//
// Login is a loopback-redirect PKCE flow against Supabase Auth's GitHub
// provider — not GitHub's own device flow. See .claude/rules/auth.md for
// why: Supabase Auth's GitHub sign-in is browser-redirect only, with no
// native device-flow support. A device-flow UX (`login --device`, for
// headless/SSH machines) is a deliberate follow-up, not built here.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/devminified/carryon/internal/config"
)

// ErrNotLoggedIn is returned by AccessToken when there's no stored session
// (never logged in, logged out, or the refresh token was revoked).
var ErrNotLoggedIn = errors.New("not logged in — run `carryon login`")

// refreshSkew is how far ahead of actual expiry a cached access token is
// treated as stale, so a caller never races a token expiring mid-request.
const refreshSkew = 2 * time.Minute

// Provider is the shared token provider. The CLI and the agent each
// construct one from the laptop's config.Config and call AccessToken
// before every authenticated request.
type Provider struct {
	client *authClient
	store  *store

	mu     sync.Mutex
	cached *session
}

// NewProvider builds a Provider for the given backend.
func NewProvider(cfg *config.Config) (*Provider, error) {
	s, err := newStore()
	if err != nil {
		return nil, err
	}
	return &Provider{client: newAuthClient(cfg.APIURL, cfg.AnonKey), store: s}, nil
}

// AccessToken returns a valid access token, transparently refreshing it if
// the cached one is stale or absent. Returns ErrNotLoggedIn if there's no
// stored session, including when a refresh is rejected because the
// refresh token was revoked (reuse detected, or an admin revoked the
// device).
func (p *Provider) AccessToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cached != nil && time.Until(p.cached.ExpiresAt) > refreshSkew {
		return p.cached.AccessToken, nil
	}

	refreshToken, err := p.store.get()
	if errors.Is(err, errNoCredential) {
		return "", ErrNotLoggedIn
	}
	if err != nil {
		return "", fmt.Errorf("reading stored credentials: %w", err)
	}

	sess, err := p.client.refresh(ctx, refreshToken)
	if err != nil {
		var authErr *authError
		if errors.As(err, &authErr) && authErr.StatusCode == http.StatusUnauthorized {
			// Revoked, expired, or replayed (reuse detection tripped the
			// whole token family) — the stored credential is dead either
			// way. Clear it so the next call doesn't retry the same
			// doomed refresh token, and report it plainly as logged out.
			_ = p.store.delete()
			return "", ErrNotLoggedIn
		}
		return "", fmt.Errorf("refreshing session: %w", err)
	}

	if err := p.store.set(sess.RefreshToken); err != nil {
		return "", fmt.Errorf("storing refreshed credentials: %w", err)
	}
	p.cached = sess
	return sess.AccessToken, nil
}

// LoggedIn reports whether a session is currently stored, without making a
// network call (used by `carryon login` to ask before replacing one, and
// by `carryon status`).
func (p *Provider) LoggedIn() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cached != nil {
		return true
	}
	_, err := p.store.get()
	return err == nil
}

// CurrentUser returns who's logged in, refreshing the access token first
// if needed. Returns ErrNotLoggedIn under the same conditions as
// AccessToken.
func (p *Provider) CurrentUser(ctx context.Context) (*User, error) {
	token, err := p.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	return p.client.currentUser(ctx, token)
}

// Logout deletes the stored session from both the keyring and the file
// fallback, and clears the in-memory cache.
func (p *Provider) Logout() error {
	p.mu.Lock()
	p.cached = nil
	p.mu.Unlock()
	return p.store.delete()
}

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// session is a Supabase Auth session: a short-lived access token (the JWT
// RLS checks against via auth.uid()) and a refresh token. expiresAt is
// computed on receipt from the server's expires_in, not trusted verbatim.
type session struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// authClient talks to a Supabase project's Auth endpoints (/auth/v1/...).
// Every request carries apikey, matching every official Supabase client —
// confirmed against a live local instance that Kong accepts requests
// without it locally, but that's local-only leniency; the hosted gateway
// requires it.
type authClient struct {
	baseURL string // e.g. http://127.0.0.1:54421 or https://xyz.supabase.co
	apiKey  string
	http    *http.Client
}

func newAuthClient(baseURL, apiKey string) *authClient {
	return &authClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// authorizeURL builds the GitHub sign-in URL to open in the browser.
// Verified against a live local Supabase Auth instance: GET
// /auth/v1/authorize?provider=github&redirect_to=...&code_challenge=...
// &code_challenge_method=s256 302s to GitHub's own OAuth consent page.
func (c *authClient) authorizeURL(redirectTo, codeChallenge string) string {
	q := url.Values{
		"provider":              {"github"},
		"redirect_to":           {redirectTo},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"s256"},
	}
	return c.baseURL + "/auth/v1/authorize?" + q.Encode()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	Msg          string `json:"msg,omitempty"`
}

// exchangeCode trades the PKCE auth code (received on the loopback
// callback) and its verifier for a session. Verified against a live local
// instance: POST /auth/v1/token?grant_type=pkce with a JSON body of
// {auth_code, code_verifier}.
func (c *authClient) exchangeCode(ctx context.Context, code, verifier string) (*session, error) {
	body := map[string]string{"auth_code": code, "code_verifier": verifier}
	return c.tokenRequest(ctx, "pkce", body)
}

// refresh trades a refresh token for a new session. Supabase Auth rotates
// the refresh token on every use (enable_refresh_token_rotation = true in
// supabase/config.toml) with reuse detection, so the returned
// RefreshToken must replace the stored one, never be discarded.
func (c *authClient) refresh(ctx context.Context, refreshToken string) (*session, error) {
	body := map[string]string{"refresh_token": refreshToken}
	return c.tokenRequest(ctx, "refresh_token", body)
}

func (c *authClient) tokenRequest(ctx context.Context, grantType string, body map[string]string) (*session, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding token request: %w", err)
	}

	reqURL := c.baseURL + "/auth/v1/token?grant_type=" + url.QueryEscape(grantType)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("building token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling %s: %w", reqURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading token response: %w", err)
	}

	var tr tokenResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return nil, fmt.Errorf("parsing token response (status %d): %w", resp.StatusCode, err)
	}

	if resp.StatusCode != http.StatusOK {
		msg := tr.Msg
		if msg == "" {
			msg = tr.Error
		}
		return nil, &authError{StatusCode: resp.StatusCode, Code: tr.ErrorCode, Message: msg}
	}

	return &session{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
	}, nil
}

// User is the subset of Supabase Auth's /auth/v1/user response Carryon
// needs — just enough to show who's logged in.
type User struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	UserName string `json:"-"` // github login, lifted out of user_metadata below
}

type userResponse struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	UserMetadata struct {
		UserName string `json:"user_name"`
	} `json:"user_metadata"`
}

// currentUser calls GET /auth/v1/user with accessToken. Verified against a
// live local instance: requires both apikey and Authorization: Bearer
// headers, and rejects a malformed token with 403 bad_jwt.
func (c *authClient) currentUser(ctx context.Context, accessToken string) (*User, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/auth/v1/user", nil)
	if err != nil {
		return nil, fmt.Errorf("building user request: %w", err)
	}
	req.Header.Set("apikey", c.apiKey)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling /auth/v1/user: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading user response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var tr tokenResponse
		_ = json.Unmarshal(raw, &tr)
		msg := tr.Msg
		if msg == "" {
			msg = tr.Error
		}
		return nil, &authError{StatusCode: resp.StatusCode, Code: tr.ErrorCode, Message: msg}
	}

	var ur userResponse
	if err := json.Unmarshal(raw, &ur); err != nil {
		return nil, fmt.Errorf("parsing user response: %w", err)
	}
	return &User{ID: ur.ID, Email: ur.Email, UserName: ur.UserMetadata.UserName}, nil
}

// authError is a Supabase Auth API error response.
type authError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *authError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("supabase auth: %s (%s, HTTP %d)", e.Message, e.Code, e.StatusCode)
	}
	return fmt.Sprintf("supabase auth: %s (HTTP %d)", e.Message, e.StatusCode)
}

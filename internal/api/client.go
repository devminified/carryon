// Package api is the Edge Function client: everything the laptop binary
// needs from Carryon's server side (register-project, match-project, and
// later register-session/register-chunk/get-manifest/…) goes through
// here, authenticated with the caller's access token from internal/auth —
// never the service-role key, which this binary must never hold (see
// .claude/rules/architecture.md's trust-zone boundary).
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client calls a Supabase project's Edge Functions (/functions/v1/...).
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// NewClient builds a Client for the given backend. baseURL and apiKey are
// a config.Config's APIURL/AnonKey — the same ones internal/auth uses.
func NewClient(baseURL, apiKey string) *Client {
	return NewClientWithHTTP(baseURL, apiKey, &http.Client{Timeout: 30 * time.Second})
}

// NewClientWithHTTP is NewClient with a caller-supplied *http.Client —
// CO-505's load-test tool uses this to wrap Edge Function calls in a
// RoundTripper that injects random failures (internal/loadtest's own
// "random network drops" testing point), without adding a test-only
// code path to the normal agent/CLI construction, which stays on
// NewClient.
func NewClientWithHTTP(baseURL, apiKey string, httpClient *http.Client) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    httpClient,
	}
}

// Error is an Edge Function's error response shape (see
// .claude/rules/edge-functions.md's "Response shape is stable").
type Error struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("carryon api: %s (%s, HTTP %d)", e.Message, e.Code, e.StatusCode)
	}
	return fmt.Sprintf("carryon api: HTTP %d", e.StatusCode)
}

// IsNotFound reports whether err is a 404 from an Edge Function (e.g.
// match-project's "no project registered for this remote").
func IsNotFound(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// IsForbidden reports whether err is a 403 from an Edge Function (e.g.
// match-project's "registered, but you're not a member").
func IsForbidden(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden
}

// post calls an Edge Function with a JSON body and the caller's access
// token, and decodes a JSON response into out (which may be nil to
// discard the body).
func (c *Client) post(ctx context.Context, function, accessToken string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encoding %s request: %w", function, err)
	}

	url := c.baseURL + "/functions/v1/" + function
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building %s request: %w", function, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", c.apiKey)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("calling %s: %w", function, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading %s response: %w", function, err)
	}

	if resp.StatusCode >= 300 {
		var er errorResponse
		_ = json.Unmarshal(raw, &er)
		return &Error{StatusCode: resp.StatusCode, Code: er.Error, Message: er.Message}
	}

	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("parsing %s response: %w", function, err)
	}
	return nil
}

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

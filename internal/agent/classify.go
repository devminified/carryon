package agent

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/devminified/carryon/internal/api"
	"github.com/devminified/carryon/internal/auth"
)

// ErrFatal marks an upload error CO-112 says must not be retried
// automatically. Agent only needs to tell fatal and retryable errors
// apart (via errors.Is against this sentinel) — ClassifyError below is
// what actually decides which is which, so that decision lives in one
// place instead of being duplicated at every call site that can produce
// one of these errors.
var ErrFatal = errors.New("fatal upload error — won't be retried automatically")

// ClassifyError wraps err with ErrFatal when retrying it won't help
// (CO-112): the laptop isn't logged in at all (auth.ErrNotLoggedIn), or
// the server says the caller has lost access — 401 (even after a fresh
// token: by the time any Edge Function call happens, TokenSource has
// already transparently refreshed it, so a 401 here means the server
// itself rejected a token that looked valid), 403, or 404. Every other
// shape of failure — timeouts, 5xx, DNS failures, connection refused, a
// transient 409 — is retryable by omission: ClassifyError returns it
// completely unchanged, and Agent's own exponential backoff (not this
// function) is what keeps retrying it from spinning.
//
// Every caller that can produce one of these errors on Agent's behalf —
// internal/upload.Uploader.Upload and internal/filter.Filter's own
// match-project lookup — runs its return value through this before it
// reaches Agent.
func ClassifyError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, auth.ErrNotLoggedIn) {
		return fmt.Errorf("%w: %w", err, ErrFatal)
	}
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
			return fmt.Errorf("%w: %w", err, ErrFatal)
		}
	}
	return err
}

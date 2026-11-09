package api

import (
	"context"
	"time"
)

// EndSession calls the end-session Edge Function (CO-111) once the
// upload covering a session's SessionEnd event has been confirmed. Safe
// to call more than once for the same sessionId (I3) — the server-side
// update is a plain, idempotent status flip.
func (c *Client) EndSession(ctx context.Context, accessToken, sessionID string) error {
	body := map[string]string{"sessionId": sessionID}
	return c.post(ctx, "end-session", accessToken, body, nil)
}

// ConfirmPull calls the confirm-pull Edge Function (CO-304) once
// `carryon pull` has fully succeeded for sessionID — writes the
// audit_log's "pull_complete" row. There's no other way for the server
// to know a pull finished: chunk bytes flow straight from Storage to
// the laptop (I9), never through the API, so get-manifest's own
// "pull_manifest" row only ever covers the start of a pull.
func (c *Client) ConfirmPull(ctx context.Context, accessToken, sessionID string) error {
	body := map[string]string{"sessionId": sessionID}
	return c.post(ctx, "confirm-pull", accessToken, body, nil)
}

// RenameSession calls the rename-session Edge Function (CO-405):
// `carryon rename <id> "<title>"`. Sets title_source = 'manual'
// server-side so a later AI re-summary (CO-407) never silently
// overwrites a developer's own title. Returns an *Error satisfying
// IsForbidden if the caller is neither the session's owner nor an
// admin of its project, IsNotFound if sessionID doesn't exist.
func (c *Client) RenameSession(ctx context.Context, accessToken, sessionID, title string) error {
	body := map[string]string{"sessionId": sessionID, "title": title}
	return c.post(ctx, "rename-session", accessToken, body, nil)
}

// DeleteSession calls the delete-session Edge Function (CO-307):
// `carryon admin delete <sid> --reason`. reason is mandatory — the
// server refuses an empty or whitespace-only one (validation_failed,
// HTTP 400). Returns an *Error satisfying IsForbidden if the caller
// isn't an admin on the session's project, IsNotFound if sessionID
// doesn't exist.
func (c *Client) DeleteSession(ctx context.Context, accessToken, sessionID, reason string) error {
	body := map[string]string{"sessionId": sessionID, "reason": reason}
	return c.post(ctx, "delete-session", accessToken, body, nil)
}

// ListSessionsInput is list-sessions' (CO-204) optional filters — every
// field's zero value means "don't filter on this."
type ListSessionsInput struct {
	Project string
	Dev     string
	Status  string
	Since   *time.Time
	// Limit is capped and defaulted server-side if zero; see
	// list-sessions/index.ts's DEFAULT_LIMIT.
	Limit int
}

// SessionSummary is one row of list-sessions' response — everything
// `carryon ls` shows a column for, plus ProjectID (not displayed, but
// needed by anything that filters further client-side).
type SessionSummary struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"projectId"`
	ProjectName    string    `json:"projectName"`
	OwnerLogin     string    `json:"ownerLogin"`
	Branch         string    `json:"branch"`
	Title          string    `json:"title"`
	LastActivityAt time.Time `json:"lastActivityAt"`
	Status         string    `json:"status"`
}

// ListSessions calls the list-sessions Edge Function (CO-204): every
// session the caller is a member of the project for (any role — see
// auth.md's access table), newest activity first.
func (c *Client) ListSessions(ctx context.Context, accessToken string, in ListSessionsInput) ([]SessionSummary, error) {
	body := map[string]any{}
	if in.Project != "" {
		body["project"] = in.Project
	}
	if in.Dev != "" {
		body["dev"] = in.Dev
	}
	if in.Status != "" {
		body["status"] = in.Status
	}
	if in.Since != nil {
		body["since"] = in.Since.UTC().Format(time.RFC3339)
	}
	if in.Limit > 0 {
		body["limit"] = in.Limit
	}

	var out struct {
		Sessions []SessionSummary `json:"sessions"`
	}
	if err := c.post(ctx, "list-sessions", accessToken, body, &out); err != nil {
		return nil, err
	}
	return out.Sessions, nil
}

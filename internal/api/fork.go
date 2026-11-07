package api

import "context"

// RegisterForkInput is what register-fork (CO-208) needs to create the
// sessions row for a pulled/forked session. The puller's own identity
// is derived server-side from the caller's JWT, never sent here.
type RegisterForkInput struct {
	ParentSessionID string
	NewSessionID    string
}

// RegisterForkResult is register-fork's response.
type RegisterForkResult struct {
	SessionID string `json:"sessionId"`
}

// RegisterFork calls the register-fork Edge Function (CO-208), which
// creates a new sessions row — parent_session_id set, status "live",
// owned by the caller — so a resumed session's own agent has somewhere
// to upload into. Returns an *Error satisfying IsNotFound if
// ParentSessionID doesn't exist, or IsForbidden if the caller isn't
// resumer+ on its project.
func (c *Client) RegisterFork(ctx context.Context, accessToken string, in RegisterForkInput) (*RegisterForkResult, error) {
	body := map[string]string{
		"parentSessionId": in.ParentSessionID,
		"newSessionId":    in.NewSessionID,
	}
	var out RegisterForkResult
	if err := c.post(ctx, "register-fork", accessToken, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

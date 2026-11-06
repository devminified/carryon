package api

import "context"

// EndSession calls the end-session Edge Function (CO-111) once the
// upload covering a session's SessionEnd event has been confirmed. Safe
// to call more than once for the same sessionId (I3) — the server-side
// update is a plain, idempotent status flip.
func (c *Client) EndSession(ctx context.Context, accessToken, sessionID string) error {
	body := map[string]string{"sessionId": sessionID}
	return c.post(ctx, "end-session", accessToken, body, nil)
}

package api

import (
	"context"
	"time"
)

// HeartbeatInput is CO-410/CO-504's full agent check-in payload — an
// options struct (not a long positional parameter list, CLAUDE.md's
// own convention) since this grew from 4 fields to 9 once CO-504
// added the OS/bytes-behind/last-upload/error/redaction signals.
// BlockedSessions is how many sessions Agent.writeStatus's own
// agent-status.json currently reports as Blocked (stuck in retry
// backoff, go-conventions.md). BytesBehind and LastUploadAt are the
// aggregate across every session this machine is tracking — the sum
// and the most recent, respectively — computed via
// internal/status.BytesBehind, the same real cross-reference `carryon
// status` itself uses, not a synthetic estimate. UploadAttempts/
// UploadErrors/RedactionsApplied come from internal/upload.Uploader's
// own TakeCounts — counts *since the last heartbeat*, not an all-time
// total, so a recent spike is visible rather than diluted.
type HeartbeatInput struct {
	MachineID         string
	Hostname          string
	Version           string
	OS                string
	BlockedSessions   int
	BytesBehind       int64
	LastUploadAt      *time.Time
	UploadAttempts    int64
	UploadErrors      int64
	RedactionsApplied int64
}

// Heartbeat calls the heartbeat Edge Function (CO-410, extended by
// CO-504) — the agent's own periodic "I'm alive, here's how far
// behind and how healthy I am" check-in, backing the web admin
// "Agents" page and its own observability dashboard. machineID is
// config.Config.MachineID (stable across restarts on this laptop).
func (c *Client) Heartbeat(ctx context.Context, accessToken string, in HeartbeatInput) error {
	body := map[string]any{
		"machineId":         in.MachineID,
		"hostname":          in.Hostname,
		"version":           in.Version,
		"os":                in.OS,
		"blockedSessions":   in.BlockedSessions,
		"bytesBehind":       in.BytesBehind,
		"uploadAttempts":    in.UploadAttempts,
		"uploadErrors":      in.UploadErrors,
		"redactionsApplied": in.RedactionsApplied,
	}
	if in.LastUploadAt != nil {
		body["lastUploadAt"] = in.LastUploadAt.UTC().Format(time.RFC3339)
	}
	return c.post(ctx, "heartbeat", accessToken, body, &struct{}{})
}

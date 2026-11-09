package api

import "context"

// Heartbeat calls the heartbeat Edge Function (CO-410) — the agent's
// own periodic "I'm alive, here's my version and how far behind I am"
// check-in, backing the web admin "Agents" page. machineID is
// config.Config.MachineID (stable across restarts on this laptop).
// blockedSessions is how many sessions Agent.writeStatus's own
// agent-status.json currently reports as Blocked (stuck in retry
// backoff, go-conventions.md) — the honest signal the agent already
// tracks for a struggling machine, rather than a synthetic "queue
// depth" this package has no real way to compute.
func (c *Client) Heartbeat(ctx context.Context, accessToken, machineID, hostname, version string, blockedSessions int) error {
	body := map[string]any{
		"machineId":       machineID,
		"hostname":        hostname,
		"version":         version,
		"blockedSessions": blockedSessions,
	}
	return c.post(ctx, "heartbeat", accessToken, body, &struct{}{})
}

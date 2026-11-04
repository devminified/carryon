# Architectural Principles — Read Before Writing Code

Full rationale lives in Doc (System Design Document). This file is the enforceable summary.

## Three trust zones

1. **Laptop** — does the expensive and sensitive work: redaction, compression, hashing (`internal/redact`, `internal/upload`, `internal/snapshot`). Nothing here blocks on the network except the agent's own background retries.
2. **Server / cell** — Postgres (metadata), Storage (bytes), Edge Functions (API), queue workers (background jobs). Stateless, horizontally scalable, never a single point of global state.
3. **Third parties** — each gets only what it strictly needs: GitHub sees identity and code (via OAuth + hidden refs), the Anthropic API sees redacted/condensed text only, Slack sees titles and links only.

**Don't blur these.** A laptop binary calling the Anthropic API directly, or a web app reading a local queue file, breaks the model that makes the security story (Doc §Security architecture) hold.

## Bytes bypass the API (I9)

Session and snapshot bytes flow laptop ↔ object storage directly over short-lived signed URLs. Edge Functions (`register-chunk`, `confirm-chunk`, `get-manifest`, …) only ever hand out metadata and signed URLs. **Never** add a code path where an Edge Function reads a chunk's bytes into memory to forward them — that reintroduces the server as a bottleneck and a single point of failure the design explicitly avoids.

## Append-only, idempotent, crash-safe (I2, I3, I8)

- Session files, the local queue, `session_chunks`, and `audit_log` are append-only. A "correction" is a new row/version, never an `UPDATE` of existing bytes or an edited audit entry.
- Every write that can be retried must be idempotent. Chunks are keyed by `(session_id, seq)`; a retried upload with the same key is a no-op, not a duplicate.
- State that must survive a crash (the local queue, per-session pointer files) is written via atomic rename, never a partial in-place write.

## RLS is the security boundary, behind laptop-side redaction

Permission checks happen in Postgres via RLS (I6), same as the directory default. The difference from a typical app: RLS protects data that has *already* been redacted on the laptop (I5). A secret that leaves the laptop unredacted is not fixed by any amount of RLS — see `.claude/rules/redaction.md`. Treat redaction as layer 1 and RLS as layer 3 (Doc §Security architecture's four layers); don't treat either as sufficient alone.

## Server Actions for the web app's mutations

Same as the directory default: all mutations in `web/` go through Next.js Server Actions calling into Supabase (never a client-side `supabase.from().insert()`). Reads use Server Components by default; reach for TanStack Query only once a screen genuinely needs live updates (see `data-fetching.md`).

## One writer per session (I7)

Leases with fencing tokens enforce this server-side. Any new code path that appends to a session (a new upload stage, a backfill job) must acquire the lease first — don't add a second writer "just for this one case."

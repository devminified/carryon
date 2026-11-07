# Edge Functions (Supabase, Deno/TypeScript)

The named RPCs: `register-session`, `register-chunk`, `confirm-chunk`, `end-session`, `get-manifest`, `register-snapshot`, `summarize-session`, `rescan`, `sync-github-teams`. One directory per function under `supabase/functions/`.

`sign-snapshot-bundle` (CO-202) joins this list as a narrow exception to "one function per RPC, named up front in the build plan": it exists purely because `register-snapshot`'s git-bundle fallback path needs a signed Storage upload URL before it has anything to register, the same two-phase shape `register-chunk`/`confirm-chunk` already has for chunks. It signs only — `register-snapshot` is still the one function that ever writes a `code_snapshots` row, for both the ref and the bundle path.

## Contract

```ts
// supabase/functions/register-chunk/index.ts
import { z } from 'npm:zod@4.6.7'; // pin an exact version — deno lint's no-unversioned-import is not optional

const Input = z.object({
  sessionId: z.string().uuid(),
  seq: z.number().int().nonnegative(),
  byteStart: z.number().int().nonnegative(),
  byteEnd: z.number().int().positive(),
  sha256: z.string().length(64),
});

Deno.serve(async (req) => {
  const parsed = Input.parse(await req.json()); // throws on invalid input — caught by the top-level handler
  // ... RLS-scoped insert, using the caller's JWT, never the service key
});
```

Rules:

1. **Zod-validate every input.** No free-form casting of the request body.
2. **Run with the caller's JWT**, never the service-role key — except queue workers, which use a narrowly scoped role (Doc §"API hardening"). If a function seems to need the service key to "just work," that's a sign it belongs behind RLS instead.
3. **Idempotent on `(session_id, seq)`** (or the equivalent key for the function) — a retried call must be a safe no-op, not a duplicate (I3). `register-chunk` and `confirm-chunk` in particular must tolerate at-least-once delivery.
4. **Size and schema limits enforced**, not just typed — a Zod schema with no `.max()` on a string isn't enough on a public ingress function.
5. **Rate limiting:** token bucket per user and per project (Doc §"API hardening"). Required before any function is reachable without a prior narrower check.
6. **Structured logs only** — log the session ID and the step, never the chunk's content or a raw transcript line.
7. **Response shape is stable** — same success/error envelope across functions.

## How background work actually runs here

The current build plan has no queue or outbox — it's two patterns, both already named in specific tickets:

- **Triggered directly from the request handler that caused it.** `end-session` (CO-111) is what flips a session to `ended`; `summarize-session` (CO-407) is "triggered on `end-session`" — i.e. `end-session` invokes it, synchronously or fire-and-forget, not through an intermediate queue table.
- **Scheduled (cron) Edge Functions** for periodic work: `sync-github-teams` runs every 15 minutes (CO-303), the secret rescan runs nightly (CO-306), live-session re-summarization runs every 30 minutes (CO-407). Supabase's `pg_cron` or a scheduled GitHub Action triggers these — there's no persistent job queue to feed.
- **Scheduled, but not an Edge Function at all**, when the job is pure SQL: CO-111's idle-session sweep (ending a session with 12 hours of no activity) is `pg_cron` calling a plain Postgres function directly — no Deno runtime, no HTTP hop, nothing to deploy separately. See `.claude/rules/database-rls.md`'s "Scheduled jobs that are pure SQL" for the pattern (including the default-`PUBLIC`-`EXECUTE` footgun it has to guard against). Reach for this instead of a scheduled Edge Function whenever the job needs nothing Postgres itself can't already do.

**Not part of the current build plan:** an outbox table, a queue (pgmq or otherwise), and worker processes are Doc §"Asynchronous work" (Tier 1/2+ scaling), not anything a 52-ticket build-plan item asks for. If a ticket introduces real queueing (high enough chunk volume that a synchronous call from `confirm-chunk` becomes a bottleneck), build it then — don't pre-build the outbox plumbing now.

## `summarize-session` and the Anthropic API

The only Edge Function that calls an external AI provider. It receives already-redacted, already-condensed text — never a raw transcript line, and never anything that bypassed `internal/redact` on the way up. See `.claude/rules/redaction.md` for what "already redacted" guarantees and what it doesn't.

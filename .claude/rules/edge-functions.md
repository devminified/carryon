# Edge Functions (Supabase, Deno/TypeScript)

The named RPCs: `register-session`, `register-chunk`, `confirm-chunk`, `end-session`, `get-manifest`, `register-snapshot`, `summarize-session`, `rescan`, `sync-github-teams`. One directory per function under `supabase/functions/`.

## Contract

```ts
// supabase/functions/register-chunk/index.ts
import { z } from 'npm:zod';

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
5. **Rate limiting:** token bucket per user and per tenant (Doc §"API hardening"). Required before any function is reachable without a prior narrower check.
6. **Structured logs only** — log the session ID and the step, never the chunk's content or a raw transcript line.
7. **Response shape is stable** — same success/error envelope across functions.

## The outbox pattern

`confirm-chunk` and `end-session` write an **outbox** row in the same transaction as their main change — this gives an atomic guarantee: if the chunk/session state exists, the follow-up job will run, with no "forgot to also queue the job" class of bug. A relay moves outbox rows onto queues; workers process them idempotently, at-least-once, with a 5-minute invisibility window and a dead-letter queue after 5 failures (alerting an admin).

| Queue | Work | Coalescing |
| --- | --- | --- |
| `summarize` | AI title/summary (incremental fold) | keyed by session — a newer event replaces a pending one |
| `embed` | search vectors | keyed by session |
| `rescan` | server-side secret scan of new chunks | none — every chunk scanned once |
| `compact` | manifest + object compaction | keyed by session, ended sessions only |
| `notify` | Slack messages | keyed by event |

When adding a new background job: write it as a queue worker consuming an outbox-fed queue, not as logic inlined into the Edge Function's request handler. Request handlers stay fast; everything that can fail independently or take longer than a request timeout goes through the queue.

## `summarize-session` and the Anthropic API

The only Edge Function that calls an external AI provider. It receives already-redacted, already-condensed text — never a raw transcript line, and never anything that bypassed `internal/redact` on the way up. See `.claude/rules/redaction.md` for what "already redacted" guarantees and what it doesn't.

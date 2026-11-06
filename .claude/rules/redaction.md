# Redaction & Secret Handling (I5, NFR-07)

Binding rule: **no secret matched by the current rule set leaves the laptop**, for every session, every time. This is the first of the four security layers in Doc §"Security architecture" — everything downstream (RLS, signed URLs, audit log) assumes this layer already ran.

## Where it runs

`internal/redact`, called from `internal/upload` **before compression**. Redaction must run on every chunk, with no opt-out flag, no per-project exemption, and no "trusted repo" bypass. If a ticket proposes skipping redaction for performance, the answer is to make redaction faster, not to skip it.

## What it does

- **Pattern matching:** Aho–Corasick + RE2 for known secret shapes (API keys, tokens, private keys), the same rule family gitleaks v8 ships, used as a library (not the CLI) so it runs in-process on every chunk.
- **Entropy scan:** catches high-entropy strings that don't match a known pattern — the backstop for secret formats not yet in the rule set.
- **Path denylist:** whole tool results are dropped (not redacted-in-place) for paths like `.env`, private keys, and certificates — some content is denylisted entirely rather than scanned.
- **Streaming windows for large lines:** lines over 32MB are scanned in overlapping windows (overlap ≥ longest rule length), so a secret can never hide across a window boundary.

## Rules for changing the rule set

- Every new or changed redaction rule needs a corresponding case in the redaction corpus test suite before it merges — a rule with no test proving it fires (and no test proving it doesn't over-fire on legitimate content) doesn't ship.
- Widening what's considered "safe to pass through" (a new exemption, a narrower pattern) is a security-sensitive change — treat it like an RLS policy change: it needs the same scrutiny as loosening a `using` clause.
- Never log the matched secret itself, even at debug level, even locally. Log that a redaction fired, the rule ID, and the location — never the value.

## Server-side rescan is a backstop, not the primary defense (CO-306)

A nightly `rescan` Edge Function re-scans stored chunks against the current rule set, to catch anything that slipped past layer 1 because the rule set has since improved. On a hit: rotate the exposed credential, purge the chunk, log the incident — in that order. The rescan existing does not justify loosening laptop-side redaction; it exists because rule sets improve over time and old chunks were scanned under an older rule set.

## Signed URLs

Every signed URL (upload or download) is scoped to one object and one HTTP method, and expires in **5 minutes or less** (NFR-07, I6) — with one verified, platform-level exception: Supabase Storage's upload signed URLs (`createSignedUploadUrl`, used by `register-chunk`/CO-110) have a fixed **2-hour** expiry with no `expiresIn` option, confirmed against a live local instance (both the SDK and the raw `/object/upload/sign/...` endpoint ignore an `expiresIn` body field entirely — there is no config.toml knob for it either). Download signed URLs (`createSignedUrl`, used by `get-manifest`/CO-203) *do* support `expiresIn` and must stay at 5 minutes or less — this exception is upload-URL-only. A longer TTL "to make testing easier" doesn't ship — fix the test instead; this one exception is a documented platform limit, not an opt-out pattern to reuse elsewhere without the same verification.

Upload signed URLs for a chunk's object key must be requested with `{upsert: true}` (`x-upsert: true` header — the SDK's `createSignedUploadUrl(path, {upsert: true})` sets this correctly; a raw request needs the header, not a body field) — verified against a live instance that *registering* a signed URL for an existing key 409s (`KeyAlreadyExists`) without it. This is what makes retrying a chunk upload (same `session_id`/`seq`, e.g. after `confirm-chunk` failed despite the `PUT` succeeding) safe rather than hitting a wall at the sign step (I3).

An Edge Function's `createSignedUploadUrl(...).data.signedUrl` bakes in that function's own `SUPABASE_URL` env var — inside Docker (local dev) this is an internal hostname (`http://kong:8000`), unreachable from the laptop making the actual `PUT`. The *token* itself (`data.token`) carries no host claim, only the object path, scope and expiry, so `register-chunk` (CO-110) returns the token, not the full URL, and the laptop client builds its own upload URL from its own already-correct `api_url`. Found by a real `PUT` failing DNS resolution on `kong` from outside Docker, not assumed — the same caution applies to any future Edge Function that hands a Storage URL back to a laptop client.

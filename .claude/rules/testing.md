# Testing

Four runtimes, one CI run per PR. This mirrors Doc's "CI on every pull request" exactly — don't invent a different matrix.

## Go (`cmd/`, `internal/`)

- `go vet` and `golangci-lint` — static analysis, zero tolerance.
- `go test -race ./...` — the race detector is mandatory given the agent's debounce/scheduler and the pull worker pool (`go-conventions.md`).
- Then cross-compile all five release targets (macOS arm64/amd64, Linux amd64/arm64, Windows amd64) — a package that only compiles on the dev's own OS is not done.
- **Redaction corpus tests** (`internal/redact`) — every rule-set change needs a positive case (it fires) and a negative case (it doesn't over-fire). See `redaction.md`.
- **Chaos tests** (CO-112, CO-505) for crash/offline/reboot durability (NFR-04) — kill the agent mid-upload, mid-write, and assert no lost bytes and a byte-identical rebuild.

## Database

- `supabase db lint`, then apply all migrations to a fresh database.
- **pgTAP** tests for every RLS policy, every role, every table (NFR-08) — a forbidden role must get zero rows, not an error that leaks existence. Files live under `supabase/tests/database/*.sql`, one per table; run with `supabase test db supabase/tests/database --local` (wired into CI's `supabase` job, CO-302). See `database-rls.md` for the role-simulation harness pattern.

## Edge Functions

- `deno lint`, `deno fmt --check`, and `deno test` across `supabase/functions/` — all three are a required CI check (`ci.yml`'s `supabase` job), same as Go's static analysis.
- `deno test` per function, covering the Zod schema boundary and idempotency on `(session_id, seq)` retries (I3).

## Web (`web/`)

- Lint, type-check, Vitest unit tests for pure functions and schemas, matching the directory default.

## End-to-end (the fixture-writer test)

A Linux CI runner starts local Supabase, runs the fake Claude transcript writer (`test/fixtures/writer`) that appends lines and fires the hook exactly like Claude Code would, then checks the upload reaches Storage. A second simulated user pulls the session and the test compares bytes to assert a byte-identical rebuild (I4, NFR-06). This is the one test that exercises the full laptop → Supabase → laptop loop — don't skip it to save CI time; it's the closest thing to a real resume and is what Doc's "real test" gate (Build plan) means in CI form.

## Coverage targets

100% on `internal/redact` (secret matching) and the chunk verify/hash-chain logic in `internal/pull` — these back I4 and I5 directly, with one documented exception: `internal/redact`'s `redactJSON` sits at 99.7%, not 100%, because its one remaining uncovered line is a defensive `len(stack) == 0` bounds check inside the closing-delimiter case that — verified empirically against `encoding/json`'s own `Decoder.Token()` — can never actually be reached: a stray closing delimiter with no matching open is always rejected by the decoder itself as a syntax error before it would ever be handed back as a token. Keep the guard (cheap insurance against a future change proving this wrong) but don't chase the last 0.3% by weakening it or writing a test that doesn't really exercise what it claims to — that would be less honest than this line. 70%+ elsewhere.

## Nightly compatibility test (CO-503, NFR-10)

A scheduled workflow resumes a fixture session with the team's pinned Claude Code version. This is the gate for ever upgrading the pinned version — don't bump it without this test passing.

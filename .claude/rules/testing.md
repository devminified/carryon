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

A Linux CI runner starts local Supabase, runs `cmd/carryon/e2e_test.go`'s `TestPullEndToEndAgainstLocalSupabase` — not a separate `test/fixtures/writer` binary, which was this file's original plan but was never built that way; CO-211 built the fake Claude transcript writer as a Go test helper (`buildFakeClaudeForE2E`) directly inside that test file instead, since the real work (upload, manifest, rebuild, rewrite, launch) all needed Go-internal access to `internal/upload`/`internal/pull` anyway. One real developer uploads a session through the real upload pipeline, a second simulated user pulls it, and the test compares bytes to assert a byte-identical rebuild (I4, NFR-06). This is the one test that exercises the full laptop → Supabase → laptop loop — don't skip it to save CI time; it's the closest thing to a real resume and is what Doc's "real test" gate (Build plan) means in CI form. CO-506's `selfhost-e2e.yml` runs this exact same test, unmodified, against the self-hosted stack instead of `supabase start`.

## Load, soak and cost test (CO-505)

`internal/loadtest` + `cmd/carryon-loadtest` (driven by `make loadtest`) is a separate tool from the fixture-writer test above — it doesn't assert correctness so much as throughput and cost under many real, concurrent developers. It runs N real `agent.Uploader`s (one per simulated developer) at a jittered 30-120s-per-reply cadence against a real backend, plus a team-wide stream of real pulls (`internal/pull.Rebuild`) that verify the same byte-identical-rebuild property (NFR-04) under load rather than in isolation. `make loadtest`'s own default is a fast, free dry run against local Supabase (8 simulated hours per developer compressed into seconds via a `-speed` multiplier) — the real 50-developer x 8-real-hour run against Supabase Cloud staging the ticket describes is a deliberately separate, explicitly-gated invocation (`-confirm-real-cost`, a non-local `-api-url`) documented in `docs/co-505-load-test.md`, since it spends real money and isn't something CI or a routine PR should ever trigger.

## Coverage targets

100% on `internal/redact` (secret matching) and the chunk verify/hash-chain logic in `internal/pull` — these back I4 and I5 directly, with one documented exception: `internal/redact`'s `redactJSON` sits at 99.7%, not 100%, because its one remaining uncovered line is a defensive `len(stack) == 0` bounds check inside the closing-delimiter case that — verified empirically against `encoding/json`'s own `Decoder.Token()` — can never actually be reached: a stray closing delimiter with no matching open is always rejected by the decoder itself as a syntax error before it would ever be handed back as a token. Keep the guard (cheap insurance against a future change proving this wrong) but don't chase the last 0.3% by weakening it or writing a test that doesn't really exercise what it claims to — that would be less honest than this line. 70%+ elsewhere.

## Nightly compatibility test (CO-503, NFR-10)

`.github/workflows/claude-compat-nightly.yml` — a matrix of 3 OSes x {the team's pinned Claude Code version, latest}, each leg running `.github/scripts/claude-compat-check.sh`: installs that leg's Claude Code version, starts an ephemeral local Supabase ("test backend," the same `supabase start` technique `ci.yml`'s own `supabase`/`web-e2e` jobs use — never `carryon-staging`/`carryon-prod`), runs a real scripted `claude -p` session in a fixture repo, captures and uploads it with a real `carryon agent`, pulls it as a second real user (seeded via `~/.carryon/credentials`' own file-fallback mechanism — see that script's own comment for why this is the real, verified `internal/auth` fallback path, not a test-only backdoor), resumes it headless, and checks the answer, the `~/.claude/projects/<encoded>` folder name (`carryon debug encode-path`, CO-209), and the session-file's own line types against the known set (an unrecognized type is reported, never a failure on its own). The result — pass or fail — is recorded via the real, deployed `record-compat-run` Edge Function (`claude_compat_runs` table), which is what `web/src/modules/admin/data/actions.ts`'s own gate reads: **admins cannot set `pinned_version` (CO-308) to a version with no green run recorded for every one of the 6 (os, version-label) combinations** — clearing the pin (null) is always allowed.

Deliberately `workflow_dispatch`-only for now, not a live `schedule:` trigger — it spends a real `ANTHROPIC_API_KEY`'s credits on every run, so going live is the team's own call once `COMPAT_CI_ANTHROPIC_API_KEY`/`COMPAT_CI_SECRET`/`COMPAT_CI_RECORD_RUN_URL` are set as real repository secrets and a few manual dispatches have gone green — see the workflow file's own comment for the exact `schedule:` line to uncomment. The 14-consecutive-green-nights phase-gate validation point only starts counting once it's live.

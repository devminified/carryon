# CO-505 — Load, soak and cost test

This is the operator guide for `make loadtest` / `cmd/carryon-loadtest`. The tool itself and its test coverage live at `internal/loadtest/` and `cmd/carryon-loadtest/`; this doc covers *running* it, not its internals (read the package's own doc comments for that).

## Two very different things this tool can do

1. **A free, fast local dry run** (`make loadtest`, no setup). Registers a throwaway project against your already-running local Supabase dev stack, runs 2 simulated developers for 8 simulated hours (compressed into a handful of real seconds via `-speed`), verifies NFR-04 (byte-identical rebuild) under a real pull, and cleans up after itself. Safe to run as often as you like.
2. **The ticket's real scenario**: 50 developers x 8 real hours against real Supabase Cloud staging. This spends real money — Edge Function invocations, Storage, compute, and real Anthropic API credits if any simulated session happens to trip `summarize-session`'s own live sweep. **This is not something to run casually or as part of routine CI.** The tool itself refuses to target a non-local `-api-url` without `-confirm-real-cost`, specifically so this decision is always a deliberate one, not something a script or a careless flag combination triggers by accident.

## Running the real scenario

1. **Provision 50 real staging accounts ahead of time.** This tool never creates an account — same reasoning as every other one-time external auth setup in this codebase (`auth.md`'s GitHub OAuth App / GitHub App sections). Use Supabase Auth's admin API (or the Studio UI) against `carryon-staging` to create 50 real users with known email/password pairs.
2. **Register (or reuse) one project on staging**, and grant each of the 50 accounts at least `resumer` role on it — the first account can be the project's admin (`register-project` makes its caller admin automatically); grant the rest directly, the same way an admin would via the web app's admin console (`auth.md`'s role table).
3. **Write a credentials file**: a JSON array of `{"email": "...", "password": "..."}`, one per account.
4. **Decide the run is wanted.** This is the point to actually stop and think about it — not a formality. Confirm:
   - The staging project's plan tier can absorb this load without surprising anyone.
   - Nobody else is relying on `carryon-staging` being quiet for the window this will run in.
   - You're prepared for the real dollar cost (see "Cost" below) — this is not reversible once spent.
5. Run it for real, at real time (`-speed 1` — do not compress an actual soak):

   ```bash
   go run ./cmd/carryon-loadtest \
     -api-url https://<your-staging-ref>.supabase.co \
     -anon-key <staging-anon-key> \
     -credentials-file ./staging-50-devs.json \
     -developers 50 \
     -duration 8h \
     -speed 1 \
     -pulls-per-hour 5 \
     -network-drop-rate 0.02 \
     -confirm-real-cost \
     -out loadtest-report-staging.md
   ```

   This runs for a real 8 hours. Run it in a place that will stay up for that long (a CI runner with an 8h timeout, a tmux session on a box you control — not a laptop that will sleep).

## What the report tells you, and what it doesn't

`RenderMarkdown`'s output (written to `-out`) covers every one of the ticket's own testing points directly: 5xx/retry/data-loss, NFR-04's byte-identical rebuild, and that the whole thing is scripted and repeatable (literally — it's `make loadtest`/this exact command). It also includes a monthly cost extrapolation, scaled from this run's own measured per-developer traffic up to 50 developers x a realistic working month.

**What it does not include**, by design, and why:

- **Database CPU, Storage request-latency percentiles, summary job throughput.** These are server-side metrics this tool has no privileged access to — read them from the Supabase dashboard for the exact time window the run covered (the report's own duration/start time tells you that window).
- **Real Anthropic cost.** The synthetic traffic this tool generates never completes a real Claude Code session in a way that triggers `summarize-session`'s 30-minute live sweep — there's no real model call to measure. If you want a real Anthropic cost line, read `ai_usage` for this run's time window (real token counts, already split input/output) and call `loadtest.ExtrapolateMonthlyCost` yourself with those numbers, or open an issue to add a `-read-ai-usage` flag if this becomes a repeat need.
- **A final sign-off.** The report's own "Validation points" section lists CO-505's two checklist items as unchecked boxes on purpose — "reviewed and approved" and "plan tier confirmed" are human decisions this tool cannot make for you. Don't check them in the generated file; have the actual reviewer do that in the ticket/PR.

## Pricing assumptions

`internal/loadtest.DefaultPricing()` is a documented-as-approximate rate card (compute flat-tier, Storage per-GB and per-request, Edge Function per-invocation, Anthropic Claude Haiku per-token) — the same "approximate, not a billing API" spirit as `supabase/functions/_shared/anthropic.ts`'s own `cost_usd` column. Confirm current rates against Supabase's and Anthropic's own pricing pages before treating a report's total as final; pricing changes over time and this tool doesn't fetch it live.

## Re-running safely

The local dry run (`make loadtest`) registers and tears down its own throwaway project every time — safe to re-run freely, and safe to interrupt (its cleanup runs via `defer`, so a normal exit always cleans up; a hard kill, as usual for any test fixture, can leave a throwaway project behind — `delete from projects where name = 'loadtest-dry-run'` cleans up any stragglers).

The real staging run does **not** clean up the project or accounts it used — those are yours to manage (the same accounts can be reused for a repeat run later). It also does not delete the sessions it created on staging; if staging has a retention policy, those age out the normal way (`purge-expired-sessions`, CO-307) — if staging has no `closed_at` set, they won't, so consider a manual cleanup pass afterward if staging is meant to stay clean.

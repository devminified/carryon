# Common Commands

```bash
# Go binary
go build ./cmd/carryon          # local build
go vet ./...
golangci-lint run
go test -race ./...
goreleaser release --snapshot --clean   # local release dry-run

# Local dev
supabase status >/dev/null 2>&1 || supabase start   # see "Local Supabase container" below
cd web && pnpm install && pnpm dev   # Next.js on :3000

# Database
supabase migration new <name>
supabase migration up
supabase db lint
supabase test db supabase/tests/database --local   # pgTAP RLS suite (CO-302)
supabase gen types typescript --linked > web/src/shared/types/database.types.ts

# Edge Functions
supabase functions new <name>
supabase functions serve        # local edge function dev
deno test supabase/functions/<name>
supabase functions deploy <name>

# Web quality (run from web/)
pnpm typecheck                  # tsc --noEmit
pnpm lint                       # eslint
pnpm format                     # prettier --write
pnpm test                       # vitest
pnpm test:e2e                   # playwright

# Release (the carryon binary only — functions/migrations deploy on merge, see below)
git tag v0.1.0 && git push --tags   # triggers GoReleaser cross-compile + release

# CO-308: one-time, local, to provision the real production update-signing key
go run ./cmd/carryon-sign-manifest genkey
# release.yml runs the sign step itself on every tag push; only needed by hand when testing packaging locally:
go run ./cmd/carryon-sign-manifest sign -version <v> -private-key-hex <hex> -base-url <url> -asset <os>/<arch>=<path> -out manifest.json
```

## Edge Functions and migrations deploy on merge, not on tag

Both `supabase/functions/**` and `supabase/migrations/**` auto-deploy to `carryon-staging` on every merge to `main` that touches them (`.github/workflows/deploy-functions.yml`, `deploy-db.yml`) — never by hand-running `supabase functions deploy`/`supabase db push` from a dev machine. Each pipeline's CI gate (`ci.yml`'s `supabase` job: `deno lint`/`deno fmt --check`/`deno test`, `supabase db lint`/`db reset`, and the pgTAP RLS suite) runs as a required check before the merge that triggers deploy, so cloud deploy is always preceded by a passing local-equivalent run.

The binary reads its backend from `~/.carryon/config.toml` (`api_url`, `anon_key`) — the same build points at local, staging (`carryon-staging`), production (`carryon-prod`), or a self-hosted instance. Never hardcode an environment's URL in Go source.

## Release signing (CO-308)

`release.yml` builds once (`goreleaser release --clean --skip=publish`), signs a release manifest with `cmd/carryon-sign-manifest` if `UPDATE_SIGNING_PRIVATE_KEY_HEX` is set, then creates the real GitHub release and uploads every asset via `gh release create` — never a second `goreleaser` invocation, since rebuilding would embed a different `-X main.date=...` and invalidate whatever was just signed. `UPDATE_PUBLIC_KEY_HEX` (baked into every release build via `-ldflags`) and `UPDATE_SIGNING_PRIVATE_KEY_HEX` (the signing step only) are both unset today — see `docs/co-308-fleet-rollout.md` for the one-time `carryon-sign-manifest genkey` step that provisions them for real, and why every laptop's daily update check stays a safe no-op until then.

## Local Supabase container — check before starting, never duplicate

The local project is named `carryon-dev` (`supabase/config.toml`'s `project_id`), so its containers are `supabase_<service>_carryon-dev` — distinguishable at a glance from any other local Supabase project running on the same machine (this repo's local ports are also shifted +100 from the defaults for exactly that reason: `54421` etc., not `54321`).

**Before any task that needs the local database** (testing a migration, running a query, checking RLS), check first — never run `supabase start` as a reflex:

```bash
supabase status >/dev/null 2>&1 && echo "already running" || supabase start
```

- `supabase status` exits `0` with connection info if the stack is up, non-zero if it's down — use the exit code, don't parse output.
- If it's already running, **use it as-is**. Don't stop and restart it "to be safe," and don't run `supabase start` again "just in case" — it happens to be idempotent (checked: re-running it while already up doesn't create duplicate containers), but relying on that instead of checking first wastes a Docker image pull/health-check cycle every time, and the `status` check is instant.
- If another machine-local Supabase project is running under a different name (`docker ps` shows containers not matching `*_carryon-dev`), leave it alone — don't stop someone else's stack. That's what the port shift exists to avoid colliding with in the first place.
- `supabase stop` only when a task is truly finished and the stack won't be needed again shortly — don't stop/start it between every command in the same session.

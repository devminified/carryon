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

# Release
git tag v0.1.0 && git push --tags   # triggers GoReleaser + functions deploy
```

The binary reads its backend from `~/.carryon/config.toml` (`api_url`, `anon_key`) — the same build points at local, staging (`carryon-staging`), production (`carryon-prod`), or a self-hosted instance. Never hardcode an environment's URL in Go source.

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

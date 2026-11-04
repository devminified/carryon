# Common Commands

```bash
# Go binary
go build ./cmd/carryon          # local build
go vet ./...
golangci-lint run
go test -race ./...
goreleaser release --snapshot --clean   # local release dry-run

# Local dev
supabase start                  # local Supabase, Docker
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

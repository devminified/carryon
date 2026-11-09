# Carryon — web app

Next.js (App Router) + `@supabase/ssr`, GitHub sign-in via Supabase Auth. See the repo root's `CLAUDE.md` and `.claude/rules/` (`architecture.md`, `layers.md`, `folder-structure.md`, `auth.md`) for the binding conventions.

## Local dev

Requires local Supabase running (`supabase start` from the repo root) — `web/.env.local` points at it by default. See `.env.example` for what's needed against staging/production instead.

```bash
pnpm install
pnpm dev          # http://127.0.0.1:3000 — use 127.0.0.1, not localhost (see auth's redirect allowlist)
```

## Checks

```bash
pnpm lint
pnpm typecheck
pnpm test         # vitest — unit tests for pure functions/schemas
pnpm test:e2e     # playwright — needs local Supabase running; one test (real GitHub OAuth
                  # redirect) only runs outside CI, see e2e/auth.spec.ts.
                  # e2e/session-list.spec.ts also needs its own fixture data seeded once:
                  #   PGPASSWORD=postgres psql postgresql://postgres:postgres@127.0.0.1:54422/postgres \
                  #     -f e2e/seed.sql
                  # (not part of supabase/seed.sql — it's 10,050 rows, only web-e2e needs it)
pnpm build
```

Regenerate `src/shared/types/database.types.ts` after any migration:

```bash
supabase gen types typescript --local | grep -v '^Connecting to db' > src/shared/types/database.types.ts
```

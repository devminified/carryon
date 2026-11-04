# Adding a Ticket's Worth of Work

Carryon doesn't have a single "add a module" recipe like a CRUD app — a ticket lands in one of four shapes. Pick the matching checklist.

## A new laptop pipeline stage (e.g. a new `internal/{name}` package)

1. Create `internal/{name}/` with its own tests; wire it into the agent's pipeline or the relevant cobra command in `cmd/carryon/`.
2. If it introduces a new state file or queue, follow the append-only / atomic-rename pattern (I2, I8) — see `architecture.md`.
3. If it touches the network, it must be retried safely and idempotent (I3) — see `go-conventions.md`.
4. Add `go test -race` coverage, and a chaos test if it can be interrupted mid-operation (crash, offline, reboot).

## A new Edge Function / schema change

1. Write the SQL migration: tables, indexes, RLS policies (`using` **and** `with check`), triggers for the audit log.
2. `supabase migration up`, then regenerate `database.types.ts`.
3. Scaffold the function under `supabase/functions/{name}/` with a Zod input schema (see `edge-functions.md`).
4. If it changes state another step depends on, write it through the outbox pattern rather than inline.
5. Write a pgTAP test asserting a forbidden role gets zero rows, and a `deno test` for the function's own logic.

## A new web page / feature (`web/`)

1. Add the module folder under `web/src/modules/{name}/` (`types.ts`, `schemas.ts`, `data/`, `hooks/` if needed, `ui/`) — see `layers.md`.
2. Add the route under `web/src/app/(dashboard)/{name}/`, plus a navigation entry in the dashboard layout.
3. Add a `loading.tsx` or `<Suspense>` boundary with a matching skeleton (`loading.md`).
4. Write at least one Vitest unit test for any non-trivial pure function or schema.
5. Verify the page respects roles — a `viewer` should never see an action only `admin` can take, and the underlying Server Action must `requireRole` + rely on RLS, not just hide the button.

## A new CLI command

1. Add the cobra command in `cmd/carryon/`, thin `RunE` delegating to `internal/`.
2. Document it in `README.md`'s command list and in `commands.md` if it's a common one.
3. Add it to the CI matrix if it needs its own integration test (e.g. `carryon pull` already does, via the fixture-writer e2e test).

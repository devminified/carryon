# Naming Conventions

## TypeScript / `web/`

| Thing | Convention | Example |
| --- | --- | --- |
| Files | `kebab-case.ts` / `kebab-case.tsx` | `session-table.tsx` |
| React components | `PascalCase` | `SessionTable` |
| Functions | `camelCase` | `listSessionsForProject` |
| Hooks | `use-{thing}.ts`, `useThing` | `useLiveSessionStatus` |
| Constants | `UPPER_SNAKE_CASE` | `MAX_RETENTION_DAYS` |
| Types | `PascalCase` | `SessionRow`, `SessionStatus` |
| Zod schemas | `camelCase` ending in `Schema` | `renameSessionSchema` |
| Zustand stores | `use{Name}Store` | `useUIStore` |
| DB tables | `snake_case` plural | `session_chunks` |
| DB columns | `snake_case` | `created_at`, `project_id` |
| Routes | kebab-case, grouped with `()` for layouts | `/(dashboard)/sessions/[id]` |

## Go (`cmd/`, `internal/`)

| Thing | Convention | Example |
| --- | --- | --- |
| Packages | short, lowercase, no underscores | `redact`, `upload`, `pull` |
| Exported identifiers | `PascalCase` | `UploadChunk`, `NewAgent` |
| Unexported identifiers | `camelCase` | `debounceWindow` |
| Files | `snake_case.go` | `signed_url.go` |
| Test files | `{name}_test.go` | `redact_test.go` |
| Errors | `Err{Thing}` sentinel, or wrapped with `%w` | `ErrLeaseHeld` |

## Edge Functions (`supabase/functions/`)

Directory name matches the RPC name exactly, kebab-case: `register-chunk`, `get-manifest`, `sync-github-teams` — not the ticket ID, not an abbreviation.

## Tickets, branches, commits

Ticket IDs are `CO-<phase><nn>` (e.g. `CO-107`). Branch names use the ticket ID as a prefix: `co-107-capture-daemon`. See `git-workflow.md` for the full branch and commit convention.

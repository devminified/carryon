# Folder Structure — Canonical

```text
carryon/
├── CLAUDE.md
├── README.md
├── go.mod / go.sum
├── .github/workflows/                  CI, release, nightly compat test
│
├── .claude/
│   └── rules/                          detailed rules, imported by CLAUDE.md
│
├── cmd/
│   └── carryon/
│       └── main.go                     the one binary: wires cobra commands to internal/
│
├── internal/
│   ├── config/                         ~/.carryon/config.toml (api_url, anon_key) — loaded before anything else
│   ├── auth/                           shared token provider (CO-103): login/logout, PKCE, keyring, refresh
│   ├── hook/                           stdin JSON (session_id, transcript_path, cwd) → queue append
│   ├── install/                        merges Carryon's hooks into ~/.claude/settings.json (CO-106), backup/restore
│   ├── agent/                          daemon loop, debounce, scheduler
│   ├── queue/                          append-only queue file and compaction
│   ├── state/                          per-session pointer files (last uploaded byte offset)
│   ├── reader/                         incremental JSONL reader
│   ├── redact/                         hand-ported gitleaks-pattern rules + Aho-Corasick prefilter + path denylist (CO-301; no gitleaks library dep, no entropy scan — see redaction.md)
│   ├── upload/                         zstd compress, signed-URL client, retries, chunking
│   ├── snapshot/                       temp-index git snapshots, bundle fallback
│   ├── pull/                           manifest fetch, download, verify, decompress, rewrite, place, launch
│   ├── claudepath/                     ~/.claude path encoding per OS
│   ├── project/                        git root and remote normalization, registry
│   ├── filter/                         wraps an agent.Uploader, skipping sessions outside a registered project (CO-108)
│   ├── api/                            Edge Function client (register-session, get-manifest, …)
│   ├── status/                         `carryon status`'s Report/Gather/Render (CO-113) — reads agent/filter's own status files
│   ├── logfile/                        daily-rotating JSON log writer (CO-113), 7 kept — what `carryon logs` tails
│   └── service/                        service install per OS (launchd / systemd / Windows Service)
│
├── supabase/
│   ├── config.toml
│   ├── migrations/                     SQL migrations, timestamp-prefixed, RLS in the same file as the table
│   ├── seed.sql
│   ├── tests/
│   │   └── database/                   pgTAP RLS suite (CO-302), one *.sql file per table — see database-rls.md
│   └── functions/                      Edge Functions (Deno/TypeScript)
│       ├── register-session/
│       ├── register-chunk/
│       ├── confirm-chunk/
│       ├── end-session/
│       ├── get-manifest/
│       ├── list-sessions/             backs `carryon ls` (CO-204) — not in Doc's original RPC list, see edge-functions.md
│       ├── register-fork/             creates the sessions row for a pulled/forked session (CO-208) — not in Doc's original RPC list, see edge-functions.md
│       ├── get-config/                 server-configurable redaction rule overrides (CO-301) — not in Doc's original RPC list, see edge-functions.md
│       ├── register-snapshot/
│       ├── sign-snapshot-bundle/       signs the upload URL for register-snapshot's git-bundle fallback (CO-202)
│       ├── summarize-session/
│       ├── rescan/
│       ├── sync-github-teams/           GitHub App auth + team membership → project_members reconciliation (CO-303)
│       ├── .env.example                 Edge Function secrets beyond Supabase's own auto-provided ones (CO-303: GITHUB_APP_*, CRON_SECRET) — supabase/functions/.env (gitignored) for local dev
│       └── _shared/                    schema validation, has_role helper, Resend/Slack clients, github.ts (GitHub App JWT + installation token + team members, CO-303)
│
├── web/                                Next.js app
│   ├── next.config.ts
│   ├── tailwind config, components.json, eslint config, etc. — directory defaults apply
│   └── src/
│       ├── app/                        routes — THIN, no business logic
│       │   ├── (auth)/login/page.tsx
│       │   ├── (dashboard)/
│       │   │   ├── layout.tsx          auth-gated shell
│       │   │   ├── projects/page.tsx
│       │   │   ├── sessions/page.tsx
│       │   │   ├── sessions/[id]/page.tsx
│       │   │   └── admin/page.tsx
│       │   └── api/                    only when Server Actions genuinely don't fit
│       ├── modules/                    vertical slices, one per domain
│       │   ├── sessions/
│       │   │   ├── types.ts, schemas.ts
│       │   │   ├── data/{queries,mutations,actions,keys}.ts
│       │   │   ├── hooks/
│       │   │   └── ui/
│       │   ├── projects/
│       │   └── admin/
│       ├── shared/
│       │   ├── constants/{roles.ts, routes.ts}
│       │   ├── types/{database.types.ts, auth.ts, common.ts}  # database.types.ts is generated — DO NOT EDIT
│       │   ├── lib/{supabase/{client,server,admin}.ts, format/, auth.ts, utils.ts}
│       │   └── ui/                     reusable presentational components, shadcn generated code
│       └── middleware.ts               auth refresh, route protection
│
├── docs/                                long-form specs, requirements, decisions — e.g. docs/co-302-role-matrix.md
│
└── test/
    └── fixtures/                       sample sessions and repos, including the fake transcript writer used by CI e2e
```

## Rules About This Structure

- **Never** put laptop-side logic in `web/`, or vice versa. They share a database and a protocol, not a process or a package.
- **Never** create `web/src/components/` at the top level. Reusable UI lives in `web/src/shared/ui/`; module-specific UI lives in `web/src/modules/{module}/ui/`.
- **Never** put business logic in `web/src/app/`. Pages render layouts and module UI components.
- A `web/` module **may** import from `web/src/shared/`. A module **must not** import another module's internals.
- Every new laptop-side pipeline stage gets its own `internal/{name}/` package, named for the stage, not the ticket.
- Every new Edge Function gets its own directory under `supabase/functions/`, named for the RPC it implements, matching the names in Doc's architecture table.

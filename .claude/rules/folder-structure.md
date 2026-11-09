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
│   ├── carryon/
│   │   └── main.go                     the one binary: wires cobra commands to internal/
│   └── carryon-sign-manifest/          release-side only (CO-308) — never shipped to a laptop; genkey + sign subcommands, see internal/update
│
├── internal/
│   ├── config/                         ~/.carryon/config.toml (api_url, anon_key, machine_id) — loaded before anything else; machine_id (CO-410) is generated and persisted on first Load() if missing, for the agent's own heartbeat
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
│   ├── update/                         CO-308's signed-manifest fetch/verify/stage/self-check/swap/restart — the daily auto-update mechanism
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
│       ├── summarize-session/           the AI title/summary job's 30-minute live-session sweep (CO-407) — its *other* trigger, end-session, calls _shared/summarize.ts directly, never through this HTTP endpoint; see edge-functions.md
│       ├── rescan/                      nightly backstop secret scan + rewrite over recent chunks (CO-306)
│       ├── purge-expired-sessions/      daily retention sweep for closed, past-retention projects (CO-307), supports {"dryRun": true}
│       ├── delete-session/              carryon admin delete <sid> --reason's backend (CO-307)
│       ├── sync-github-teams/           GitHub App auth + team membership → project_members reconciliation (CO-303)
│       ├── confirm-pull/                writes audit_log's "pull_complete" row once a real pull fully succeeds (CO-304) — not in Doc's original RPC list, see edge-functions.md
│       ├── get-session-transcript/      backs the web session viewer (CO-403) — viewer-role transcript chunks only, not get-manifest reused; writes audit_log's "view" row; see edge-functions.md
│       ├── get-session-family/          backs the session viewer's fork family tree (CO-404) — an iterative up-then-down walk over parent_session_id, not a recursive SQL CTE; see edge-functions.md
│       ├── rename-session/               `carryon rename <id> "<title>"`'s backend (CO-405) — the web app's own rename goes straight through a Server Action + RLS instead, see edge-functions.md
│       ├── resummarize-session/          the web app's own admin-only, rate-limited "Re-summarize" button backend (CO-408) — calls _shared/summarize.ts directly, same shape as end-session; see edge-functions.md
│       ├── send-daily-digest/            opt-in once-daily per-developer digest, one Slack message per opted-in project (CO-409); see edge-functions.md and notifications.md
│       ├── heartbeat/                    the agent's own periodic check-in, backing the admin "Agents" page (CO-410) — runs as the caller, upserts agent_heartbeats; see edge-functions.md
│       ├── search/                       semantic (hybrid vector + full-text) session search backing the web app's search box (CO-501) — embeds the query, then calls `search_sessions` via the CALLER's own client so RLS is the real enforcement; see edge-functions.md
│       ├── .env.example                 Edge Function secrets beyond Supabase's own auto-provided ones (CO-303: GITHUB_APP_*, CRON_SECRET; CO-306: RESCAN_CRON_SECRET; CO-307: PURGE_CRON_SECRET; CO-409: DIGEST_CRON_SECRET, WEB_APP_URL) — supabase/functions/.env (gitignored) for local dev
│       └── _shared/                    schema validation, has_role helper, github.ts (GitHub App JWT + installation token + team members, CO-303), storage.ts (resolveBucket — a project's dedicated bucket, or the shared default, CO-305), redact.ts (hand-kept TS port of internal/redact's rule patterns, CO-306), zstd.ts (the one function that reads/rewrites chunk bytes directly, CO-306), transcript-schema.ts (CO-406: hand-kept TS port of the web viewer's parse-transcript.ts schema), condense-transcript.ts (CO-406: the condensed-transcript builder), anthropic.ts (CO-407: the real Anthropic Messages API client, Claude Haiku), summarize.ts (CO-407: summarizeSession() — the AI title/summary job's actual logic, called from end-session, summarize-session, and resummarize-session, see edge-functions.md), slack.ts (CO-409: postToSlackWithRetry + the three Block Kit message builders — the one shared Slack-sending primitive, see notifications.md), embed.ts (CO-501: embedText() — wraps Supabase's own built-in gte-small model via Supabase.ai.Session, see docs/co-501-embedding-choice.md), supabase-ai.d.ts (CO-501: the ambient Supabase.ai.Session type declaration deno check needs — real only inside the deployed edge runtime, undefined under deno test)
│
├── web/                                Next.js app
│   ├── next.config.ts
│   ├── tailwind config, components.json, eslint config, etc. — directory defaults apply
│   └── src/
│       ├── app/                        routes — THIN, no business logic
│       │   ├── (auth)/login/page.tsx
│       │   ├── auth/callback/route.ts  exchanges the GitHub OAuth code for a session (CO-401)
│       │   ├── (dashboard)/
│       │   │   ├── layout.tsx          auth-gated shell: top bar, project switcher, user menu (CO-401)
│       │   │   ├── page.tsx            overview — CO-401 ships a bare project-list placeholder; CO-402 replaces it with the real per-project session list
│       │   │   ├── p/[project]/page.tsx  CO-401 ships a bare role-gated placeholder; CO-402 builds out the real session list here
│       │   │   ├── sessions/[id]/page.tsx  the real session viewer (CO-403): chat, collapsed tool calls, diffs, redaction highlighting, progressive chunk loading, virtualized
│       │   │   └── admin/              CO-410's real admin console, five pages — projects/, members/, audit/, agents/, config/, each gated by requireGlobalAdmin() directly (not a shared layout.tsx — see auth.md's own "404 gotcha" for why); admin/page.tsx just redirects to admin/projects
│       │   └── api/                    only when Server Actions genuinely don't fit — e.g. admin/audit/export/route.ts (CO-410): a real downloadable CSV file needs a Route Handler, not a Server Action; takes the exact same filter query params as admin/audit/page.tsx so the export always matches what's on screen
│       ├── modules/                    vertical slices, one per domain
│       │   ├── auth/                   sign-in form, user menu, sign-out action (CO-401) — no types.ts/schemas.ts of its own yet, nothing non-trivial to type beyond what shared/lib/auth.ts already exports
│       │   │   ├── data/actions.ts     signOut() — a 'use server' function bound straight to a <form action={...}>, not a table mutation; see layers.md's note on why ui/ may import this but not data/queries.ts
│       │   │   └── ui/{login-form,user-menu}.tsx
│       │   ├── sessions/
│       │   │   ├── types.ts, schemas.ts  schemas.ts (CO-405): renameSessionSchema/pinSessionSchema — mirrors rename-session's own Zod bounds, see edge-functions.md
│       │   │   ├── data/{queries,mutations,actions,keys}.ts   CO-402: project session list (filters, pagination); actions.ts (CO-405): renameSessionAction/pinSessionAction, the web app's own rename/pin path (direct Server Actions + RLS, not the rename-session Edge Function — that one's for the CLI); queries.ts's searchSessionsForProject (CO-501) is the project page's own entry point once `q` is set — calls the `search` Edge Function, then resolves ranked IDs back through the same RLS-scoped client listSessionsForProject uses, superseding that function's own plain full-text branch for a `q` filter
│       │   │   ├── lib/search-filters.ts                       CO-501: matchesSessionListFilters() — the dev/status/outcome/branch/date narrowing searchSessionsForProject applies over its ranked results, kept out of data/queries.ts (which imports 'server-only') so it stays unit-testable
│       │   │   ├── hooks/, ui/                                 CO-402: live-updating session table; sortItems (CO-405, in hooks/use-live-sessions.ts) re-sorts pinned-first on every realtime merge; ui/pin-button.tsx (CO-405): admin-only pin toggle; ui/session-table.tsx (CO-501): highlights a result's matchingPassage snippet when present
│       │   │   └── viewer/                                     CO-403: the session viewer, its own types/data/lib/hooks/ui — big enough a sub-feature to get its own subtree rather than crowd the module's top level
│       │   │       ├── types.ts
│       │   │       ├── data/queries.ts    getSessionTranscript() — calls get-session-transcript, collapses its 403/404 to notFound(); getSessionFamily() (CO-404) — calls get-session-family, collapses 403/404 to an empty tree (not notFound — getSessionTranscript already proved the session's real)
│       │   │       ├── lib/               parse-transcript.ts (the real Claude Code JSONL schema), zstd.ts (WASM decode, see next.config.ts's resolveAlias note), fetch-transcript.ts, correlate.ts (tool_use/tool_result pairing), tool-summary.ts, group-chunks.ts, build-family-tree.ts (CO-404: groups FamilyNodes by parentId for the tree UI)
│       │   │       ├── hooks/use-transcript.ts   progressive per-chunk loading
│       │   │       └── ui/                prose Markdown, redaction highlighting, collapsible tool calls + diffs, virtualized (@tanstack/react-virtual), side panel, subagent streams, family-tree.tsx (CO-404: the fork lineage tree, rendered in the side panel), rename-control.tsx (CO-405: renames a session from the side panel, shown only when the page's own canRename check — owner or admin — passes)
│       │   ├── projects/               types.ts, data/queries.ts (CO-401: listProjectsForCurrentUser, getProjectForCurrentUser), ui/project-switcher.tsx
│       │   └── admin/                  CO-410's five admin pages — types.ts, schemas.ts (looseUuid: Zod's own z.string().uuid() rejects seed.sql's non-RFC-strict fake IDs, see schemas.ts's own comment)
│       │       ├── data/queries.ts     every read here runs on the service-role client (createAdminClient()) — requireGlobalAdmin() already ran first, see auth.md
│       │       ├── data/actions.ts     registerProjectAction, updateProjectSettingsAction, add/update/removeMember*Action, updateRedactionConfigAction — every one logs its own audit_log row (logAdminAction)
│       │       ├── lib/csv.ts          auditLogToCsv() — the one place in the whole app that needs CSV, three lines of RFC 4180 escaping, no library
│       │       └── ui/                 admin-nav.tsx, admin-page-shell.tsx (a plain component, not a layout.tsx — see its own doc comment), register-project-form.tsx, project-settings-form.tsx, members-table.tsx, add-member-form.tsx, audit-filters.tsx (a plain GET <form>, no client JS), audit-table.tsx, agents-table.tsx, redaction-config-form.tsx
│       ├── shared/
│       │   ├── constants/{roles.ts, routes.ts}
│       │   ├── types/{database.types.ts, auth.ts, common.ts}  # database.types.ts is generated — DO NOT EDIT
│       │   ├── lib/{supabase/{client,server,admin}.ts, format/, auth.ts, utils.ts, edge-function.ts}  edge-function.ts (CO-403): server-side caller for Edge Functions outside the RLS-scoped table reads most of this app uses — same apikey/Authorization headers internal/api.Client.post uses
│       │   ├── node-builtin-stub.js    empty fs/path shim for zstd-codec's Emscripten bundle (CO-403) — see next.config.ts's turbopack.resolveAlias comment; not a real polyfill, the Node-only code path it stands in for is dead in a browser
│       │   └── ui/                     reusable presentational components, shadcn generated code
│       └── proxy.ts                    auth refresh, route protection — Next.js 16 renamed middleware.ts to proxy.ts (same mechanism); see node_modules/next/dist/docs/.../middleware.md. Also the real 404 enforcement for /p/[project], /sessions/[id], and /admin/* (CO-402/403/410) — see auth.md's "404 gotcha"
│
├── docs/                                long-form specs, requirements, decisions — e.g. docs/co-302-role-matrix.md
│
├── packaging/                           CO-308 — non-Go packaging inputs .goreleaser.yaml's own pipes read or that release.yml/a human runs directly
│   ├── deb/postinstall.sh               runs `carryon install` as the real (non-root) invoking user after `apt install`/`dpkg -i`
│   ├── winget/                          *.yaml.tmpl + generate.sh — winget manifest generation; submitting them is a deferred external step, see docs/co-308-fleet-rollout.md
│   └── managed-settings/                managed-settings.json — Claude Code enterprise-managed hooks for MDM fleets; deploying it to the real OS path is the user's own MDM tooling, out of this repo's scope
│
├── install.sh                           CO-308's `curl | sh` installer — downloads, verifies against checksums.txt, extracts, runs `carryon install`
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

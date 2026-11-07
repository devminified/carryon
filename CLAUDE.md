# Devminified Carryon — CLAUDE.md

This is the **always-loaded entrypoint**. It contains the non-negotiable rules and locked decisions.
Detailed rules live under `.claude/rules/` and are imported below — Claude Code follows the `@` references and reads them when relevant.

**Treat the rules as binding.** When in doubt, choose the option that preserves an invariant in "System invariants" below, even if it means writing more code.

This file adapts the directory-wide defaults in `~/Documents/GitHub/CLAUDE.md` to Carryon's actual shape: a Go laptop binary, a Supabase backend, and a Next.js web app, not a single Next.js monolith. Where this file is silent, the directory default applies to the `web/` app.

---

## Rules of Engagement

1. **MVP-first.** Ship the smallest thing that gets one teammate resuming another teammate's live session. Not the perfect platform. See the phase map in Doc.
2. **Vertical slices over horizontal layers.** One ticket end-to-end (migration → RLS → Edge Function → CLI/web UI → manual test) before starting the next.
3. **No speculative scale.** Build for the team today, not for "millions of users" (that tier exists only on paper in Doc §Scaling). Don't add partitioning, a message broker, or cell architecture before Tier 1 actually needs it.
4. **No premature abstractions.** Three similar lines is better than the wrong abstraction. Wait for the third real occurrence before extracting.
5. **Defence in depth, in the order the invariants name.** Laptop-side redaction first, RLS second, app-layer checks third. If a constraint can't be enforced in Postgres or in the redaction engine, it can't ship as a guarantee.
6. **Session transcripts and code snapshots are a hard constraint, same tier as PII.** A transcript can contain anything a developer pasted or Claude read. Never log one raw, never widen what leaves the laptop unredacted, never bypass RLS "to debug."
7. **"Done" has a definition.** Migration applied + RLS verified with a forbidden-role pgTAP test + CLI or UI exercised against local Supabase. Compiling code, or `go build` succeeding, is not done.
8. **Honest status reporting.** If something is partial, broken, or skipped, say so. Don't dress it up as complete.
9. **Update docs alongside code.** When a rule under `.claude/rules/` becomes wrong, fix it in the same change. Stale rules are worse than no rules.
10. **One PR per ticket.** Don't bundle a schema change with a CLI feature with a web page. Reviewers should be able to revert any single PR without untangling.

---

## Project Context

Carryon captures every Claude Code session a developer runs on a registered repo, uploads it (redacted) to Supabase as it happens, and lets a teammate pull it back down — code and conversation both — and resume exactly where it left off. See Doc for the product pitch, Doc for the 52-ticket build plan, and Doc for the full design (invariants, algorithms, scaling, security). Read the closest doc before guessing.

**Who uses it:** every Devminified developer (capture is automatic once a repo is registered), anyone pulling a teammate's session (`resumer`/`admin` roles), and project/CTO admins doing access review (`admin` role). Roles — `viewer`, `resumer`, `admin` — are per-project and mirrored from GitHub team membership; see `.claude/rules/auth.md`.

**In scope:** the `carryon` Go binary (hook, capture agent, CLI), the Supabase backend (schema, RLS, Edge Functions, Storage), the Next.js web app, AI summaries/search, fleet rollout, self-hosting, open-source release.

**Out of scope (for now):** agents other than Claude Code, editing sessions in the browser, live multiplayer sessions.

---

## System invariants — read before changing laptop or server code

These ten hold everywhere in the codebase. A PR that would break one gets rejected in review regardless of how it tests. Full rationale and enforcement mechanism: Doc §"Design principles and system invariants".

| ID | Invariant |
| --- | --- |
| I1 | The developer is never blocked — the hook only appends to a local log; all real work happens off that path. |
| I2 | Append-only everywhere — session files, the local queue, chunks, the audit log. Corrections are new versions, never mutations. |
| I3 | Effectively-once delivery — retries are safe; writes are idempotent, keyed by `(session_id, seq)`. |
| I4 | Integrity is verifiable end to end — per-chunk SHA-256, a hash chain, a contiguous watermark. |
| I5 | No secret leaves the laptop unredacted, for every pattern in the current rule set. |
| I6 | Tenant isolation — RLS on every row, tenant-scoped storage paths, short-lived signed URLs. |
| I7 | One writer per session — enforced with leases and fencing tokens. |
| I8 | Crash-safe state — atomic rename, write-ahead queue, commit-after-confirm. |
| I9 | The server is never in the data path for bytes — laptop and object storage talk directly via signed URLs. |
| I10 | Every component scales horizontally — no step depends on a single global lock or machine. |

---

## Tech Stack — Locked

| Layer | Choice |
| --- | --- |
| Laptop binary | **Go 1.23+**, one static binary per OS/arch, released with GoReleaser |
| CLI framework | **`spf13/cobra`** — `install`, `uninstall`, `login`, `hook`, `agent`, `status`, `ls`, `pull`, `export`, `rename` |
| Compression | **`klauspost/compress/zstd`**, level 3 |
| Secret detection | `internal/redact`'s own hand-ported gitleaks-v8-pattern rules (native `regexp`, no gitleaks library dependency — measured ~150-transitive-package cost for no benefit over a plain JSONL scan, see `.claude/rules/redaction.md`) + a Carryon path denylist |
| Background service | **`kardianos/service`** — launchd / systemd user unit / Windows Service |
| Credential storage | **`zalando/go-keyring`** — Keychain / Credential Manager / Secret Service |
| Git | the system `git` binary via `os/exec` |
| Database | **Supabase Postgres 15+** with `pgvector` |
| File storage | **Supabase Storage**, private bucket, signed URLs only |
| Server logic | **Supabase Edge Functions** (TypeScript, Deno) |
| Auth | **Supabase Auth**, GitHub OAuth + device flow for the CLI; roles mirrored from GitHub teams |
| Web app | **Next.js (App Router)**, `@supabase/ssr`, on Vercel or self-hosted |
| AI | **Anthropic API** (Claude Haiku) for titles/summaries |
| Package manager | **pnpm** for `web/` and `supabase/functions/`; Go modules for the binary |

For everything the directory default already locks (TypeScript strict, Tailwind, Zod at every boundary, ESLint/Prettier/Husky/commitlint, Vercel hosting) and this file doesn't override, that default applies to `web/`.

---

## Repository layout

```text
carryon/
├── cmd/carryon/          main.go: the one binary
├── internal/             hook, agent, queue, state, reader, redact, upload, snapshot, pull, claudepath, project, api, service
├── supabase/             migrations/, functions/ (Deno), seed.sql
├── web/                  Next.js app
├── test/fixtures/        sample sessions and repos
└── .github/workflows/    CI, release, nightly compat test
```

Full detail and the `web/src` tree: @.claude/rules/folder-structure.md

---

## The Five Architectural Principles (non-negotiable)

1. **Three trust zones, not one monolith.** Laptop (sensitive work: redact, compress, hash), server/cell (metadata + bytes + background jobs), third parties (GitHub, Anthropic, Slack — given only what they strictly need). Don't blur the line by, say, having the web app read a laptop's local queue file, or having the laptop call the Anthropic API directly.
2. **Bytes bypass the API.** Session and snapshot bytes flow laptop ↔ object storage directly over signed URLs (I9). API/Edge Functions only ever hand out metadata and signed URLs — never proxy the bytes themselves.
3. **RLS is the security boundary**, same as the directory default, with one addition: redaction on the laptop is the boundary RLS sits behind. A bug that lets an unredacted secret leave the laptop isn't fixed by tightening RLS afterward.
4. **Append-only, idempotent writes.** Any new write path (a chunk, a manifest, an audit row) must be safe to retry and must never mutate a byte once committed. If you're tempted to `UPDATE` a chunk row, you've modeled it wrong.
5. **The web app follows the directory default.** RSC reads, Server Action writes, Supabase client never holds the service-role key in a client component. Module code for `web/` lives under `web/src/`, scoped per concern (`sessions`, `projects`, `admin`), not spread across `app/`.

Full detail: @.claude/rules/architecture.md

---

## Anti-Patterns — Reject These on Sight

- **Any laptop code path that blocks on the network.** The hook only appends to a local log (I1); only the agent talks to the network.
- **Mutating a chunk, manifest, or audit row in place.** Append-only (I2) — write a new row/version instead.
- **An upload or Edge Function that isn't safe to retry.** Every write must be idempotent on `(session_id, seq)` or equivalent (I3).
- **A redaction rule change that isn't covered by the redaction corpus tests.** See `.claude/rules/redaction.md`.
- **Reading or writing Storage/Postgres without the tenant (`project_id`) in the query.** Tenant isolation (I6) is not optional.
- **A signed URL with no expiry, or longer than 5 minutes.**
- **The server proxying session bytes through an API route "just this once."** Breaks I9; use a signed URL.
- **An Edge Function trusting the client's role claim instead of `has_role()`.**
- **Disabling RLS.** Ever.
- **Skipping `with check` on `update`/`insert` policies.**
- **Editing `database.types.ts` by hand.** It's generated.
- **`any` types, the `enum` keyword, `useEffect` for first-render data, `{ data, error }` returns from `lib/` helpers, boolean-trap params** — same as the directory default, applies to `web/`.
- **Passing `--dangerously-skip-permissions` (or any permission-skipping flag) from `carryon pull`.** Claude Code's normal approval prompts must stay on for a resumed session (Doc §"Pulled transcripts are untrusted input").
- **Rendering a pulled transcript's content as raw HTML in the web viewer.** Sanitize Markdown, strict CSP, no raw HTML — a transcript contains text Claude read from the open web.
- **Bypassing commitlint with `--no-verify`.**
- **No AI/tool attribution in commits or PRs** — see `.claude/rules/git-workflow.md`.

---

## Detailed Rules (loaded on demand)

Architecture & structure:

- @.claude/rules/architecture.md — the three trust zones and the five principles in full
- @.claude/rules/folder-structure.md — canonical tree for the whole repo and for `web/src`
- @.claude/rules/go-conventions.md — Go binary conventions: cobra commands, error handling, context, channels
- @.claude/rules/module-checklist.md — steps to add a ticket's worth of work: migration, Edge Function, CLI command, or web page

Server & data:

- @.claude/rules/database-rls.md — schema, `tenant_id`, `has_role()`, pgTAP, compaction
- @.claude/rules/edge-functions.md — Edge Function conventions, the outbox pattern, queues
- @.claude/rules/auth.md — GitHub OAuth device flow, Supabase Auth, roles, `requireRole`
- @.claude/rules/redaction.md — the laptop redaction engine, the path denylist, the server-side rescan

Web app (`web/`):

- @.claude/rules/layers.md — module layering for `web/src/modules/{sessions,projects,admin}`
- @.claude/rules/data-fetching.md — Server Components, Server Actions, when TanStack Query earns its keep
- @.claude/rules/state-zustand.md — when client state is actually needed
- @.claude/rules/react-19-hooks.md — `useActionState`, `useOptimistic`, `use()`, `useFormStatus`
- @.claude/rules/tables.md — the sessions/projects list table
- @.claude/rules/forms.md — rename, consent/policy acknowledgment forms
- @.claude/rules/loading.md — Suspense, `loading.tsx`, skeletons
- @.claude/rules/notifications.md — Slack notify queue, fallback email/SMS

Conventions & ops:

- @.claude/rules/naming.md — files, components, hooks, DB, Go packages
- @.claude/rules/testing.md — go test, pgTAP, deno test, Vitest, Playwright, the fixture-writer e2e test
- @.claude/rules/qa-testing.md — phase-gate QA sweep SOP mapped to the NFR table
- @.claude/rules/tooling.md — ESLint, Prettier, Husky, commitlint, golangci-lint
- @.claude/rules/git-workflow.md — branch-per-ticket model, commit format
- @.claude/rules/commands.md — pnpm, go, and supabase CLI commands

---

## When You Are Unsure

If a request would force you to violate a rule here, in `.claude/rules/`, or an invariant in Doc, **say so and propose a compliant alternative** instead of bending the rule. The invariants exist because a crash, a retry, or a malicious transcript will eventually hit every code path that skips them.

If you cannot find an existing pattern that matches what you're building, **read the closest existing module first** — `internal/upload/` for a new laptop-side pipeline stage, `supabase/functions/register-chunk/` for a new Edge Function, `web/src/modules/sessions/` for a new web feature — and follow its shape.

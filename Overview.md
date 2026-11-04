# Devminified Carryon — Technical Requirements (TRD)

Nov 4, 2026 · @Devminified

The technical plan for building Carryon, the product described in Doc. It splits the work into five phases. Each phase has its own tab of tickets, and each ticket carries a description, testing points and validation points.

## Purpose and scope

This TRD turns the PRD into 52 buildable tickets across five phases, an estimated 12 weeks for two engineers. The PRD says *what* Carryon does and *why*. The TRD says *how* each piece is built, tested and accepted.

**In scope:** the `carryon` Go binary (hook, capture agent, CLI), the Supabase backend (schema, row-level security, Edge Functions, Storage), the Next.js web app, AI summaries and search, fleet rollout, self-hosting and the open-source release.

**Out of scope:** support for agents other than Claude Code (one research ticket only, CO-509), editing sessions in the browser, and live multiplayer sessions.

### How tickets are written

Every ticket on the phase tabs follows the same template:

| Field | What it holds |
| --- | --- |
| **ID · Title** | `CO-<phase><nn>`, for example `CO-107`. The ID is the branch name prefix (`co-107-capture-daemon`). |
| **Size** | S (≤ 1 day), M (2–3 days) or L (4–5 days). These are estimates. |
| **Depends on** | Tickets that must be merged first |
| **Description** | What to build, the interfaces it exposes, and the edge cases it must handle |
| **Testing points** | The automated and manual tests the engineer writes and runs before review |
| **Validation points** | Acceptance criteria the reviewer or QA ticks off before the ticket closes |

Testing points prove the code works. Validation points prove it meets the requirement. A ticket is closed only when every validation box is ticked.

## Phase map

&#91;embedded content: phase map · 5 phases, 5 gates\]

Phase 2 is highlighted because that's where Carryon first does its core job. Phases 3–5 make it safe, browsable and shareable. Phases 4 and 5 can overlap if a third engineer joins, since the web app depends only on CO-302.

## Technical stack and repository layout

One monorepo holds everything. A single Go binary runs on every laptop, and all server logic lives in Supabase.

| Layer | Technology | Notes |
| --- | --- | --- |
| Laptop binary | Go 1.23+, one static binary per OS and architecture | Released with GoReleaser for macOS (arm64, amd64), Linux (amd64, arm64) and Windows (amd64) |
| CLI framework | `spf13/cobra` | Commands: `install`, `login`, `hook`, `agent`, `status`, `ls`, `pull`, `export`, `rename` |
| Compression | `klauspost/compress/zstd` | Level 3, fast with a good ratio on JSON |
| Secret detection | gitleaks v8 `detect` package, used as a library | Same rules as the gitleaks CLI, plus a Carryon path denylist |
| Background service | `kardianos/service` | launchd on macOS, systemd user unit on Linux, Windows Service |
| Credential storage | `zalando/go-keyring` | macOS Keychain, Windows Credential Manager, Secret Service on Linux |
| Git | The system `git` binary, called through `os/exec` | Behaves the same as the developer's git, including credentials and hooks config |
| Database | Supabase Postgres 15+ with `pgvector` | Migrations in `supabase/migrations`, row-level security on every table |
| File storage | Supabase Storage, private bucket `carryon-sessions` | Accessed only through signed URLs |
| Server logic | Supabase Edge Functions (TypeScript, Deno) | `register-session`, `register-chunk`, `confirm-chunk`, `end-session`, `get-manifest`, `register-snapshot`, `summarize-session`, `rescan`, `sync-github-teams` |
| Auth | Supabase Auth with GitHub OAuth, device flow for the CLI | Project roles mirrored from GitHub teams |
| Web app | Next.js (App Router), `@supabase/ssr` | Hosted on Vercel or self-hosted with Node |
| AI | Anthropic API (Claude Haiku) for summaries | Embedding provider chosen in CO-501 |

```text
carryon/
├── cmd/carryon/          main.go: the one binary
├── internal/
│   ├── hook/             stdin JSON to queue append
│   ├── agent/            daemon loop, debounce, scheduler
│   ├── queue/            append-only queue file and compaction
│   ├── state/            per-session pointer files
│   ├── reader/           incremental JSONL reader
│   ├── redact/           gitleaks rules and path denylist
│   ├── upload/           zstd, signed-URL client, retries
│   ├── snapshot/         temp-index git snapshots, bundle fallback
│   ├── pull/             manifest, rebuild, rewrite, place, launch
│   ├── claudepath/       ~/.claude path encoding per OS
│   ├── project/          git root and remote normalization
│   ├── api/              Edge Function client
│   └── service/          service install per OS
├── supabase/
│   ├── migrations/       SQL schema and RLS policies
│   ├── functions/        Edge Functions (TypeScript)
│   └── seed.sql
├── web/                  Next.js app
├── test/fixtures/        sample sessions and repos
└── .github/workflows/    CI, release, nightly compat test
```

## Non-functional requirements

These targets apply to every phase. Tickets reference them by ID in their validation points.

| ID | Requirement | Target | How it is measured |
| --- | --- | --- | --- |
| NFR-01 | Hook latency | p99 under 50 ms. The hook always exits 0. | Benchmark in CI on each OS, plus agent telemetry |
| NFR-02 | Cloud lag on a live session | Server copy at most 2 minutes behind on a normal connection | Compare the local file size with `sessions.total_bytes` |
| NFR-03 | Agent footprint | Under 30 MB memory and under 1% average CPU | `carryon status` and an 8-hour soak test |
| NFR-04 | Durability | No lost bytes across crash, offline or reboot. The rebuilt file is byte-identical to the redacted source. | Chaos tests in CO-112 and CO-505 |
| NFR-05 | Idempotency | A retried upload never creates a duplicate chunk | Unique `(session_id, seq)`, plus a retry test |
| NFR-06 | Pull time | Under 60 s for a 50 MB session on a 50 Mbps link, excluding the first git clone | Timed e2e test |
| NFR-07 | Secret handling | No secret matched by the rule set leaves the laptop. Signed URLs live 5 minutes or less. All traffic over TLS. | Redaction corpus tests, plus the server rescan in CO-306 |
| NFR-08 | Access control | A user without the role gets zero rows or HTTP 403, in 100% of policy tests | pgTAP tests on every RLS policy |
| NFR-09 | Platforms | macOS 13+, Ubuntu 22.04+, Windows 11 | CI matrix |
| NFR-10 | Claude Code compatibility | One pinned version for the team. Any upgrade must pass the nightly resume test. | CO-503 |
| NFR-11 | Offline tolerance | Queue survives at least 7 days offline without data loss | Offline soak test |
| NFR-12 | Privacy scope | Nothing outside registered repos is ever uploaded | Negative tests in CO-108 |

## Environments, CI/CD and definition of done

### Environments

| Environment | Backend | Used for |
| --- | --- | --- |
| Local | `supabase start` (Docker) with `seed.sql` | Development and integration tests |
| Staging | Separate Supabase project, `carryon-staging` | Phase gate demos, dogfooding by the Carryon team |
| Production | Supabase project `carryon-prod` | All Devminified developers |

The binary reads its backend from `~/.carryon/config.toml` (`api_url`, `anon_key`), so the same build points at any environment, including a self-hosted one.

### CI on every pull request

- **Go:** `go vet`, `golangci-lint` and `go test -race ./...`, then cross-compiles all five targets.
- **Database:** `supabase db lint`, applies migrations to a fresh database, and runs pgTAP tests for every RLS policy.
- **Edge Functions:** `deno test`.
- **Web:** lint, type-check and unit tests.
- **End-to-end:** a Linux runner starts local Supabase, runs a fake Claude transcript writer (`test/fixtures/writer`) that appends lines and fires the hook, then checks the upload. A second user pulls the session and compares bytes.

### Release

GoReleaser runs on tags `v*`. It signs and notarizes macOS binaries, signs Windows binaries, and publishes to a Homebrew tap and winget. Edge Functions deploy with `supabase functions deploy` from the same tag. The web app deploys from `main`.

### Definition of done (every ticket)

- [ ] Code merged to `main` through a reviewed pull request
- [ ] All testing points automated where possible, and CI green
- [ ] Every validation point ticked by someone other than the author
- [ ] Logs and errors are actionable (what failed, which session, what to do)
- [ ] User-facing docs or runbook updated when behavior changes

## Ticket index

All 52 tickets in build order. Set each ticket's status here. Its full description, testing points and validation points are on its phase tab. Sizes total roughly 24 engineer-weeks, which is about 12 weeks for two engineers.

| ID | Ticket | Phase | Size | Depends on | Status |
| --- | --- | --- | --- | --- | --- |
| CO-101 | Monorepo and Go module scaffold | 1 | S | – | Not started |
| CO-102 | Supabase project and base schema | 1 | M | – | Not started |
| CO-103 | `carryon login` with GitHub device flow | 1 | M | 101, 102 | Not started |
| CO-104 | Project registry and remote normalization | 1 | S | 102 | Not started |
| CO-105 | `carryon hook` command | 1 | S | 101 | Not started |
| CO-106 | `carryon install` (hooks and settings) | 1 | M | 105 | Not started |
| CO-107 | Capture agent daemon | 1 | L | 103, 105 | Not started |
| CO-108 | Project filter | 1 | S | 104, 107 | Not started |
| CO-109 | Incremental reader and pointer state | 1 | M | 107 | Not started |
| CO-110 | Chunk upload pipeline | 1 | L | 102, 103, 109 | Not started |
| CO-111 | Session record lifecycle | 1 | S | 110 | Not started |
| CO-112 | Offline queue and retry | 1 | M | 110 | Not started |
| CO-113 | `carryon status` and logs | 1 | S | 107, 112 | Not started |
| CO-201 | Code snapshot capture | 2 | M | 110 | Not started |
| CO-202 | Git bundle fallback | 2 | S | 201 | Not started |
| CO-203 | `get-manifest` Edge Function | 2 | M | 110, 201 | Not started |
| CO-204 | `carryon ls` | 2 | S | 203 | Not started |
| CO-205 | Session rebuild | 2 | M | 203 | Not started |
| CO-206 | Worktree restore | 2 | M | 201, 202 | Not started |
| CO-207 | Path rewrite | 2 | M | 205 | Not started |
| CO-208 | Session ID rewrite and fork registration | 2 | S | 207 | Not started |
| CO-209 | Claude path encoder and file placement | 2 | S | 208 | Not started |
| CO-210 | Launch, header and version check | 2 | S | 209 | Not started |
| CO-211 | `carryon pull` end to end, plus `export` | 2 | M | 203–210 | Not started |
| CO-301 | Redaction engine | 3 | L | 110 | Not started |
| CO-302 | Roles and row-level security | 3 | L | 102 | Not started |
| CO-303 | GitHub team sync | 3 | M | 302 | Not started |
| CO-304 | Audit log | 3 | S | 302 | Not started |
| CO-305 | Storage hardening | 3 | S | 302 | Not started |
| CO-306 | Server-side secret rescan | 3 | M | 301 | Not started |
| CO-307 | Retention and deletion | 3 | M | 302, 304 | Not started |
| CO-308 | Fleet rollout and auto-update | 3 | L | 106, 113 | Not started |
| CO-309 | Consent and policy acknowledgment | 3 | S | 103 | Not started |
| CO-401 | Web app scaffold and sign-in | 4 | M | 302 | Not started |
| CO-402 | Project and session list | 4 | M | 401 | Not started |
| CO-403 | Session viewer | 4 | L | 402 | Not started |
| CO-404 | Fork family tree | 4 | S | 403 | Not started |
| CO-405 | Rename and pin | 4 | S | 403 | Not started |
| CO-406 | Condensed transcript builder | 4 | M | 205 | Not started |
| CO-407 | AI title and summary job | 4 | L | 406 | Not started |
| CO-408 | Summary display and re-run | 4 | S | 402, 407 | Not started |
| CO-409 | Slack notifications | 4 | M | 407 | Not started |
| CO-410 | Admin pages | 4 | M | 302–304 | Not started |
| CO-501 | Semantic search | 5 | L | 407 | Not started |
| CO-502 | "Who touched this file" lookup | 5 | S | 407 | Not started |
| CO-503 | Claude Code compatibility CI | 5 | M | 211 | Not started |
| CO-504 | Observability | 5 | M | 113, 410 | Not started |
| CO-505 | Load, soak and cost test | 5 | M | 211, 407 | Not started |
| CO-506 | Self-hosted deployment | 5 | L | 410 | Not started |
| CO-507 | Security review and threat model | 5 | M | 301–308 | Not started |
| CO-508 | Open-source release | 5 | M | 506, 507 | Not started |
| CO-509 | Research: other coding agents | 5 | M | 211 | Not started |

# Devminified Carryon — Design & Runbook

Nov 4, 2026 · @Devminified

## Summary

Carryon copies every Claude Code session from developer machines to company storage while the developer works, so anyone with project access can pull a session and resume it on their own machine without asking the developer.

Today a session exists only as a file on one laptop: `~/.claude/projects/<encoded-path>/<session-id>.jsonl`. If the developer is offline, the session is unreachable. Claude Code also deletes these files after 30 days by default ([Claude Code docs](https://code.claude.com/docs/en/claude-directory)), so the history is lost even for the developer.

Carryon has three parts:

1. **Capture agent** on each developer machine. It uploads the session file and a snapshot of the code, including uncommitted work, after every Claude reply.
2. **Carryon server.** Postgres stores the index and permissions, and object storage holds the session files and code snapshots.
3. **Pull CLI** (`carryon pull <session>`). It restores the matching code, places the session file where Claude Code looks for it, and resumes it as a fork.

**Decided on 4 Nov 2026:**

- **Name:** Devminified Carryon. The CLI command is `carryon`.
- **Stack:** the agent and CLI are one Go binary, and the backend is Supabase.
- **Scope:** only sessions inside registered company repos are uploaded.

The gap this fills: tools like Entire tie sessions to commits. An urgent handoff usually happens before anything is committed, so Carryon also captures the uncommitted state.

## Architecture

The capture agent pushes and the pull CLI fetches. Both talk only to the Carryon API, which checks permissions and then hands out short-lived signed URLs for the files in object storage.

&#91;embedded content: system overview · laptop to Supabase to teammate\]

Read it left to right. The hook only drops a note in a local queue, and the capture agent does all the network work. It registers the chunk with the Carryon API, uploads it straight to Storage over a signed URL, and pushes the code snapshot to your git remote. `carryon pull` reads all three back on the teammate's side.

| Component | Runs on | What it does | Suggested stack |
| --- | --- | --- | --- |
| Capture hook | Dev machine, inside Claude Code | Fires after every reply and at session end, then hands the event to the agent and returns at once | Shell script in `settings.json` |
| Capture agent | Dev machine, background | Uploads the session file and a code snapshot, redacts secrets, retries when offline | Go, single binary |
| Carryon API | Company server | Login, project permissions, signed upload and download URLs, audit log | Supabase Edge Functions (TypeScript) + Postgres + Auth |
| Object storage | Company server | Holds the session files and code snapshots | Supabase Storage, S3, Cloudflare R2 or MinIO |
| Web UI | Browser | Lists sessions by project, developer and date, shows each session read-only, copies the pull command | Next.js on the same API |
| Pull CLI | Any team member's machine | Restores the code and the session, then starts Claude Code | Same Go binary as the agent |

## Database recommendation

Use object storage for the session files and Postgres for everything else. Supabase bundles both, plus login and per-row permissions, so it is the fastest way to start. Self-host it, or use plain Postgres with MinIO, if a client contract requires that data stay on your own servers.

| Option | Use it for | Why or why not |
| --- | --- | --- |
| Object storage (Supabase Storage, S3, R2, MinIO) | The session files and code snapshots: the source of truth | Resume needs the exact file back, byte for byte. Files run from a few KB to tens of MB and only grow, and blob storage handles that cheaply. |
| Postgres (Supabase) | The index: projects, members, sessions, uploads, audit log | Relational data with permissions. Row-level security enforces "only project members can see this session". |
| pgvector (inside the same Postgres) | Phase 3: search such as "which session touched the payment retry logic?" | Embeddings are lossy, so you can't resume a session from them. Use them for search only, never as the primary store. |
| Time-series DB | Not needed | Sessions are documents, not metrics. Nothing here needs time-window aggregation. |

**Upload strategy.** Session files only grow, with one line added per message. The agent remembers the last byte it uploaded and sends only the new bytes as a chunk. On pull, the server joins the chunks back into one file. If a file ever shrinks or its earlier bytes change, the agent uploads the whole file again as a new version.

**Storage layout:** `carryon/<project>/<user>/<session-id>/chunk-<seq>.jsonl.zst`, plus `carryon/<project>/<user>/<session-id>/code-<seq>.bundle` for code snapshots.

```sql
create table projects (
  id uuid primary key default gen_random_uuid(),
  name text not null,
  git_remote text not null unique          -- e.g. git@github.com:devminified/acme-api.git
);

create table project_members (
  project_id uuid references projects on delete cascade,
  user_id uuid references auth.users on delete cascade,
  role text check (role in ('viewer','resumer','admin')) not null,
  primary key (project_id, user_id)
);

create table sessions (
  id uuid primary key,                     -- Claude Code's own session_id
  project_id uuid references projects not null,
  user_id uuid references auth.users not null,
  machine text,
  original_cwd text not null,              -- needed to rewrite paths on pull
  branch text,
  head_sha text,
  claude_code_version text,
  title text,                              -- /rename name, else AI-written title
  status text check (status in ('live','ended')) default 'live',
  started_at timestamptz default now(),
  last_activity_at timestamptz default now(),
  total_bytes bigint default 0,
  parent_session_id uuid references sessions,  -- set when this session is a pulled fork
  summary text,                            -- AI-written, see Session lifecycle
  outcome text check (outcome in ('done','in_progress','blocked')),
  files_touched text[],
  embedding vector(1024)                   -- pgvector, phase 3; size depends on the embedding model
);

create table session_chunks (
  session_id uuid references sessions on delete cascade,
  seq int not null,
  byte_start bigint not null,
  byte_end bigint not null,
  object_key text not null,
  sha256 text not null,
  uploaded_at timestamptz default now(),
  primary key (session_id, seq)
);

create table code_snapshots (
  session_id uuid references sessions on delete cascade,
  seq int not null,
  head_sha text not null,
  wip_ref text,                            -- refs/carryon/<user>/<session-id> on your git remote
  object_key text,                         -- git bundle fallback
  taken_at timestamptz default now(),
  primary key (session_id, seq)
);

create table audit_log (
  id bigserial primary key,
  actor uuid references auth.users,
  action text not null,                    -- upload | view | pull | delete
  session_id uuid,
  at timestamptz default now()
);
```

## Capture: getting sessions off the laptop

A Claude Code hook triggers every upload. Each hook event receives `session_id`, `transcript_path` and `cwd` as JSON on stdin ([hooks reference](https://docs.anthropic.com/zh-CN/docs/claude-code/hooks)). The hook only records the event. The background agent does the upload, so Claude Code never waits on the network.

&#91;embedded content: chunked upload · 8-step cycle\]

Each reply adds a few lines to the file, and the agent sends only the lines after the last uploaded offset. A ten-hour session therefore uploads in the same small pieces as a ten-minute one, and no single upload ever approaches Supabase's file-size limit. On pull, the chunks are downloaded in `seq` order, checked against their SHA-256 and joined back into the same file.

### 1. Settings on every developer machine

Roll this out to `~/.claude/settings.json` with your device management tool, or put it in Claude Code's managed settings so developers can't remove it:

```json
{
  "cleanupPeriodDays": 3650,
  "hooks": {
    "Stop":       [{ "hooks": [{ "type": "command", "command": "carryon hook",         "timeout": 5 }] }],
    "SessionEnd": [{ "hooks": [{ "type": "command", "command": "carryon hook --final", "timeout": 5 }] }]
  }
}
```

- `Stop` fires after every Claude reply, which keeps the server copy minutes behind the laptop at most.
- `SessionEnd` fires when the session closes and marks it `ended`.
- `cleanupPeriodDays` stops Claude Code from deleting the local transcripts after 30 days.

### 2. What `carryon hook` does (under 50 ms)

It reads the JSON from stdin, appends one line to `~/.carryon/queue.jsonl` and exits 0. It never calls the network and never fails the hook.

### 3. What the agent does (runs as a launchd or systemd service)

1. **Filters by project.** It finds the git root of `cwd` and compares the remote to the registered projects. Anything outside a registered repo stays on the laptop and is never uploaded: personal folders, unregistered repos, and quick questions asked from the home folder. The agent logs an unregistered repo once so an admin can add it.
2. **Debounces.** It uploads at most once per session every 60 seconds, plus a final upload on `SessionEnd`.
3. **Reads only the new bytes** since the last uploaded offset. It also picks up subagent transcripts in the session's `subagents/` folder.
4. **Redacts secrets** in those bytes (step 4).
5. **Compresses** with zstd, asks the API for a signed upload URL, uploads the chunk and records it in `session_chunks`.
6. **Snapshots the code** if the working tree changed since the last snapshot (step 5).
7. **Queues while offline** and retries with backoff. Nothing is lost if the laptop drops off Wi-Fi.

### 4. Redaction (before anything leaves the machine)

Run secret-detection rules over every chunk, such as the open-source gitleaks rule set (AWS keys, GitHub tokens, private keys, connection strings, JWTs). Replace each match with `[REDACTED:<rule>]`. Also redact the tool output of any read of `.env`, `*.pem`, `id_rsa` or other files on a denylist. The unredacted original never leaves the laptop. A resumed session therefore sees placeholders where the secrets were, which is the intended behavior.

### 5. Code snapshot, including uncommitted work

An urgent handoff usually happens before the developer commits. The agent records the exact working tree as a hidden git ref. It doesn't touch the developer's branch, index or stash:

```bash
cd "$REPO_ROOT"
export GIT_INDEX_FILE="$(mktemp)"
git read-tree HEAD
git add -A                                   # tracked + untracked, respects .gitignore
TREE=$(git write-tree)
COMMIT=$(git commit-tree "$TREE" -p HEAD -m "carryon snapshot $SESSION_ID")
git push -q origin "$COMMIT:refs/carryon/$DEV/$SESSION_ID" --force
rm -f "$GIT_INDEX_FILE"
```

Because `.gitignore` is respected, `.env` files and `node_modules` stay out. The ref sits outside `refs/heads`, so it doesn't appear as a branch. Check that your git host accepts pushes to custom ref namespaces. If it doesn't, the agent runs `git bundle create` and uploads the bundle to object storage instead (`code_snapshots.object_key`).

### 6. Why the developer never notices

Claude Code and the agent are separate processes. They share a file but never wait on each other.

- **The hook is the only thing Claude Code waits for.** It writes one line to `~/.carryon/queue.jsonl` in about 50 ms and exits. A 5-second timeout kills it if it ever hangs.
- **The queue is read first in, first out, but events are merged.** Ten replies within the 60-second debounce become one upload.
- **A pointer file per session**, `~/.carryon/state/<session-id>.json`, holds `{offset, next_seq}`. The agent opens the session file read-only and reads from the offset to the last complete line, so it skips any half-written line. It moves the pointer only after Supabase confirms the chunk was saved.
- **Failures repeat safely.** If an upload fails, the pointer hasn't moved and the same slice is sent again. Because `(session_id, seq)` is unique, a retry overwrites itself instead of creating a duplicate.
- **No git contention.** The snapshot uses a temporary index file, so it never takes `.git/index.lock`. The agent also runs at low CPU priority.

## Session lifecycle and AI summaries

Every session in a registered project is stored automatically, never overlaps another, and gets an AI-written title and summary, so the team can browse sessions without pulling them.

### What gets stored

The session file is the complete conversation, and it is exactly what Claude Code reloads on resume:

| In the session file | Example |
| --- | --- |
| Every developer message | "Add retry logic to the payment webhook" |
| Every Claude reply | Its explanation and plan |
| Every tool call | Read `src/payments/webhook.ts`, ran `npm test`, edited 3 files |
| Every tool result | The file contents Claude saw, test output, diffs |

Three things live outside the session file. The code state travels separately as the git snapshot. The developer's personal memory (`~/.claude/CLAUDE.md`, `CLAUDE.local.md`, auto-memory) does not travel: the person pulling gets their own instead, so project-wide rules belong in the repo's `CLAUDE.md`. MCP servers and settings travel only if they are committed in the repo's `.claude/` folder.

### Which sessions are stored

| Where the session ran | What happens |
| --- | --- |
| Inside a registered company repo | Uploaded automatically, visible to that project's team |
| Inside an unregistered git repo | Not uploaded. Logged once so an admin can register it. |
| Outside any repo (home folder, quick questions) | Not uploaded. Stays on the laptop. |

### Why sessions never overlap

- Claude Code gives every session a random UUID. That UUID is the primary key and the session's folder in Storage, so two sessions can never collide.
- Chunks are keyed by `(session_id, seq)`, so a retried upload replaces itself.
- A pulled session gets a new UUID with `parent_session_id` pointing to the original. The original and the fork grow separately, and the web UI shows them as a family tree.

### AI title and summary

- **When it runs:** a Supabase Edge Function runs when a session ends, and every 30 minutes on long live sessions.
- **What it reads:** a condensed transcript of the developer's prompts, Claude's final replies, and the list of files touched and commands run. It skips bulky tool output. Everything was already redacted on the laptop.
- **Model:** a small, fast Claude model (Haiku). Long sessions are summarized incrementally: the previous summary plus the new chunks produce the updated summary, so cost does not grow with length.
- **What it writes:** a `title` (a developer's `/rename` always wins), a 3–5 line `summary`, an `outcome` (done, in progress or blocked), `files_touched`, and later an `embedding` for search.

The result is a project page listing each developer's sessions with a title and two-line summary, newest first. Anyone with access can read a session in the browser. You pull only to continue the work.

## Pull and resume: the runbook

One command does it: `carryon pull <session-id>`. It checks out the developer's exact code in a separate folder, puts the session file where Claude Code looks for it, and opens Claude on a copy of the session.

### For the person pulling

1. `carryon login` the first time. It signs in with GitHub, and your project roles come from Carryon.
2. Find the session in the web UI (filter by project, developer, *live* or *ended*), or run `carryon ls --project acme-api --dev ali`.
3. Run `carryon pull 7c1e…`. Claude Code opens in the restored folder with the full conversation loaded.
4. Ask your questions or continue the work. To keep changes, create a branch: `git switch -c hotfix/payment-retry`, then commit and push as usual.

### What `carryon pull` does, step by step

1. **Checks access** (role `resumer` or higher) and writes a `pull` row to the audit log.
2. **Restores the code in its own worktree**, so your own checkout is untouched:

   ```bash
   git fetch origin "refs/carryon/ali/$SID:refs/carryon/ali/$SID"
   git worktree add --detach "$HOME/carryon/acme-api-${SID:0:8}" "refs/carryon/ali/$SID"
   ```

   If there is no snapshot ref, it checks out `head_sha` and warns that uncommitted work wasn't captured.
3. **Rebuilds the session file.** It downloads every chunk, checks each SHA-256, decompresses them and joins them in `seq` order.
4. **Rewrites the paths.** Every occurrence of the developer's `original_cwd` (for example `/Users/ali/work/acme-api`) becomes the new worktree path. Without this, Claude would try to open files that don't exist on your machine.
5. **Assigns a new session ID.** It replaces every `sessionId` value with a fresh UUID. If two copies of a transcript share an ID, Claude Code can report the session as not found ([why](https://www.memorylake.ai/en/blogs/export-claude-code-session-to-another-machine)).
6. **Places the file where Claude Code looks for it:**

   `~/.claude/projects/<encoded-path>/<new-session-id>.jsonl`

   `<encoded-path>` is the absolute folder path with every non-letter, non-digit character replaced by `-`. For example, `/Users/sara/carryon/acme-api-7c1e9a2b` becomes `-Users-sara-carryon-acme-api-7c1e9a2b`.
7. **Starts Claude:** `cd <worktree> && claude --resume <new-session-id> --fork-session`. The fork keeps the imported copy pristine, so you can pull it again and start fresh.
8. **Prints a header**, for example: *Ali · feature/payments · last activity 18:42 · 3 uncommitted files · Claude Code 2.x.y*.

### Doing it by hand (if the CLI is broken)

| Step | Command or location |
| --- | --- |
| Session file | Download it from the web UI (it joins the chunks for you) |
| Code | `git fetch origin refs/carryon/<dev>/<sid>` and then `git worktree add --detach <dir> FETCH_HEAD` |
| Path rewrite | `sed -i 's#/Users/ali/work/acme-api#/Users/sara/carryon/acme-api-7c1e#g' session.jsonl` |
| Drop it in | `~/.claude/projects/-Users-sara-carryon-acme-api-7c1e/<uuid>.jsonl` |
| Resume | `cd <dir> && claude --resume <uuid> --fork-session` |

If the session used MCP servers, plugins or extra directories, they have to exist on your machine too. When they're committed in the repo's `.claude/` folder, the worktree brings them along.

## Access, security and culture

A session holds everything a developer and Claude saw, so treat Carryon like your production database, not like a shared drive.

### Who can do what

| Role | Sees the session list | Reads transcripts | Pulls and resumes | Deletes |
| --- | --- | --- | --- | --- |
| viewer | Yes | Yes | No | No |
| resumer | Yes | Yes | Yes | No |
| admin (CTO, project lead) | Yes | Yes | Yes | Yes, with a reason in the audit log |

- Roles are set per project and mirrored from GitHub teams, so leaving the project team removes access.
- Postgres row-level security enforces the roles. Files are served only through signed URLs that expire after 5 minutes.
- Every view, pull and delete goes into `audit_log`, and developers can see who pulled their sessions.
- Encrypt at rest (the storage default) and use a separate bucket per client if contracts require it.

### Rules to roll out with it

- **Tell people first.** Announce in writing that Claude Code sessions on company projects are stored and can be resumed by the project team. Add it to the onboarding checklist and get sign-off. Check employment and client contracts, and local privacy law, before switching it on.
- **Company projects only.** The agent uploads only registered repos. Personal experiments stay local.
- **No secrets in prompts.** Redaction is a safety net, not a licence to paste keys.
- **Name your sessions.** Run `/rename payments-retry-fix` at the start so others can find the session. The web UI shows the name.
- **Pull is a fork, never a takeover.** The original developer's session is never modified, and they keep working on their own copy.
- **Retention:** keep sessions for the life of the project plus 90 days, then delete them in bulk with an audit entry.

## Limits and risks

| Risk | What happens | Mitigation |
| --- | --- | --- |
| Claude Code changes its session file format | Pulled sessions fail to resume after an update | Pin the Claude Code version across the team. Store `claude_code_version` with every session. Run a nightly CI test that resumes a fixture session with the current version. Upgrade only when the test passes. |
| Code drifted after the snapshot | Claude describes files as they were at the last snapshot | Always resume in the snapshot worktree. The pull header shows the snapshot time. |
| Mid-tool-call capture | The last tool call has no result in the pulled copy | Claude continues without that output. Rerun the step if it matters. |
| Very long sessions | Resume reloads everything, which costs tokens, and auto-compaction loses detail | One session per task. Encourage `/rename` and fresh sessions per ticket. |
| A secret slips past redaction | The secret is stored on the server | Run a periodic server-side rescan. On a hit, rotate the key, purge the chunk and log it. |
| The git host rejects custom refs | No uncommitted-work snapshot | The agent falls back to a git bundle in object storage. |

## Build plan

One engineer can get a working pull in about two weeks. These durations are estimates, not commitments. Ship Phase 1 on a single project with you as the first user before anyone else sees it.

&#91;embedded content: build plan · 3 phases, 2 gates\]

Each gate is a real test. If you can't resume a teammate's live session from your own machine, nothing in Phase 2 is worth building yet.

## Sources

- [Claude Code hooks reference: hook input fields](https://docs.anthropic.com/zh-CN/docs/claude-code/hooks)
- [Claude Code directory and cleanupPeriodDays](https://code.claude.com/docs/en/claude-directory)
- [Why you cannot copy a Claude Code session to another machine](https://dev.to/gowtham-sai-yadav/why-you-cannot-copy-a-claude-code-session-to-another-machine-2008)
- [Exporting a Claude Code session: the duplicate-ID problem](https://www.memorylake.ai/en/blogs/export-claude-code-session-to-another-machine)
- [Entire CLI: commit-linked session capture](https://github.com/entireio/cli)
- [claude-teleport: session transfer with path rewriting](https://pkg.go.dev/github.com/gowtham-sai-yadav/claude-teleport)

# Devminified Carryon — TRD Tickets (Phases 1–5)

All 52 tickets with full description, testing points and validation points. Companion to `Overview.md` (TRD overview, NFRs, ticket index).

---

# Phase 1 — Foundation and capture

**Goal:** every Claude Code session inside a registered repo reaches Supabase automatically, without the developer noticing. **Duration:** weeks 1–2 (estimate). **Gate:** a real session from one developer appears in Supabase within 2 minutes, and its rebuilt file is byte-identical to the laptop's (redaction comes in Phase 3).

---

### CO-101 · Monorepo and Go module scaffold

**Size:** S · **Depends on:** none

**Description.** Create the `carryon` monorepo with the layout from the Overview tab. Set up the Go module, the `cobra` root command with empty subcommands (`install`, `login`, `hook`, `agent`, `status`, `ls`, `pull`, `export`, `rename`) and `carryon version`, which prints the version, commit and build date injected by `-ldflags`. Add GitHub Actions for lint, test and cross-compile, and a GoReleaser config that builds snapshots only for now.

**Testing points**

- `go build ./...` and `go test ./...` pass on macOS, Linux and Windows runners
- `golangci-lint` passes with the agreed config (errcheck, govet, staticcheck, gosec)
- `carryon --help` lists every subcommand, and each prints "not implemented" with exit code 2
- `goreleaser build --snapshot` produces all five targets

**Validation points**

- [ ] A new engineer can clone, build and run `carryon version` in under 10 minutes using only the README
- [ ] CI runs on every pull request and blocks merge on failure
- [ ] Binary size is under 25 MB per target

---

### CO-102 · Supabase project and base schema

**Size:** M · **Depends on:** none

**Description.** Create the staging and production Supabase projects. Write migrations for `projects`, `project_members`, `sessions`, `session_chunks`, `code_snapshots` and `audit_log`, as defined in the PRD, including `parent_session_id`, `summary`, `outcome` and `files_touched` (leave `embedding` for CO-501). Add indexes on `sessions(project_id, last_activity_at desc)`, `sessions(user_id)` and `session_chunks(session_id, seq)`. Create the private Storage bucket `carryon-sessions`. Enable RLS on every table with deny-all defaults. Real policies arrive in CO-302, and Edge Functions use the service role until then.

**Testing points**

- Migrations apply cleanly to an empty database and are idempotent on re-run in CI
- Inserting a duplicate `(session_id, seq)` fails with a unique violation
- Deleting a session cascades to its chunks and snapshots
- With the anon key, every table returns zero rows (deny-all check)
- The bucket rejects unauthenticated reads and writes

**Validation points**

- [ ] Schema matches the PRD column for column (reviewed side by side)
- [ ] `supabase db diff` against staging shows no drift
- [ ] Seed data creates one project, two users and one member role for local development

---

### CO-103 · `carryon login` with GitHub device flow

**Size:** M · **Depends on:** CO-101, CO-102

**Description.** Enable the GitHub provider in Supabase Auth. Implement `carryon login`: it starts the OAuth device flow, prints the code and URL, opens the browser if possible, polls until approved, and stores the refresh token in the OS keychain (`go-keyring`, service `carryon`). Access tokens refresh automatically. `carryon logout` deletes the token. The agent and CLI share one token provider that returns a valid access token or a typed `ErrNotLoggedIn`.

**Testing points**

- Unit tests for token refresh, expiry and a revoked refresh token, using a mock auth server
- The token never appears in logs, config files or process arguments (grep the test logs)
- Headless Linux with no Secret Service falls back to a 0600 file at `~/.carryon/credentials`, with a warning
- `carryon login` while already logged in shows the current user and asks before replacing

**Validation points**

- [ ] A developer logs in on macOS, Windows and Linux in under 1 minute
- [ ] After logout, the agent stops uploading and `carryon status` says "not logged in"
- [ ] Revoking the GitHub app authorization makes the next refresh fail cleanly

---

### CO-104 · Project registry and remote normalization

**Size:** S · **Depends on:** CO-102

**Description.** Admins register a project with its git remote. Implement `NormalizeRemote(url) string` in `internal/project`. It maps `git@github.com:org/repo.git`, `https://github.com/org/repo`, `ssh://git@github.com/org/repo.git` and trailing-slash variants to one canonical form, `github.com/org/repo`, all lowercase. Store the canonical form in `projects.git_remote`. Add an admin CLI `carryon admin project add <name> <remote>` for now. The web UI replaces it in CO-410. Add an Edge Function `match-project` that returns the project ID for a canonical remote if the caller is a member.

**Testing points**

- Table-driven tests for at least 12 remote URL formats, including GitLab, Bitbucket and self-hosted hosts with ports
- Two spellings of the same repo resolve to the same project
- `match-project` returns 404 for an unregistered remote and 403 for a non-member

**Validation points**

- [ ] All active Devminified repos can be registered without manual URL edits
- [ ] A fork with a different owner does not match the original project

---

### CO-105 · `carryon hook` command

**Size:** S · **Depends on:** CO-101

**Description.** Reads the hook JSON from stdin (`session_id`, `transcript_path`, `cwd`, `hook_event_name`). It appends one line `{session_id, transcript_path, cwd, event, ts}` to `~/.carryon/queue.jsonl` using `O_APPEND` and a single `write` call, so concurrent hooks never interleave. `--final` marks a `SessionEnd`. It must never block Claude Code: no network, no git, no locks beyond the append. It exits 0 on every path, including bad input, a missing directory or a full disk. Errors go to `~/.carryon/hook-errors.log`, capped at 1 MB.

**Testing points**

- Benchmark: p99 under 50 ms on all three OS runners (NFR-01)
- 200 concurrent hook invocations produce 200 intact queue lines
- Malformed JSON, empty stdin and an unwritable home folder all still exit 0
- Lines are under 4 KB each, so writes stay atomic

**Validation points**

- [ ] A developer runs Claude Code for an hour with the hook installed and notices no delay
- [ ] The queue line count matches the number of Claude replies in that hour

---

### CO-106 · `carryon install` (hooks and settings)

**Size:** M · **Depends on:** CO-105

**Description.** Safely merges into `~/.claude/settings.json`: a `Stop` hook running `carryon hook`, a `SessionEnd` hook running `carryon hook --final`, both with timeout 5, and `cleanupPeriodDays: 3650`. It preserves every other key and every existing hook, backs up the original to `settings.json.carryon-bak-<timestamp>`, and is idempotent (running it twice adds nothing). It also installs and starts the background service (CO-107), and creates `~/.carryon/` with mode 0700. `carryon uninstall` reverses all of this.

**Testing points**

- Golden-file tests: an empty settings file, existing unrelated hooks, an existing `Stop` hook from another tool, invalid JSON (refuse and explain), and a read-only file
- Running install twice produces identical files
- Uninstall restores the exact pre-install content when nothing else changed

**Validation points**

- [ ] After install, `/hooks` in Claude Code lists both Carryon hooks
- [ ] A developer's existing custom hooks still run
- [ ] Install plus login takes under 3 minutes on a fresh laptop

---

### CO-107 · Capture agent daemon

**Size:** L · **Depends on:** CO-103, CO-105

**Description.** `carryon agent` runs as a background service: launchd on macOS, a systemd user unit on Linux and a Windows Service, via `kardianos/service`. The main loop tails `queue.jsonl` from a saved cursor. It groups events into a per-session dirty set, and runs at most one upload per session every 60 seconds (configurable), plus an immediate one on `SessionEnd`. A worker pool of 2 uploads different sessions in parallel. The queue file is compacted when every line before the cursor has been processed. The service runs at reduced priority (`nice 10`, or Windows below-normal) and restarts on crash.

**Testing points**

- Ten events for one session within 60 seconds produce exactly one upload
- A `SessionEnd` triggers an upload within 5 seconds
- Killing the agent mid-cycle and restarting it resumes from the cursor with no lost or doubled events
- Queue compaction keeps the file under 1 MB in an 8-hour soak test
- Memory and CPU stay within NFR-03 during the soak

**Validation points**

- [ ] The agent starts on boot and login on all three OSes without manual steps
- [ ] `carryon status` shows the agent as running, with its version and uptime

---

### CO-108 · Project filter

**Size:** S · **Depends on:** CO-104, CO-107

**Description.** Before any upload, resolve `git rev-parse --show-toplevel` for the event's `cwd`, read `remote.origin.url` (falling back to the first remote), normalize it, and look it up with `match-project`, caching results for 1 hour. If there's no repo, an unregistered remote or no membership, mark the session as `skipped` in local state and never read its file. Log an unregistered repo once per remote, as "Repo github.com/org/x is not registered with Carryon".

**Testing points**

- Sessions in `~`, `/tmp`, an unregistered repo, a registered repo and a nested subfolder of a registered repo: only the last two upload
- A repo with no remotes is skipped
- A cache hit makes no network call
- A skipped session's transcript file is never opened (verified with a file-access spy in tests)

**Validation points**

- [ ] NFR-12: a day of mixed personal and project use uploads only project sessions
- [ ] Registering a repo starts capture for new events within 1 hour, or immediately after `carryon status --refresh`

---

### CO-109 · Incremental reader and pointer state

**Size:** M · **Depends on:** CO-107

**Description.** In `internal/reader`, `ReadNew(path, offset) (data []byte, newOffset int64, err)` opens the file read-only, seeks to the offset, and reads up to the last `\n`, so a partial trailing line is held back. State lives in `~/.carryon/state/<session_id>.json` as `{offset, next_seq, file_size, head_hash}`, written atomically (temp file plus rename). If the file is now smaller than the offset, or the hash of its first 4 KB changed, the file was rewritten: reset to offset 0 and upload a new version with a fresh `seq` range. Subagent transcripts in `<session>/subagents/*.jsonl` are tracked as child streams with their own pointers.

**Testing points**

- Appending half a line, then the rest: the first read returns nothing for that line and the second returns it whole
- A file truncated or rewritten while running triggers a full re-upload
- The writer and reader run concurrently for 10,000 lines with no corruption (race detector on)
- A crash between upload and state write leads to re-upload of the same `seq`, never a skipped range

**Validation points**

- [ ] Rebuilding the session from the chunks produces a file byte-identical to the local transcript (NFR-04)
- [ ] Subagent transcripts appear in the server copy

---

### CO-110 · Chunk upload pipeline

**Size:** L · **Depends on:** CO-102, CO-103, CO-109

**Description.**

1. Compress the slice with zstd and compute the SHA-256 of the *uncompressed* bytes.
2. Call the `register-chunk` Edge Function with `{session_id, seq, byte_start, byte_end, sha256, size}`. It checks membership, upserts the `sessions` row on the first chunk, and returns a signed upload URL for `<project>/<user>/<session>/chunk-<seq>.jsonl.zst`, valid 5 minutes.
3. Upload with PUT.
4. Call `confirm-chunk`, which verifies the object exists and inserts the `session_chunks` row with `ON CONFLICT (session_id, seq) DO UPDATE`.
5. Only then advance the local pointer.

Chunks above 8 MB uncompressed are split, so no single request is ever large.

**Testing points**

- Integration test against local Supabase: upload 3 chunks, rebuild, compare bytes
- An expired signed URL is re-requested automatically
- Registering the same `seq` twice replaces the row instead of duplicating it (NFR-05)
- A 40 MB slice is split into 8 MB parts with contiguous byte ranges
- `confirm-chunk` rejects a chunk whose object is missing

**Validation points**

- [ ] Cloud lag on a live session is under 2 minutes (NFR-02)
- [ ] Storage objects and `session_chunks` rows match one to one, with no orphans after a 1-day test

---

### CO-111 · Session record lifecycle

**Size:** S · **Depends on:** CO-110

**Description.** On the first chunk, `register-chunk` creates the `sessions` row with `project_id`, `user_id`, `machine` (hostname), `original_cwd`, `branch`, `head_sha` and `claude_code_version`. The last three come from the agent: `git` for branch and SHA, and `claude --version` cached per day. Every chunk updates `last_activity_at` and `total_bytes`. A `SessionEnd` event calls `end-session`, which sets `status = 'ended'`. A scheduled job marks sessions with no activity for 12 hours as `ended` (reason `idle`).

**Testing points**

- First chunk creates the row, later chunks only update it
- `SessionEnd` sets `ended` within 5 seconds of the event
- The idle job ends stale sessions and leaves active ones alone
- A resumed session (same ID, new events after `ended`) goes back to `live`

**Validation points**

- [ ] The sessions list in the database shows the correct developer, branch, Claude Code version and status for a day of real use

---

### CO-112 · Offline queue and retry

**Size:** M · **Depends on:** CO-110

**Description.** Classify every network failure as retryable (timeouts, 5xx, DNS, connection refused) or fatal (401 after refresh, 403, 404 on the project). Retry with exponential backoff from 5 seconds to a 10-minute cap, with jitter. Retries are per session, so one bad session doesn't block the others. Fatal errors mark the session `blocked`, with the reason shown in `carryon status`. All state is on disk, so a reboot resumes cleanly. Offline detection pauses the workers instead of spinning.

**Testing points**

- Chaos test: cut the network for 30 minutes during active writing, restore it, verify the byte-identical rebuild
- Reboot the machine mid-backlog: no loss, no duplicates
- A 7-day offline simulation with a time-warped clock keeps the queue intact (NFR-11)
- A 403 on one session doesn't delay uploads for another

**Validation points**

- [ ] NFR-04 holds across crash, offline and reboot scenarios
- [ ] A developer working on a plane sees everything uploaded within 5 minutes of landing

---

### CO-113 · `carryon status` and logs

**Size:** S · **Depends on:** CO-107, CO-112

**Description.** `carryon status` prints:

- login state and user
- agent state, version and uptime
- the backend URL
- per active session: project, last upload time, bytes behind and any blocked reason
- recently skipped repos

`--json` prints the same as JSON for scripts. Agent logs go to `~/.carryon/logs/agent.log` with daily rotation (7 kept), as structured JSON (`slog`). `carryon logs -f` tails them.

**Testing points**

- Snapshot tests of the status output for logged-out, agent-down, healthy, lagging and blocked states
- The `--json` schema is stable and documented
- Log rotation keeps 7 files

**Validation points**

- [ ] A developer can tell in 10 seconds whether their work is being captured
- [ ] Support can diagnose a stuck upload from the status output and logs alone

---

# Phase 2 — Code snapshots, pull and resume

**Goal:** anyone with access can run one command and continue a teammate's session, with that teammate's exact code including uncommitted work. **Duration:** weeks 3–4 (estimate). **Gate:** the CTO resumes another developer's *live* session from his own laptop. Claude answers correctly about files that were only changed locally and never committed.

---

### CO-201 · Code snapshot capture

**Size:** M · **Depends on:** CO-110

**Description.** In `internal/snapshot`, after each successful chunk upload, the agent checks whether the working tree changed since the last snapshot. It compares `git write-tree` output built with a temporary index file. If the tree changed:

1. `GIT_INDEX_FILE=$(mktemp)`, then `git read-tree HEAD` and `git add -A`, which respects `.gitignore`
2. `git write-tree`, then `git commit-tree <tree> -p HEAD -m "carryon snapshot <sid>"`
3. `git push origin <commit>:refs/carryon/<github-login>/<sid> --force`
4. Call `register-snapshot` with `{session_id, seq, head_sha, wip_ref, tree_sha}`

It never touches the developer's index, stash, branch or `index.lock`. Untracked files larger than 50 MB are excluded, with a warning in status. Snapshots run at most every 5 minutes per session, plus once on `SessionEnd`.

**Testing points**

- After a snapshot, `git status` output is identical to before (porcelain diff)
- The developer runs `git commit` while a snapshot is running: both succeed, with no lock errors
- New untracked files, deleted files and renamed files all appear correctly in the snapshot tree
- An unchanged tree makes no push and no new row
- `.env` listed in `.gitignore` is absent from the snapshot

**Validation points**

- [ ] Checking out the snapshot ref on another machine reproduces the developer's working tree exactly (`diff -r`, excluding ignored files)
- [ ] Developers report no interference with their git workflow during a 1-week trial

---

### CO-202 · Git bundle fallback

**Size:** S · **Depends on:** CO-201

**Description.** If the push to `refs/carryon/*` is rejected (a git host that blocks custom refs, protected namespaces, or no push permission), create `git bundle create <tmp> <commit> ^origin/<base>` containing only the objects missing from the remote. Upload it as `<project>/<user>/<sid>/code-<seq>.bundle` through a signed URL, and register it with `object_key` instead of `wip_ref`. After a rejection, remember the fallback per repo for 24 hours so the push isn't retried on every snapshot.

**Testing points**

- A local bare remote with a `pre-receive` hook rejecting `refs/carryon/*` triggers the bundle path
- The bundle applies with `git fetch <bundle>` on a fresh clone and gives the same tree SHA
- Bundle size stays proportional to the changed objects, not the whole repo

**Validation points**

- [ ] Pull works the same for a GitHub repo (ref) and a repo on a host that rejects custom refs (bundle)

---

### CO-203 · `get-manifest` Edge Function

**Size:** M · **Depends on:** CO-110, CO-201

**Description.** Input: `session_id`. It checks that the caller has the `resumer` role or higher on the session's project. CO-302 hardens this, and until then it checks membership. It returns:

- session metadata: original cwd, branch, head SHA, Claude Code version, owner and status
- the ordered chunk list, each with `seq`, byte range, SHA-256 and a signed download URL valid 5 minutes
- the latest code snapshot, as a ref or a bundle URL

It also writes an audit row `pull_manifest`.

**Testing points**

- A non-member gets 403, and a member without the resumer role gets 403
- Chunks come back in `seq` order, with no gaps in byte ranges
- Signed URLs expire after 5 minutes (time-travel test)
- A session with 500 chunks returns in under 1 second

**Validation points**

- [ ] Every manifest request appears in `audit_log` with actor, session and time

---

### CO-204 · `carryon ls`

**Size:** S · **Depends on:** CO-203

**Description.** Lists sessions the user can see, newest activity first, with flags `--project`, `--dev`, `--status live|ended`, `--since 7d`, `--limit` and `--json`. Columns: short ID, project, developer, branch, title (or the first prompt until Phase 4), last activity (relative time) and status. Short IDs (the first 8 characters) are accepted anywhere a session ID is.

**Testing points**

- Each filter alone and in combination returns the correct rows from seed data
- An ambiguous short ID gives a clear error listing the matches
- Output fits 120 columns, and long titles are cut off with …

**Validation points**

- [ ] The CTO can find yesterday's session from a named developer in under 15 seconds

---

### CO-205 · Session rebuild

**Size:** M · **Depends on:** CO-203

**Description.** In `internal/pull`, download all chunks in parallel (4 at a time) to a temp folder. Decompress each, verify its SHA-256 against the manifest, and concatenate in `seq` order into `session.jsonl`. Verify that byte ranges are contiguous. If any chunk fails, retry it twice, then abort with the failing `seq`. Subagent transcripts are rebuilt the same way into the matching `subagents/` folder. Downloads resume after a network drop.

**Testing points**

- Rebuilt file is byte-identical to the source for fixtures of 1 KB, 5 MB and 200 MB
- A corrupted chunk (flipped byte) is detected and named
- A missing chunk in the middle is reported as a gap, never silently skipped
- Pull time is within NFR-06 for a 50 MB session

**Validation points**

- [ ] NFR-04: across 20 real sessions, every rebuild matches the laptop copy

---

### CO-206 · Worktree restore

**Size:** M · **Depends on:** CO-201, CO-202

**Description.** Find a local clone of the project. Check the current folder, then `~/.carryon/config.toml` `repos` mappings. If none exists, offer to clone into `~/carryon/repos/<name>`. Then:

- **Ref:** `git fetch origin refs/carryon/<dev>/<sid>`
- **Bundle:** download it and run `git fetch <bundle>`
- Run `git worktree add --detach ~/carryon/work/<project>-<sid8> <commit>`

If no snapshot exists, check out `head_sha` and print a warning that uncommitted work was not captured. Reuse the worktree if it already exists at the same commit. `carryon pull --clean` removes old Carryon worktrees.

**Testing points**

- The puller's own checkout is untouched: same branch, same `git status`
- A puller with uncommitted changes in their own checkout is unaffected
- Pulling the same session twice reuses the worktree
- With no local clone, the clone prompt works, and `--yes` clones without asking

**Validation points**

- [ ] The worktree content equals the developer's working tree at snapshot time

---

### CO-207 · Path rewrite

**Size:** M · **Depends on:** CO-205

**Description.** Replace every occurrence of `original_cwd` with the local worktree path inside JSON string values, line by line. Parse each line as JSON, walk the string values, and replace the path prefix. Never use raw text substitution, which can break JSON escaping. Handle Windows paths: backslashes escaped as `\\` in JSON, drive letters and case-insensitive matching. Also rewrite paths under the developer's home folder that point into the repo through symlinks, when resolvable. Lines that fail to parse are kept unchanged and counted in a warning.

**Testing points**

- Fixtures: macOS to Linux, Linux to Windows, Windows to macOS. Every rewritten line is valid JSON.
- A path that only shares a prefix (`/Users/ali/work/acme-api-old`) is not rewritten
- Tool results that contain paths inside code blocks are rewritten
- Line count and line order are unchanged

**Validation points**

- [ ] After resume, Claude opens files at the new location without "file not found" errors in a 10-question test

---

### CO-208 · Session ID rewrite and fork registration

**Size:** S · **Depends on:** CO-207

**Description.** Generate a new UUIDv4. Replace the `sessionId` field on every line, and name the file `<new-id>.jsonl`. Call `register-fork` with `{parent_session_id, new_session_id, puller}`, which creates a `sessions` row with `parent_session_id` set and `status = 'live'`. That way, if the puller keeps working, their agent uploads into the fork's record.

**Testing points**

- No line in the output still contains the old session ID in `sessionId`
- The fork row has the correct parent and puller as owner
- Pulling the same session twice creates two separate forks

**Validation points**

- [ ] The original developer's session record and file are never modified by a pull

---

### CO-209 · Claude path encoder and file placement

**Size:** S · **Depends on:** CO-208

**Description.** `claudepath.Encode(absPath)` replaces every character that isn't a letter or digit with `-`, matching Claude Code's project folder naming on each OS. Place the file at `~/.claude/projects/<encoded>/<new-id>.jsonl`, and subagent transcripts in its `subagents/` folder. Refuse to overwrite an existing file.

**Testing points**

- Encoder tests against folder names produced by the pinned Claude Code version on all three OSes. Fixtures are captured by running Claude Code in CI.
- Paths with spaces, dots, underscores and non-ASCII characters
- An existing file with the same ID aborts with a clear message

**Validation points**

- [ ] `claude --resume` finds the placed session on macOS, Linux and Windows

---

### CO-210 · Launch, header and version check

**Size:** S · **Depends on:** CO-209

**Description.** Print a header:

```text
Ali · feature/payments · last activity 18:42 · 3 uncommitted files · Claude Code 2.x.y
```

Compare the session's Claude Code version with the local one. If the local version is older by a major or minor release, warn and ask. Then `chdir` into the worktree and run `claude --resume <new-id> --fork-session`, passing through stdin, stdout and the TTY. `--no-launch` stops after placement and prints the command instead.

**Testing points**

- The version comparison handles equal, newer, older and unparseable versions
- `--no-launch` prints a command that works when pasted
- With no `claude` on PATH, the error says how to install it

**Validation points**

- [ ] Claude opens with the full history visible and answers "what were we doing?" correctly

---

### CO-211 · `carryon pull` end to end, plus `carryon export`

**Size:** M · **Depends on:** CO-203 to CO-210

**Description.** Wire CO-203 to CO-210 into `carryon pull <id>` with a progress display (manifest → code → chunks → rewrite → place → launch) and a summary at the end. Add `carryon export <id> -o session.jsonl`, which rebuilds the file without rewriting or placing it, for manual use and debugging. Every failure prints which step failed and how to retry. The full flow is covered by the CI end-to-end test.

**Testing points**

- E2E: user A writes a fixture session with uncommitted changes, user B pulls on a different OS runner and Claude Code (pinned version) resumes headless with `-p "list the files you edited"`. The output names the uncommitted file.
- Each step is interrupted in turn (Ctrl-C) and the next run recovers
- `export` output matches the server copy byte for byte

**Validation points**

- [ ] **Phase gate:** the CTO resumes a live session of another developer and Claude correctly describes uncommitted changes
- [ ] Pull completes within NFR-06
- [ ] The PRD runbook section matches the real command output

---

# Phase 3 — Security, access control and rollout

**Goal:** make Carryon safe to switch on for the whole company: no secrets leave laptops, only the right people see each project, every access is logged, and every laptop is set up the same way. **Duration:** weeks 5–6 (estimate). **Gate:** one week of use by the full team, the server-side rescan finds no secrets, and every access-control test passes.

---

### CO-301 · Redaction engine

**Size:** L · **Depends on:** CO-110

**Description.** In `internal/redact`, run the gitleaks v8 default rules plus Carryon custom rules over every slice *before* compression. Custom rules cover Supabase service keys, Anthropic keys, `postgres://user:pass@` URLs and private-key blocks. Replace each match with `[REDACTED:<rule-id>]`.

Redaction runs per JSON line on string values, so the output stays valid JSON. Add a path denylist (`.env*`, `*.pem`, `*.key`, `id_rsa*`, `*.p12`, `credentials*`, `secrets.*`). When a tool call reads a file on the denylist, its *whole* tool result is replaced with `[REDACTED:denylisted-file <name>]`. Configure the rules from the server (`get-config`) so they can be updated without a new binary.

Note that redaction changes byte lengths. Offsets keep referring to the *original* file, and chunks hold the redacted bytes. Record a per-chunk count of redactions in `session_chunks.redactions`.

**Testing points**

- A corpus of 200 known secrets across 40 types, each embedded in realistic transcript lines: 100% caught
- A corpus of 500 non-secret lines (UUIDs, hashes, base64 images, test fixtures): false-positive rate under 1%
- Every redacted line still parses as JSON
- A read of a `.env` tool call is fully replaced
- Throughput stays above 20 MB/s on a 2020 laptop, so it adds no visible lag

**Validation points**

- [ ] NFR-07: a red-team engineer pastes 20 kinds of secrets into real sessions and none reach Storage
- [ ] Resumed sessions still work with placeholders in place, and Claude asks for the value instead of failing

---

### CO-302 · Roles and row-level security

**Size:** L · **Depends on:** CO-102

**Description.** Write RLS policies for every table and Storage object, with three roles per project:

| Role | Can do |
| --- | --- |
| `viewer` | List sessions and read transcripts |
| `resumer` | Viewer rights, plus get manifests and pull |
| `admin` | Resumer rights, plus delete sessions and manage members |

Each developer can always read their own sessions. Use a `security definer` function `has_role(project_id, min_role)` to keep policies short. Edge Functions switch from the service role to the caller's JWT wherever possible. Storage policies tie object paths (`<project>/...`) to project membership.

**Testing points**

- A pgTAP test for every policy, run as every role and as a non-member: allowed operations succeed, the rest return 0 rows or are refused (NFR-08)
- Path-traversal attempts on Storage keys are refused
- A user removed from a project loses access on their next request
- Tests run in CI on every migration

**Validation points**

- [ ] A matrix of roles against actions is reviewed and signed off by the CTO
- [ ] An external reviewer cannot read another project's session with a valid token for a different project

---

### CO-303 · GitHub team sync

**Size:** M · **Depends on:** CO-302

**Description.** Each project maps to GitHub teams: `team_viewer`, `team_resumer` and `team_admin`, stored on `projects`. A scheduled Edge Function, `sync-github-teams`, runs every 15 minutes. It reads team members through the GitHub API with a GitHub App installation token, then upserts and removes `project_members` rows to match. Manual overrides set in the web UI are flagged as `source = 'manual'` and survive syncs. Changes are written to `audit_log`.

**Testing points**

- Mocked GitHub API: add, remove and role changes are applied correctly
- Manual overrides are kept
- GitHub API rate limits and outages leave memberships unchanged, never empty
- A team rename is detected through its stable team ID

**Validation points**

- [ ] Removing someone from the GitHub team removes their Carryon access within 15 minutes

---

### CO-304 · Audit log

**Size:** S · **Depends on:** CO-302

**Description.** Write an `audit_log` row for every `upload` (once per session per day, to limit volume), `view` (transcript opened in the web app), `pull_manifest`, `pull_complete`, `delete`, `role_change` and `config_change`. Columns: actor, action, session, project, IP and user agent. Only admins can read the log. A developer can read the rows that concern *their own* sessions, to see who pulled them. Rows are append-only: no update or delete grants for anyone, including admins. Only the retention job (CO-307) removes them.

**Testing points**

- Each action type writes exactly one row with the correct actor
- Update and delete on `audit_log` are refused for every role
- A developer sees pulls of their own sessions and nothing else

**Validation points**

- [ ] The CTO can answer "who pulled Ali's sessions last week?" from the log in under a minute

---

### CO-305 · Storage hardening

**Size:** S · **Depends on:** CO-302

**Description.** All signed URLs are valid for at most 300 seconds. The bucket is private, with no public URLs. Add support for a separate bucket per client (`projects.bucket`) for clients whose contracts require it. Encryption at rest is Supabase's default. Document it, and confirm the region matches client data-residency requirements. Set object size limits per upload to match the chunk size cap.

**Testing points**

- A signed URL used after 301 seconds fails
- An object in a client bucket is unreachable through a project in another bucket
- An upload over the size limit is refused

**Validation points**

- [ ] Security checklist signed: private buckets, TTL, region and encryption confirmed

---

### CO-306 · Server-side secret rescan

**Size:** M · **Depends on:** CO-301

**Description.** A nightly Edge Function job, `rescan`, scans chunks uploaded in the last 24 hours with the *latest* rule set, which may be newer than what the laptop ran. On a hit:

1. Rewrite the chunk with the secret redacted, keeping the same `seq` and a new SHA-256
2. Store `rescan_hit` in `audit_log`
3. Notify the project admin and the developer, by email or Slack once CO-409 lands, telling them to rotate the key

A full rescan of every chunk can be triggered manually after a rule update.

**Testing points**

- A secret planted directly in Storage (bypassing the laptop) is found and redacted overnight
- The rewritten chunk still rebuilds into valid JSON
- The job is resumable if it times out, and processes in batches

**Validation points**

- [ ] **Phase gate:** a full rescan after one week of team use reports zero unredacted secrets, or every hit was rotated and recorded

---

### CO-307 · Retention and deletion

**Size:** M · **Depends on:** CO-302, CO-304

**Description.** Each project has `retention_days_after_close` (default 90) and `closed_at`. A daily job deletes the sessions, chunks, snapshots (Storage objects and `refs/carryon/*` through the GitHub API) and audit rows of projects past retention. Admins can delete a single session with a mandatory reason, which is kept in the audit log as the only remaining record. `carryon` exposes `admin delete <sid> --reason`.

**Testing points**

- Expired projects are fully purged: no rows, no objects, no refs
- Active projects are untouched
- Deleting a parent session keeps its forks, with `parent_session_id` set to null
- A deletion without a reason is refused

**Validation points**

- [ ] A dry-run report lists what would be deleted before the first real run, and the CTO approves it

---

### CO-308 · Fleet rollout and auto-update

**Size:** L · **Depends on:** CO-106, CO-113

**Description.** Package the binary for:

- Homebrew tap (`devminified/tap/carryon`)
- winget and a signed MSI
- a `.deb`, plus a `curl | sh` installer script that verifies the checksum

Each installer runs `carryon install` after install. Add Claude Code managed settings (the enterprise-managed `settings.json`) containing the Carryon hooks, so they can't be removed, for teams using MDM. The agent checks a release manifest daily, downloads a newer signed binary, verifies the signature, swaps it atomically and restarts itself. Admins can pin a version in `get-config`.

**Testing points**

- A fresh install on each OS reaches a working `carryon status` without manual steps
- Auto-update from vN to vN+1 succeeds. A tampered binary is refused. A failed update rolls back.
- A pinned version stops updates past it
- Managed settings take precedence over user edits

**Validation points**

- [ ] Every developer laptop reports the same Carryon version within 48 hours of a release

---

### CO-309 · Consent and policy acknowledgment

**Size:** S · **Depends on:** CO-103

**Description.** On first `carryon login`, show a short notice covering:

- what is captured: Claude Code sessions in registered company repos
- what isn't: anything outside them
- who can see it: the project team
- retention, and where to read the full policy

The developer must type `yes`. Store the acknowledgment in a `consents` table with the policy version. When the policy version changes, the next login asks again. The agent uploads nothing for a user with no current consent.

**Testing points**

- No consent means no uploads (verified end to end)
- A policy version bump triggers a new prompt
- The consent record holds the user, version and timestamp

**Validation points**

- [ ] HR and legal approve the notice text
- [ ] 100% of active developers have a current consent before the phase gate

---

# Phase 4 — Web app and AI summaries

**Goal:** anyone with access can browse, read and understand sessions in the browser, each with an AI-written title and summary, and pull only when they want to continue. **Duration:** weeks 7–9 (estimate). **Gate:** the CTO finds a specific piece of work by its summary and reads the whole session without pulling it.

---

### CO-401 · Web app scaffold and sign-in

**Size:** M · **Depends on:** CO-302

**Description.** Create a Next.js App Router app in `web/` using `@supabase/ssr` for server-side auth with GitHub sign-in. It has a base layout (top bar, project switcher, user menu), route protection, and loading and error states. All data access goes through the user's JWT, so RLS from CO-302 applies everywhere, and the browser never sees a service key. Deploy previews per pull request. Production lives at `carryon.devminified.com`.

**Testing points**

- Playwright: sign in, sign out, deep link to a protected page redirects to sign-in and back
- A grep of the bundle shows no service-role key
- Lighthouse accessibility score of at least 90 on the main pages

**Validation points**

- [ ] A developer signs in with GitHub and sees only their projects

---

### CO-402 · Project and session list

**Size:** M · **Depends on:** CO-401

**Description.** `/p/<project>` lists sessions, newest activity first. Each row shows title, two-line summary, developer, branch, status (a live dot when activity is under 5 minutes old), outcome badge, last activity and size. Filters: developer, status, outcome, date range and branch. Free-text search covers title and summary using Postgres full-text search until CO-501 adds semantic search. Pagination is cursor-based, 50 per page. Live sessions update through Supabase Realtime.

**Testing points**

- Filters combine correctly against seed data
- A new chunk upload updates "last activity" within 5 seconds, without a reload
- 10,000 sessions in a project: the first page loads in under 1 second
- A non-member gets a 404, not a 403, so project names don't leak

**Validation points**

- [ ] The CTO can see what each developer worked on today in one screen

---

### CO-403 · Session viewer

**Size:** L · **Depends on:** CO-402

**Description.** `/s/<session>` renders the transcript:

- developer messages and Claude replies as a chat, with Markdown rendered
- tool calls collapsed into one-line summaries ("Edited `webhook.ts` (+12 −03)", "Ran `npm test`, exit 1"), expandable to full input and output
- diffs shown as diffs
- redacted values highlighted

The page loads chunks progressively from signed URLs, decompressing zstd in the browser with a WASM decoder, and virtualizes long transcripts. A side panel shows metadata, summary, files touched, forks and a copy button for `carryon pull <id>`. Opening the page writes a `view` audit row.

**Testing points**

- Renders the fixtures correctly: plain chat, many tool calls, subagents, a 200 MB session
- A 200 MB session stays responsive: first content in under 2 seconds, smooth scrolling
- Unknown line types display raw JSON instead of crashing, so future Claude Code formats degrade gracefully
- A `view` audit row is written once per open

**Validation points**

- [ ] A non-coder (the CTO) can follow what happened in a session without pulling it

---

### CO-404 · Fork family tree

**Size:** S · **Depends on:** CO-403

**Description.** On the session page, show the session's lineage: its parent (if it is a fork), siblings and children, as a compact tree. Each node shows the developer, title and date, and links to that session. Data comes from a recursive query on `parent_session_id`.

**Testing points**

- Trees of depth 1, 3 and 10 render correctly
- Forks the viewer can't access appear as "1 fork in a project you can't see", without details
- A deleted parent shows as "deleted session"

**Validation points**

- [ ] Anyone can see who picked up a session and where the work continued

---

### CO-405 · Rename and pin

**Size:** S · **Depends on:** CO-403

**Description.** The session owner and project admins can rename a session in the web app or with `carryon rename <id> "<title>"`. A manual title sets `title_source = 'manual'`, and AI summaries never overwrite it. Admins can pin important sessions to the top of the project list, for example "Payment architecture decision".

**Testing points**

- A manual title survives a re-summary
- Only the owner and admins can rename, which RLS enforces
- Pinned sessions sort first and show a pin icon

**Validation points**

- [ ] Renaming in the CLI shows up in the web app within 5 seconds

---

### CO-406 · Condensed transcript builder

**Size:** M · **Depends on:** CO-205

**Description.** A shared TypeScript module used by the summary job. It reads a session's chunks and produces a condensed text:

- every developer prompt, in full
- Claude's final text reply for each turn
- one line per tool call (tool, target, result status)
- the deduplicated list of files touched and commands run

It drops bulky tool output such as file contents, long logs and images. The output is capped at about 60,000 tokens: the oldest middle turns are dropped first, and the first and last turns are always kept. It supports incremental mode, condensing only chunks after a given `seq`.

**Testing points**

- A 200 MB session condenses to under the cap
- No redacted placeholder is expanded and no secret is reintroduced
- Incremental output plus the previous output equals the full output on fixtures
- `files_touched` is exact against a hand-labelled fixture

**Validation points**

- [ ] Reading only the condensed text, a reviewer can tell what the session was about

---

### CO-407 · AI title and summary job

**Size:** L · **Depends on:** CO-406

**Description.** Edge Function `summarize-session`. It is triggered on `end-session`, and every 30 minutes for live sessions with new chunks. It calls the Anthropic API (Claude Haiku) with the condensed transcript and the previous summary (incremental mode). It asks for structured JSON:

- `title`, at most 8 words
- `summary`, 3–5 sentences: what was asked, what was done, where it stopped
- `outcome`: `done`, `in_progress` or `blocked`, plus `blocked_reason`
- `files_touched`

It validates the JSON and retries once if it's invalid. It writes the results to `sessions`, without overwriting a manual title. Each project has a token budget per day, and the job logs cost.

**Testing points**

- Golden tests on 20 fixture sessions: valid JSON, the title is at most 8 words, the outcome matches a human label in at least 85% of cases
- An API failure retries with backoff and leaves the old summary in place
- A manual title is never overwritten
- The budget cap stops calls and logs a warning

**Validation points**

- [ ] In a blind review of 30 real sessions, developers rate at least 80% of summaries "accurate and useful"
- [ ] Average cost per summarized session is recorded and approved

---

### CO-408 · Summary display and re-run

**Size:** S · **Depends on:** CO-402, CO-407

**Description.** Show title, summary, outcome badge and files touched on the list and the session page. Mark summaries as "AI-generated" with their time. Admins can click "Re-summarize". The list shows "Summarizing…" while a job runs.

**Testing points**

- The UI updates live when the job writes a new summary
- Re-summarize is rate-limited to once per minute per session

**Validation points**

- [ ] **Phase gate:** the CTO searches for a past piece of work by a phrase from its summary, finds it, and reads the session without pulling

---

### CO-409 · Slack notifications

**Size:** M · **Depends on:** CO-407

**Description.** Optional per project: a Slack channel (incoming webhook or Slack app) receives a message when a session ends with outcome `blocked` ("Ali's session *Payment webhook retry* is blocked: failing integration test"), with a link to the session. Other messages: a daily digest of sessions per developer (opt-in), and secret-rescan hits (CO-306) sent to admins. Messages never include transcript content beyond the title and summary.

**Testing points**

- Each message type renders correctly in the Slack Block Kit preview
- A failed webhook is retried and logged without breaking the summary job
- Projects with notifications off send nothing

**Validation points**

- [ ] Blocked sessions reach the project channel within 10 minutes of ending

---

### CO-410 · Admin pages

**Size:** M · **Depends on:** CO-302, CO-303, CO-304

**Description.** `/admin`, for admins only, has these pages:

- **Projects:** register a repo (replacing the CLI from CO-104), map GitHub teams, set retention, bucket and Slack
- **Members:** view synced members and add manual overrides
- **Audit:** filter by actor, action, project and date, and export CSV
- **Agents:** last check-in, version and lag per developer machine, from agent heartbeats added here
- **Config:** redaction rules version, pinned agent version and summary budget

**Testing points**

- Non-admins get a 404 on every admin route
- Every config change writes an audit row
- CSV export matches the filtered view

**Validation points**

- [ ] An admin onboards a new client project end to end in the web app in under 5 minutes

---

# Phase 5 — Search, hardening and release

**Goal:** make past work findable by meaning, prove Carryon survives Claude Code upgrades and real load, make it installable on a client's own servers, and release it as open source. **Duration:** weeks 10–12 (estimate). **Gate:** the nightly compatibility test has been green for 2 weeks, a self-hosted install works from the docs alone, and the security review has no open high-severity findings.

---

### CO-501 · Semantic search

**Size:** L · **Depends on:** CO-407

**Description.**

1. Choose an embedding provider and model. Record the decision in an ADR, comparing quality on 50 labelled queries, cost and self-host options.
2. Add `sessions.embedding vector(<dim>)` with an HNSW index.
3. Embed `title + summary + files_touched` after each summary update. Also embed per-turn chunks of the condensed transcript into a `session_passages` table for finer matches.
4. Add an Edge Function `search` that takes a query, embeds it and returns sessions ranked by similarity. It is filtered by RLS (only projects the caller can see) and combined with full-text rank (hybrid).
5. In the web app, the search box switches to semantic results. Results highlight the matching passage.

**Testing points**

- On 50 labelled queries ("where did we fix duplicate webhooks"), the right session is in the top 3 at least 80% of the time
- Search never returns sessions from projects the caller can't access (RLS test with embeddings)
- p95 search latency under 500 ms with 100,000 passages
- Re-embedding after a summary change replaces old vectors

**Validation points**

- [ ] The CTO finds three real past pieces of work by describing them in plain words

---

### CO-502 · "Who touched this file" lookup

**Size:** S · **Depends on:** CO-407

**Description.** Index `files_touched` with a GIN index. In the web app, a file search box (`src/payments/webhook.ts`) lists sessions that touched that file, newest first, with developer and outcome. The CLI gets `carryon ls --file <path>`.

**Testing points**

- Exact and prefix matches (`src/payments/`) work
- Paths are normalized relative to the repo root, so different machines match
- Results respect RLS

**Validation points**

- [ ] Before editing a file, a developer can see who last worked on it with Claude and read that session

---

### CO-503 · Claude Code compatibility CI

**Size:** M · **Depends on:** CO-211

**Description.** A nightly GitHub Actions workflow runs on all three OSes. It installs (a) the team's pinned Claude Code version and (b) the latest release. For each:

1. Run a scripted headless Claude Code session in a fixture repo, using `claude -p` with a test API key and a low-cost model
2. Capture it with the agent and upload it to a test backend
3. Pull it as another user
4. Resume it headless and ask a question only answerable from the history

The test checks the answer, the encoder folder names (CO-209) and the session-file line types it knows about. Unknown new line types are reported, not failed. On failure it posts to Slack, and admins can't move the pinned version (CO-308) to a version without a green run.

**Testing points**

- The workflow fails when a fixture introduces a breaking format change (simulated)
- Run cost per night is logged and stays within an agreed budget

**Validation points**

- [ ] **Phase gate:** 14 consecutive green nights on the pinned version
- [ ] A new Claude Code release is either green within 24 hours or flagged with a specific breaking change

---

### CO-504 · Observability

**Size:** M · **Depends on:** CO-113, CO-410

**Description.** The agent sends a heartbeat every 15 minutes with version, OS, queue depth, bytes behind, last upload time, error counts and redaction counts (no content). The backend stores it in `agent_heartbeats` with 30-day retention. The admin Agents page (CO-410) and a dashboard show cloud lag p50 and p95, upload error rate, summary job latency and failures, storage growth per project, and AI cost per day. Alerts go to Slack when an agent hasn't checked in for 24 hours on a workday, lag p95 exceeds 10 minutes, the error rate exceeds 5% or storage grows abnormally.

**Testing points**

- Heartbeats contain no transcript data (schema check)
- Each alert fires once in a simulated condition and clears when resolved
- The dashboard numbers match database queries

**Validation points**

- [ ] NFR-02 lag is measured continuously and visible to admins

---

### CO-505 · Load, soak and cost test

**Size:** M · **Depends on:** CO-211, CO-407

**Description.** Simulate 50 developers for 8 hours each on staging. Each runs the fixture writer at a realistic rate (one reply every 30–120 seconds, occasional 10 MB tool outputs), with 5 pulls per hour across the team and random network drops. Measure Edge Function latency, database CPU, Storage requests, summary job throughput and total cost. Then extrapolate the monthly Supabase and Anthropic cost for 50 developers.

**Testing points**

- No 5xx errors from Edge Functions under load, or only retried ones with zero data loss
- Every simulated session rebuilds byte-identically at the end (NFR-04)
- The test is scripted and repeatable (`make loadtest`)

**Validation points**

- [ ] A cost report with a monthly estimate is reviewed and approved
- [ ] The required Supabase plan tier is confirmed

---

### CO-506 · Self-hosted deployment

**Size:** L · **Depends on:** CO-410

**Description.** Package Carryon for a client's own servers:

- a `deploy/selfhost/` folder with a Docker Compose file for self-hosted Supabase (Postgres, Auth, Storage, Edge Runtime, Realtime) and the web app
- migrations and functions applied by an init container
- S3-compatible storage support, such as MinIO or the client's S3
- GitHub Enterprise Server support for OAuth and team sync
- an optional AI provider, with summaries turned off when none is configured

The binary points at it with `carryon config set api_url ...`, or through a config file pushed by MDM. Add a short install guide and a health-check page.

**Testing points**

- CI brings up the self-host stack and runs the full e2e test against it
- An install with summaries disabled works, and the UI hides AI fields
- Upgrading from version N to N+1 of the stack keeps all data

**Validation points**

- [ ] **Phase gate:** an engineer who didn't build Carryon deploys it on a fresh VM from the guide alone in under 2 hours

---

### CO-507 · Security review and threat model

**Size:** M · **Depends on:** CO-301 to CO-308

**Description.** Write a threat model in STRIDE format covering:

- a stolen laptop token
- a malicious insider pulling sessions
- a compromised Edge Function
- secret leakage through redaction gaps
- a supply-chain attack on the binary or update channel
- prompt injection through a pulled transcript

Run an internal review and, if budget allows, an external pen test of the API, RLS and update channel. Fix every high and critical finding. Document how the rest are mitigated.

**Testing points**

- Each threat maps to at least one mitigation and one test
- `gosec`, `govulncheck`, `npm audit` and container scans are clean or documented

**Validation points**

- [ ] **Phase gate:** no open high or critical findings
- [ ] The threat model is signed off by the CTO

---

### CO-508 · Open-source release

**Size:** M · **Depends on:** CO-506, CO-507

**Description.** Prepare the public repo `devminified/carryon`:

- remove internal URLs, client names and credentials, and check the git history with gitleaks
- choose a license (MIT recommended)
- add README, CONTRIBUTING, CODE_OF_CONDUCT and SECURITY (vulnerability reporting)
- add a docs site: quick start, architecture with the PRD diagrams, self-hosting and FAQ
- set up issue templates and public GitHub Actions

The Devminified production deployment keeps a private config repo. Publish a launch post about the urgent-handoff problem.

**Testing points**

- A fresh clone of the public repo builds, tests and runs the e2e test with no private access
- A gitleaks scan of the full history is clean

**Validation points**

- [ ] Legal approves the license and the trademark use of "Devminified Carryon"
- [ ] An outside developer gets from the README to a working local install in under 30 minutes

---

### CO-509 · Research: other coding agents

**Size:** M · **Depends on:** CO-211

**Description.** A time-boxed research ticket of 3 days. Assess capturing and resuming sessions from OpenAI Codex CLI and other terminal agents your clients use: where they store sessions, whether the format is append-only, whether they offer a resume command and a hook or notify mechanism. Write an ADR recommending whether to support them, and how the agent's capture layer would become pluggable (`internal/agents/<name>`). Don't build it in this phase.

**Testing points**

- A prototype captures one Codex CLI session into a local folder, if feasible within the time box

**Validation points**

- [ ] The ADR is reviewed and a go or no-go decision is recorded

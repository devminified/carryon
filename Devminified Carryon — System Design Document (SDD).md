# Devminified Carryon — System Design Document (SDD)

Nov 4, 2026 · @Devminified

How Carryon is built underneath: the invariants it never breaks, the algorithms and data structures behind each component and why they were chosen, and how the same design grows from one team to millions of developers. It sits below Doc (what and why) and Doc (tickets).

## Design principles and system invariants

Carryon is robust because a small set of invariants holds everywhere, not because any single component is clever. Every design choice below exists to keep one or more of these true. Code review rejects any change that would break one.

| ID | Invariant | Enforced by |
| --- | --- | --- |
| **I1** | **The developer is never blocked.** Nothing Carryon does can slow down or fail a Claude Code session. | The hook only appends to a local log, and all real work happens in a separate process (§ Laptop agent) |
| **I2** | **Append-only everywhere.** Session files, the local queue, chunks and the audit log are only ever appended to, never edited in place. | The data model. Corrections are new versions, never mutations. |
| **I3** | **Effectively-once delivery.** Every byte is delivered at least once, and duplicates are harmless. | At-least-once retries plus idempotent writes keyed by `(session_id, seq)` (§ Upload protocol) |
| **I4** | **Integrity is verifiable end to end.** A rebuilt session is provably identical to what was uploaded, in order, with nothing missing. | Per-chunk SHA-256, a hash chain linking chunks, and a contiguous watermark |
| **I5** | **No secret leaves the laptop unredacted** (for every pattern in the current rule set). | Laptop-side redaction before compression, plus a server-side rescan as a second line of defense |
| **I6** | **Tenant isolation.** A user can never read data from a project or organization they don't belong to. | Row-level security on every row, tenant-scoped storage paths, short-lived signed URLs |
| **I7** | **One writer per session.** At any moment, only one machine may append to a given session. | Leases with fencing tokens |
| **I8** | **Crash-safe state.** A crash, power loss or reboot at any instruction leaves recoverable state. | Atomic rename for state files, write-ahead queue, commit-after-confirm |
| **I9** | **The server is never in the data path for bytes.** Session bytes flow laptop ↔ object storage directly. | Pre-signed URLs, so API servers only handle small metadata calls and scale independently |
| **I10** | **Every component can be scaled horizontally.** No step requires a single global lock or a single machine. | Stateless APIs, partitioned data and cell-based deployment (§ Scaling) |

Three principles guide choices the invariants don't settle:

1. **Prefer boring, proven mechanisms.** Write-ahead logs, content hashes and leases have decades of production history. Novelty is a risk, not a feature.
2. **Pick each data structure for the problem in front of it.** A structure earns its place by the access pattern it serves. § Algorithms and data structures states the access pattern, the complexity and the rejected alternative for every choice.
3. **Degrade, don't fail.** When something is unavailable (network, Supabase, the AI model), the affected feature pauses and catches up later. Capture never stops because summaries are down.

## Architecture overview

&#91;embedded content: architecture overview · 3 trust zones\]

Three trust zones, each with a different job. The laptop does the expensive and sensitive work: redaction, compression and hashing. The cell stores metadata and bytes, and runs background jobs. Third parties get only what they strictly need: GitHub handles identity and code, the Anthropic API sees redacted, condensed text, and Slack sees titles and links. The agent also pushes code snapshots to GitHub as hidden git refs (not drawn, to keep the picture readable).

## Laptop agent internals

&#91;embedded content: laptop agent pipeline · 10 stages\]

Read it as a snake: the top row runs left to right, and the bottom row right to left. Only the hook runs while Claude Code waits. Every other stage runs in the background agent, and each is backed by the data structure named in its box. The next section explains each choice. Two properties hold across the whole pipeline: memory is bounded (by the channel size and the chunk cap), and every stage after the reader works on one session's slice at a time, so a slow or failing session can't stall the others.

## Algorithms and data structures

Every structure here was chosen for a specific access pattern. Where a familiar structure was *not* used (for example, a binary search tree for indexes), the reason is stated, because rejecting the wrong structure matters as much as picking the right one. *n* is the number of items, *k* the bytes processed.

| # | Where | Problem | Choice | Cost | Rejected alternative |
| --- | --- | --- | --- | --- | --- |
| 1 | Hook → agent | Hand off events without ever blocking | **Append-only log** (write-ahead log) with a persisted read cursor | O(1) append | A socket to the agent: fails when the agent is down, breaking I1. SQLite: lock contention between concurrent hook processes. |
| 2 | Agent scheduler | Upload each dirty session at most once per debounce window | **Hash map** (session → state) + **min-heap** priority queue of due times | Mark dirty O(1), schedule O(log n), next due O(1) | Polling every session, O(n) per tick. One timer per session: nondeterministic order and hard to test. |
| 3 | Agent workers | Bound concurrency and memory | **Bounded FIFO channel** with backpressure + an in-flight **set** for per-session exclusion | O(1) | Unbounded goroutines: memory spikes after a long offline period |
| 4 | Reader | Never upload half a JSON line | **Reverse scan** for the last `\n` in the read window | O(k) | Parsing every line to find the end: same result, 5–10× slower |
| 5 | Redaction | Find secrets for \~200 rules in one pass | **Aho–Corasick** automaton over all rule keywords, then **RE2** regex only on candidate windows, then a **Shannon-entropy** check | O(k + matches), linear-time regex | Every regex on every line, O(k·r). Backtracking regex (PCRE): catastrophic backtracking (ReDoS) on hostile input. |
| 6 | Redaction and path rewrite | Walk arbitrary JSON safely | **Iterative depth-first traversal with an explicit stack**, depth limit 512 | O(nodes), memory bounded by depth | Recursion: a crafted, deeply nested line could overflow the call stack |
| 7 | Compression | Small chunks compress poorly | **zstd with a trained dictionary** built from sample transcripts, dictionary ID stored per chunk | Linear | gzip: slower and worse ratio. zstd without a dictionary: weak on 10–100 KB chunks. |
| 8 | Integrity | Detect missing, reordered or altered chunks | **SHA-256 per chunk + hash chain** hₙ = SHA-256(hₙ₋₁ ‖ shaₙ) | O(1) per append, O(n) full verify | Full Merkle tree: only needed for random partial verification (kept as a later option for the viewer) |
| 9 | Retries | Recover without stampedes | **Exponential backoff with full jitter**, a **circuit breaker** per endpoint, a client **token bucket** | O(1) | Fixed-interval retry: thousands of agents retry in lockstep after an outage (thundering herd) |
| 10 | Server ordering | Readers must never see a gap | **Contiguous watermark**: the highest seq such that every lower seq is confirmed, advanced over a **bitmap** of received seqs | Amortized O(1) per confirm | Reading whatever chunks exist: a pull could silently miss a chunk still in flight |
| 11 | Single writer | Two machines appending to one session | **Lease + fencing token** (monotonic counter). The server rejects writes carrying an older token. | O(1) | A plain lock: a paused or zombie writer wakes up and corrupts the session |
| 12 | Code snapshot | Detect working-tree changes cheaply | **Git's Merkle DAG**: compare the tree hash from `write-tree` | O(changed files) thanks to git's index cache | Hashing every file: O(repo size) every 60 seconds |
| 13 | Database indexes | Range scans on billions of rows | **B+tree** (Postgres btree) on `(project_id, last_activity_at desc)` and similar | O(log\_b n), b ≈ hundreds, so 3–4 page reads | **Binary search tree:** log₂ n ≈ 30 levels, each a random disk read. Fine in RAM, wrong on disk. |
| 14 | Viewer | Load the chunk holding byte offset X | **Binary search** over the sorted `byte_start` array | O(log n) | Linear scan of chunks |
| 15 | File lookup | "Which sessions touched `webhook.ts`?" | **Inverted index** (Postgres GIN) on `files_touched` | O(log n) per term | `LIKE` scans: O(n) |
| 16 | Semantic search | Nearest summaries to a query | **HNSW** graph index (pgvector) | Approximately O(log n) per query | Exact scan: O(n). IVFFlat: needs retraining as data grows and loses recall. |
| 17 | Fork lineage | Show a session's family | **Tree**: adjacency list (`parent_session_id`) + **materialized path** (`ltree`) | Ancestors and subtree in O(depth) with an index | Recursive query alone: fine for small trees, slow for deep lineages at scale |
| 18 | Path rewrite | Map old paths to new, never a partial match | **Trie keyed by path segments**, longest-prefix match | O(path length) | String `replace`: `/acme-api` would also corrupt `/acme-api-old` |
| 19 | Pull | Leave no half-finished state on failure | **Undo stack** (LIFO): each completed step pushes its compensating action, and on failure the stack is popped and undone | O(steps) | Ad-hoc cleanup code: misses cases as steps grow |
| 20 | Summaries | Long sessions must not cost more over time | **Incremental fold**: Sₙ = summarize(Sₙ₋₁, new chunks) | O(new data) per update | Re-summarizing the whole transcript: O(session length) each time |
| 21 | Server jobs | Reliable async work | **Transactional outbox** → queue (`pgmq`, then Kafka or SQS at scale) with visibility timeouts, **partitioned by session ID** | O(1) enqueue and dequeue | Firing jobs straight from request handlers: lost on crash |
| 22 | API protection | Fair use per user and tenant | **Token bucket (GCRA)** rate limiting at the gateway | O(1) | Fixed windows: allow bursts of 2× the limit at window edges |
| 23 | Cell routing | Find a tenant's cell | **Directory** (hash map, cached) from tenant to cell | O(1) | Consistent hashing: moves tenants automatically when cells change, but tenants must be moved deliberately, with their data |

### Why these matter most

**The append-only log, cursor and commit-after-confirm (1, 4, 10) are the core of durability.** Together they give at-least-once delivery. Idempotent writes (I3) turn it into effectively-once. Every crash scenario reduces to "resend from the last confirmed offset", which is always safe.

**The min-heap scheduler (2)** lets one agent track thousands of sessions with a single timer. After a week offline, it drains in due-time order instead of all at once, and the bounded channel (3) caps memory at a fixed number of in-flight chunks.

**Aho–Corasick plus RE2 (5)** makes redaction both fast and safe. Aho–Corasick checks every rule's keywords in one linear pass, so 200 rules cost about the same as one. Go's `regexp` package is RE2, which guarantees linear-time matching. A hostile transcript line therefore cannot freeze the agent with catastrophic backtracking, which is a real risk with most regex engines.

**The hash chain (8)** makes integrity cheap to prove. The server stores the chain head per session. A puller recomputes it while downloading, and any missing, swapped or altered chunk changes the head.

**B+trees instead of binary trees (13).** Binary search trees are the textbook answer for ordered lookups, but on disk each level is a random read. A B+tree packs hundreds of keys per 8 KB page, so a billion rows need about 4 reads instead of about 30. That's why every production database, Postgres included, uses B+trees, and why Carryon relies on them rather than inventing its own index.

## Upload protocol

Every chunk goes through the same five steps, and the agent advances its pointer only after the last one. A crash at any step is recovered by repeating from the start of that chunk. Repeats are harmless because every step is idempotent.

### The five steps

1. **Lease.** Once per session, then renewed with each chunk: `acquire-lease {session_id, machine_id}` returns `{fencing_token, expires_at}` with a 10-minute TTL. If another machine holds an unexpired lease, the server answers `409 held_elsewhere`, and the agent marks the session blocked as "active on another machine" (I7).
2. **Register.** `register-chunk {session_id, generation, seq, byte_start, byte_end, sha256, chain_hash, size, dict_id, fencing_token}`. The server checks:
   - the fencing token is current
   - `seq` is not below the watermark
   - `byte_start` equals the previous chunk's `byte_end`

   It then returns a pre-signed PUT URL for `<tenant>/<project>/<session>/<generation>/<seq>.zst`, valid 5 minutes. The idempotency key is `(session_id, generation, seq)`.
3. **Upload.** PUT the bytes straight to object storage with a checksum header, so storage itself rejects corrupted bodies (I9).
4. **Confirm.** `confirm-chunk {session_id, generation, seq, fencing_token}`. The server checks the object exists with the expected size and checksum. In one transaction it:
   - inserts the chunk row (`ON CONFLICT DO NOTHING` when the SHA matches, `409` if it differs, which should be impossible)
   - advances the contiguous watermark and chain head
   - writes an outbox event `chunk_confirmed` for the summary and search jobs
5. **Commit.** Only now does the agent atomically write its pointer `{offset, next_seq, chain_hash}`: write a temp file, `fsync`, then `rename`.

### What happens if it crashes at each step

| Crash after | State left behind | Recovery on restart | Result |
| --- | --- | --- | --- |
| 1. Lease | Lease held, nothing uploaded | Lease is renewed with the same machine ID | No effect |
| 2. Register | Registration with no object | Register again, which returns a fresh URL | No effect |
| 3. Upload | Object stored, not confirmed | Upload again (overwrites the identical bytes), then confirm | No duplicate. An unconfirmed object is garbage-collected after 24 hours if never confirmed. |
| 4. Confirm | Row committed, local pointer not advanced | Same chunk is sent again, and confirm sees the matching SHA and returns success | No duplicate (I3) |
| 5. Commit | Fully done | Nothing to do | — |

### Ordering and gaps

The agent keeps at most one chunk in flight per session, so sequence numbers arrive in order in normal operation. Different sessions upload in parallel. The server still assumes nothing about arrival order. It records confirmed seqs above the watermark in a bitmap and advances the watermark only across a contiguous run. Readers, meaning pulls and the viewer, see only data up to the watermark, so they can never observe a gap (I4).

### Generations

If the agent detects that a session file was rewritten instead of appended (it got smaller, or its first 4 KB changed), it starts a new **generation**: `generation + 1`, `seq` reset to 0, and the whole file uploaded again. Readers always use the newest generation whose watermark covers the whole file. Older generations are kept for 7 days, then deleted. Under I2 this should be rare, but the protocol never assumes Claude Code won't do it.

### Code snapshots

Snapshots use the same idempotent pattern. The agent pushes the commit to `refs/carryon/<user>/<session>` (or uploads a bundle), then calls `register-snapshot {session_id, seq, commit_sha, tree_sha}`. Git refs are content-addressed, so a repeated push of the same commit is a no-op.

### Why not a message broker on the laptop path

A broker such as Kafka adds an always-on dependency between laptops and storage, for no gain. Object storage already provides durable, horizontally scaled writes, and pre-signed URLs let millions of clients write to it directly. Brokers appear only *behind* the confirm step, where server-side jobs fan out (§ Server data architecture).

## Session state machine

&#91;embedded content: session state machine · 8 states\]

The agent stores each session's state in its pointer file, and the server mirrors it in `sessions.status`. Transitions happen only along the drawn arrows, and the code implements them as an explicit transition table, not scattered `if` statements, so an illegal transition is a compile-time or test-time error. **Blocked** covers the cases that need a person: lost access (`403`), a revoked login, or the session active on another machine. `carryon status` shows the reason and the fix.

## Server data architecture

Postgres holds small, strongly consistent metadata. Object storage holds the large, immutable bytes. Background work runs from a queue fed by a transactional outbox, so request handlers stay fast and nothing is lost on a crash.

### Multi-tenancy from day one

Every table carries `tenant_id` (the organization) as the leading column of its primary key and of every index. This costs nothing at 10 users. At millions, it lets any tenant's data be moved to another database cell without rewriting a single query (§ Scaling). The tenant ID is also a custom claim in the user's JWT, so row-level security can filter on it without a join.

### Tables, keys and partitioning

| Table | Primary key | Partitioning | Why |
| --- | --- | --- | --- |
| `sessions` | `(tenant_id, id)` | None until tier 2, then hash by `tenant_id` | Hot, small rows, read by the web app list |
| `session_chunks` | `(tenant_id, session_id, generation, seq)` | **Range by month** of `created_at` | The highest-volume table. Monthly partitions make retention a metadata-only `DROP PARTITION`, not a billion-row `DELETE`. |
| `session_manifests` | `(tenant_id, session_id, generation)` | Hash by `tenant_id` | Compacted chunk lists for ended sessions (below) |
| `leases` | `(tenant_id, session_id)` | None | Tiny, updated with every chunk |
| `audit_log` | `(tenant_id, id)` | Range by month | Append-only, and retention is per tenant |
| `outbox` | `(id)` | None | Short-lived: rows are deleted once relayed |

### Compaction: keeping the database small

A long live session may produce hundreds of chunk rows and objects. That's fine while it is live, but at scale it is the main source of database growth. When a session ends, a background job:

1. Writes one **manifest object** listing every chunk with its byte range, SHA-256 and the chain head
2. Optionally merges small chunk objects into a few large ones, the same idea as compaction in log-structured storage engines. This cuts storage request costs and speeds up pulls.
3. Replaces the session's chunk rows with one `session_manifests` row

The database then holds about one row per ended session instead of hundreds. Pulls read the manifest. Live sessions still use chunk rows.

### Row-level security that stays fast

Every policy calls `has_role(tenant_id, project_id, min_role)`, a `STABLE SECURITY DEFINER` function doing one B+tree lookup on `project_members (user_id, project_id)`. Because the function is marked stable, Postgres evaluates it once per statement rather than once per row. Policies are tested with pgTAP for every role and every table (TRD CO-302).

### Asynchronous work

`confirm-chunk` and `end-session` write an **outbox** row in the same transaction as their main change. That gives an atomic guarantee: if the chunk exists, the follow-up job will run. A relay moves outbox rows onto queues, and workers process them:

| Queue | Work | Coalescing |
| --- | --- | --- |
| `summarize` | AI title and summary (incremental fold) | Keyed by session: a newer event replaces a pending one, so a busy session triggers one job, not fifty |
| `embed` | Search vectors | Keyed by session |
| `rescan` | Server-side secret scan of new chunks | None: every chunk is scanned once |
| `compact` | Manifest and object compaction | Keyed by session, ended sessions only |
| `notify` | Slack messages | Keyed by event |

Workers are idempotent and receive messages at least once. A message stays invisible for 5 minutes while it is being processed. After 5 failures it moves to a dead-letter queue and alerts an admin. Tiers 1 and 2 use `pgmq` (a queue built on Postgres with `SKIP LOCKED`). Tier 3 uses a partitioned log (Kafka or SQS FIFO) keyed by session ID, which keeps per-session ordering while spreading load.

### Consistency model

- Metadata in Postgres is strongly consistent.
- Object storage on S3-compatible backends gives strong read-after-write consistency for new objects. Chunk objects are written once and never modified, which avoids the edge cases of overwrites.
- The watermark is the only thing readers trust. A chunk is "visible" only once its confirm transaction commits, so a reader can never see a half-written state.

## Pull path

A pull is a streaming pipeline. Memory use depends on the chunk size, not the session size, so a 2 GB session pulls on an 8 GB laptop. Every step either completes or is undone.

### Pipeline

1. **Manifest.** `get-manifest` returns metadata, the ordered chunk list up to the watermark (or the compacted manifest), the chain head and the snapshot reference. Access is checked and audited here.
2. **Download.** A bounded worker pool (default 6) fetches chunks in parallel through signed URLs. Results go into a **reorder buffer**: a min-heap keyed by `seq`, so chunks can arrive in any order but are emitted strictly in sequence. The heap holds at most `pool size × 2` chunks, which caps memory.
3. **Verify.** Each chunk's SHA-256 is checked as it streams in, and the running hash chain is recomputed. At the end, the chain must equal the manifest's chain head and the total byte count must equal the last `byte_end`. Any mismatch aborts the pull and names the chunk.
4. **Decompress.** A streaming zstd decoder with the chunk's dictionary.
5. **Rewrite.** A streaming line scanner reads one JSON line at a time. The iterative JSON walker (explicit stack, depth limit) applies the path trie and the session-ID rewrite to string values. Lines that fail to parse pass through unchanged and are counted.
6. **Write.** The output goes to a temp file in the destination folder. It is `fsync`ed, then atomically renamed to `~/.claude/projects/<encoded>/<new-id>.jsonl`. Claude Code never sees a partial file.
7. **Launch** `claude --resume <new-id> --fork-session` in the restored worktree.

### Local chunk cache

Downloaded chunks are kept in `~/.carryon/cache`, a **content-addressed store** keyed by SHA-256, behind an **LRU cache** (hash map plus doubly linked list, O(1) get and evict) capped at 2 GB. Pulling the same live session again later downloads only the new chunks. Identical chunks shared between a session and its forks are stored once.

### Undo stack

Each step that changes something pushes its compensating action:

| Step | Pushed onto the undo stack |
| --- | --- |
| Worktree created | `git worktree remove --force <dir>` |
| Fork registered on the server | `abandon-fork <id>`, which marks the fork row abandoned |
| Temp file written | Delete the temp file |
| Session file placed | Delete the placed file |

On any failure or Ctrl-C, the stack is popped and each action run in reverse order. The machine is left exactly as it was, apart from the chunk cache, which is safe to keep. Undo actions are themselves idempotent, so a crash *during* rollback is fixed by running `carryon pull --cleanup`, which replays the persisted stack.

### Why streaming matters at scale

A batch design (download everything, then process) needs memory and disk equal to the full session several times over, and shows nothing until the end. The streaming design starts writing after the first chunk, keeps memory flat, and makes pull time roughly equal to download time.

## Scaling to millions of users

The same design runs at every size. What changes between tiers is how many copies of the server side exist, never the protocol or the laptop binary. Scale comes from three properties already in the design: bytes bypass the API (I9), APIs are stateless, and every row is keyed by tenant.

### Capacity estimates

Assumptions, to be replaced by measured values after Phase 1:

- 6 active hours per developer per workday
- 20 MB of raw transcript per day
- one chunk about every 2 minutes while active, with 2 API calls per chunk
- a 6× compression ratio
- daytime peak at 4× the 24-hour average

| Tier | Developers | API calls/s (avg → peak) | Chunks/day | New storage/day | Server shape |
| --- | --- | --- | --- | --- | --- |
| 1 | up to 1,000 | 0.4 → 2 (for 100 devs) | 18,000 (100 devs) | 0.35 GB | One Supabase project |
| 2 | up to 100,000 | 42 → 170 (10k devs) | 1.8 M | 35 GB | Larger Supabase plan, read replicas, partitioning, compaction |
| 3 | millions | 4,200 → 17,000 (1M devs) | 180 M | 3.5 TB (≈ 0.9 PB per year) | Cells, below |

Even at 1M developers, API traffic is modest per server, because each call is a small metadata write. The real pressures are the **number of chunk rows** and **storage volume**, and both are handled by design: compaction folds chunk rows into one manifest per ended session, monthly partitions make retention free, and per-tenant retention plus crypto-shredding keeps storage bounded.

### Tier 3: cell architecture

&#91;embedded content: tier 3 · cell architecture\]

A **cell** is a complete, independent copy of the server side: a stateless edge API, a Postgres cluster with read replicas, an object storage bucket and queue workers. A cell is sized for about 50,000 developers, roughly 850 calls/s at peak, which one well-tuned Postgres with connection pooling handles comfortably. So 1M developers means about 20 cells, and 10M means about 200. Adding capacity means adding cells. Nothing has to be re-architected.

- **Routing.** At login, the client asks the global control plane which cell its tenant lives in, and caches the answer. Every later request goes straight to that cell. The control plane is off the hot path, so it can't become a bottleneck.
- **Placement.** Small tenants share cells. Large or regulated tenants get a dedicated cell, in a specific region for data residency.
- **Moving a tenant** (rebalancing, or a residency change): copy its rows and objects to the new cell, catch up the changes, briefly freeze writes, flip the directory entry, then unfreeze. Agents queue locally during the freeze, so no developer is blocked (I1).
- **Blast radius.** An outage, bad deploy or noisy tenant affects one cell's tenants only. Deploys roll out cell by cell, starting with an internal canary cell.

### Bottlenecks and how each is removed

| Pressure | Shows up as | Fix |
| --- | --- | --- |
| Chunk rows | Database size and write load | Compaction to manifests, monthly partitions, longer debounce for idle sessions |
| Database connections | Pool exhaustion | Connection pooling (Supavisor or PgBouncer). Edge functions hold connections only for single statements. |
| A hot tenant | One tenant dominating a cell | Per-tenant rate limits, then move it to a dedicated cell |
| Storage requests | Cost and latency on pull | Object compaction for ended sessions, plus the local chunk cache |
| AI cost | Summary spend growing with activity | Per-session coalescing, the incremental fold, a small model, per-tenant budgets |
| Auth | Token checks on every call | JWTs are verified locally in each cell with cached public keys. The control plane is needed only at login and refresh. |

## Security architecture

Security rests on four layers, and each one assumes the layer before it might fail.

| Layer | Threat it stops | Mechanism |
| --- | --- | --- |
| **1. Laptop** | Secrets leaving the machine | Redaction before compression (I5): Aho–Corasick + RE2 + entropy, plus a path denylist that drops whole tool results for `.env`, keys and certificates. Lines over 32 MB are scanned in streaming windows that overlap by the longest rule length, so no secret can hide across a window boundary. |
| **2. Identity** | Stolen or leaked credentials | GitHub OAuth device flow. Access tokens last 1 hour. Refresh tokens live in the OS keychain and **rotate on every use with reuse detection**: if an old refresh token is replayed, the whole token family is revoked. Each laptop is a registered device that admins can revoke remotely. |
| **3. Authorization** | Seeing another team's or tenant's data | Row-level security on every row (I6). Signed URLs scoped to one object and one HTTP method, valid 5 minutes. Tenant ID baked into every storage path and checked by storage policies. |
| **4. Detection** | Anything that got past layers 1–3 | Append-only audit log, nightly server-side secret rescan, owners notified when their session is pulled, alerts on unusual pull volume per user |

### Encryption

- TLS 1.2+ for every connection.
- At rest, storage and database encryption by default.
- From tier 2, **envelope encryption per tenant**: each tenant has its own data key, wrapped by a cloud KMS key. Deleting a tenant's key makes all of its objects unreadable instantly (**crypto-shredding**), even in backups. This is how retention and "delete everything for client X" are guaranteed, not just attempted.

### Supply chain and updates

A compromised update channel would put code on every developer laptop, so it gets the strictest controls:

- Releases are built in CI from tagged commits, with reproducible builds.
- Binaries are signed (Sigstore/cosign), and macOS binaries are notarized.
- The update manifest follows **The Update Framework (TUF)** model. Root keys are kept offline. A release requires signatures from a threshold of keys (for example 2 of 3), which protects against rollback, freeze and mix-and-match attacks. One stolen key cannot ship a release.
- Admins can pin a version, so updates never land mid-incident.

### Pulled transcripts are untrusted input

A pulled session was written partly by another person, and it contains web pages and tool output Claude read. Text in it could try to instruct the puller's Claude (prompt injection). Mitigations:

- Resume is always a fork in an isolated worktree, never in the puller's main checkout.
- Carryon never passes permission-skipping flags. Claude Code's normal approval prompts stay on.
- The pull header warns when the session contains web fetches or tool output from outside the repo.
- The web viewer renders Markdown through a sanitizer with a strict Content Security Policy and no raw HTML, which prevents cross-site scripting through transcript content.

### API hardening

Every endpoint validates its input against a JSON schema and enforces size limits. Rate limits use a token bucket per user and per tenant. Edge Functions run with the caller's JWT, never the service key, except the queue workers, which use a narrowly scoped role.

## Failure modes and recovery

Every failure below has a detection signal and an automatic recovery path. None of them breaks I1 (the developer is never blocked), and only the last row can lose data.

| Failure | How it is detected | Impact | Recovery |
| --- | --- | --- | --- |
| Agent crashes or is killed | Service manager, missing heartbeat | Capture pauses. The session file is safe on disk. | Service manager restarts the agent, which resumes from the queue cursor. Alert after 24 h on a workday. |
| Laptop offline for days | Queue depth, lag metric | Server copy falls behind | Drains in due-time order on reconnect. Backoff with jitter prevents stampedes. |
| Supabase or storage outage | 5xx responses, circuit breaker opens | Uploads pause across all laptops | Local queues hold everything. On recovery, jittered backoff spreads the reconnect load. |
| AI model unavailable | Job failures | Summaries delayed | Jobs retry, then dead-letter. Capture and pull are unaffected. |
| Bad server deploy writes wrong metadata | Hash chain or byte-count mismatch on pull, canary checks | Pulls of affected sessions fail loudly instead of silently | Roll back. Chunk objects are immutable, so metadata can be rebuilt from them. |
| Claude Code changes its file format | Nightly compatibility CI, unknown line types counted | Viewer shows raw JSON for new types | Pull still works: bytes are copied exactly and only known string fields are rewritten. Teams stay on the pinned version until CI is green. |
| Claude Code rewrites a session file | File shrank or its first 4 KB changed | None | New generation uploaded (§ Upload protocol) |
| Same session active on two machines | Lease conflict (`409 held_elsewhere`) | The second machine's uploads pause | Status shows "active on another machine". The lease expires 10 minutes after the first machine stops. |
| Clock skew between laptops | Not needed: leases and TTLs use server time, and local debounce uses the monotonic clock | None | — |
| Poison job (always fails) | Retry count | One job stuck | Dead-letter queue and admin alert, while other jobs continue |
| Secret slips past laptop redaction | Nightly rescan | Secret stored server-side until the rescan | Chunk redacted in place, key rotation requested, event audited |
| Region outage (tier 3) | Health checks | Cell unavailable | Point-in-time database restore plus cross-region object replication. Targets: RPO ≤ 15 min, RTO ≤ 4 h. |
| **Laptop disk destroyed before upload** | — | **Loss of at most the last debounce window (about 1–2 minutes)** | Not recoverable. This is the one bounded loss, and the debounce setting controls its size. |

## What we guarantee and what we cannot

No real system is unbreakable. What makes Carryon robust is that every known failure has a detection signal and a recovery path, and the guarantees below are tested continuously rather than assumed.

### Guaranteed (by design, and verified by tests)

- **The developer is never slowed or blocked** by Carryon (I1).
- **No duplicates and no silent gaps.** Every confirmed byte appears exactly once, in order, and a reader never sees a gap (I3, I4).
- **Integrity is provable.** A pull either reproduces the uploaded bytes exactly, or fails and names the bad chunk.
- **Bounded loss.** The worst case is losing the last debounce window if a laptop's disk is destroyed before upload.
- **Pulls are reversible.** A failed pull leaves the machine as it was.
- **Tenant and project isolation,** as long as the row-level security policies are correct. That is why every policy has pgTAP tests in CI.

### Not guaranteed (and why)

| Limit | Reason | How we narrow it |
| --- | --- | --- |
| 100% secret detection | Redaction recognizes known patterns. A novel secret format can slip through. | Entropy checks, a nightly rescan with updated rules, and a rule that secrets never go into prompts |
| Claude Code format stability | The session format is internal to Claude Code and may change between versions | Pinned version, nightly compatibility CI, and byte-exact pass-through of anything unrecognized |
| Identical behavior after resume | The puller's personal memory, MCP servers and settings may differ from the original developer's | Commit project rules and MCP config to the repo's `CLAUDE.md` and `.claude/` |
| Protection from a malicious tenant admin | Admins legitimately have broad access | Every admin action is audited and visible to developers |
| Perfect summaries | AI summaries can be wrong | Labelled as AI-generated, editable, and never used for access decisions |

### How robustness is verified

- **Property-based tests:** for any generated transcript and any sequence of crashes, network drops and retries, `rebuild(upload(x)) == redact(x)`.
- **Fuzzing** (Go native fuzzing) of the reader, JSON walker, redactor and path rewriter with hostile input: huge lines, deep nesting, invalid UTF-8, unusual escapes.
- **Formal model of the upload protocol:** a TLA+ specification of leases, fencing tokens, confirms and the watermark, model-checked for the properties "no two writers", "no gaps visible" and "no lost confirmed chunk". Distributed protocols are where intuition fails most, and model checking finds bugs that tests miss.
- **Chaos tests** in CI and staging: kill the agent at random points, cut the network, restart Postgres mid-confirm, then check every session rebuilds byte for byte.
- **Load and soak tests** at 10× expected volume before each scale tier is entered.

## Architecture decision records

Each decision lists the options considered. Reverse one only with a new ADR that explains what changed.

| ADR | Decision | Alternatives rejected | Main reason |
| --- | --- | --- | --- |
| 001 | Go for the laptop binary | Rust, TypeScript/Bun | Single static binary, cross-compiling, gitleaks as a library, team speed. Rust's speed is unused because the work is I/O-bound. |
| 002 | Postgres for metadata, object storage for bytes | Everything in Postgres, a document database | Bytes are large and immutable, metadata is small and relational. Each store does what it is best at. |
| 003 | Append-only file log for hook-to-agent handoff | Socket to the agent, SQLite | Works when the agent is down (I1), has no lock contention, and is crash-safe |
| 004 | Direct-to-storage uploads with pre-signed URLs | Uploading through the API | API servers stay out of the byte path (I9) and scale independently |
| 005 | At-least-once delivery plus idempotency | Exactly-once delivery | Exactly-once delivery across a network is impossible in general. Idempotent writes give the same result reliably. |
| 006 | Leases with fencing tokens | Plain locks, no locking | Survives paused or zombie writers (I7) |
| 007 | Hash chain for integrity | Merkle tree, no verification | Chunks are only appended and read in order, so a chain is O(1) per append and sufficient |
| 008 | Redaction on the laptop, rescan on the server | Server-only redaction | Secrets should never leave the machine. The server rescan is the backstop, not the plan. |
| 009 | RE2 (Go `regexp`) for all pattern matching | PCRE-style engines | Linear-time guarantee, so hostile input can't freeze the agent |
| 010 | zstd with a trained dictionary | gzip, zstd without a dictionary | Better ratio on small JSON chunks, at high speed |
| 011 | B+tree, GIN and HNSW indexes in Postgres | Custom index structures, a separate search engine | Proven, transactional and covered by row-level security. Revisit only if a measured bottleneck appears. |
| 012 | Resume always as a fork in a separate worktree | Resuming in place in the user's checkout | Never touches the puller's work or the original session, and contains prompt injection |
| 013 | Cell-based scaling with a tenant directory | One global database, consistent hashing | Blast-radius isolation, data residency per tenant, deliberate tenant moves |
| 014 | Supabase for tiers 1–2, self-hostable stack for tier 3 | Building on raw cloud services from day one | Fastest path to working software. The same Postgres and S3 interfaces carry over to tier 3 unchanged. |

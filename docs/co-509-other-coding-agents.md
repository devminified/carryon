# CO-509: capturing sessions from other terminal coding agents

**Decision: go for OpenAI Codex CLI, via a new `internal/agents/<name>` capture-layer interface; no-go (for now) on Aider; defer Gemini CLI pending a closer look at its own hook payload schema. Build none of this in Phase 5 — this ticket is the ADR only, per its own "don't build it in this phase" instruction.**

## Context

Carryon's entire capture pipeline (`internal/hook`, `internal/agent`, `internal/reader`, `internal/claudepath`) is written against one very specific contract: Claude Code's own hook system (`SessionStart`/`SessionEnd`/etc., each firing with `session_id`/`transcript_path`/`cwd` on stdin) and its own on-disk JSONL transcript format under `~/.claude/projects/<encoded-path>/`. This ticket asks whether that same approach — a hook that fires on real events, reading an append-only transcript file the agent itself already writes — generalizes to other terminal coding agents Devminified's clients use, starting with OpenAI Codex CLI, and if so, how the capture layer should be restructured so a second agent doesn't mean a second copy of `internal/agent`'s own daemon/debounce/upload wiring.

**Method, and its limit, stated up front:** this research was done by (1) installing the real, current OpenAI Codex CLI package (`@openai/codex@0.167.1`) into an isolated scratch directory and inspecting its actual `--help` output, subcommand list, and `codex doctor` diagnostics directly — not assumed from docs — and (2) cross-referencing multiple public sources (official docs where available, community write-ups otherwise) for details that required either a live account or a local model runtime to observe directly. No OpenAI API credentials or local model runtime (e.g. Ollama) were available in this environment, so **no real session was ever actually captured** — the ticket's own "prototype captures one Codex CLI session… if feasible within the time box" condition was checked and found not feasible here specifically because of that missing credential, not skipped for convenience. Facts below are marked `[verified]` (checked directly against the installed binary or an official doc page) or `[reported, unverified]` (from a public but third-party source, not independently confirmed).

## What was evaluated

### OpenAI Codex CLI — go

| Question | Answer |
| --- | --- |
| Where sessions live | `~/.codex/sessions/YYYY/MM/DD/rollout-<timestamp>-<id>.jsonl` `[reported, unverified — no live session was produced to confirm the exact path directly]` |
| Append-only? | Described as append-only JSONL by every source checked, same shape as Claude Code's own transcript files `[reported, unverified]` |
| Resume command | `codex resume [SESSION_ID] [PROMPT]`, `--last` for the most recent, a picker by default `[verified — real `--help` output from the installed binary]` |
| Fork | `codex fork`, same shape as `resume` (picker or `--last`) `[verified]` |
| Lifecycle management | `codex archive`/`codex unarchive`/`codex delete`, plus `codex migrate-rollouts` for "legacy local sessions to paginated thread history" — i.e. Codex's own on-disk format has already changed at least once across versions `[verified — all four are real subcommands in `codex --help`]` |
| Hook/notify mechanism | A real hook-trust system exists: `codex resume --dangerously-bypass-hook-trust` is a real, documented flag ("Run enabled hooks without requiring persisted hook trust for this invocation") `[verified — real flag text from `codex resume --help`]`. Multiple third-party sources describe a fuller lifecycle-hook set named identically to Claude Code's own (`SessionStart`, `SessionEnd`, `PreToolUse`, `PostToolUse`, `UserPromptSubmit`, …), configured via `~/.codex/hooks.json` or a `[hooks]` table in `~/.codex/config.toml`, with `SessionEnd` receiving `session_id`/`cwd`/`transcript_path`/`reason` on stdin `[reported, unverified — this exact payload shape was not confirmed against a live hook firing]` |
| Non-interactive mode | `codex exec` (alias `e`) — the same shape as `claude -p`, useful for the nightly compat-check-style scripted sessions `.github/scripts/claude-compat-check.sh` already uses for Claude Code `[verified]` |
| Daemon / remote protocol | `codex app-server` and `codex remote-control` run a background server with a websocket-based remote-control protocol (`--remote <ws://…>`, `--remote-auth-token-env`) — a second, lower-level integration point beyond hooks, not explored further here since the hook-based approach already looks sufficient and is the closer analog to Carryon's existing design `[verified — real flags/subcommands]` |

**Why this is a go:** every piece Carryon's capture pipeline actually depends on for Claude Code — a real lifecycle hook firing on session end with the session id and transcript path, and an append-only, line-oriented transcript file on disk — has a same-shaped counterpart in Codex CLI, confirmed as real (not merely rumored) subcommands and flags on the actual installed binary. The remaining unverified details (exact rollout JSONL line schema, exact hook JSON payload) are implementation detail to pin down when this is actually built, not open questions that would change the go/no-go call.

### Aider — no-go, for now

`[verified directly against aider.chat's own official docs page](https://aider.chat/docs/config/options.html)`:

- History is **one file per project** (`.aider.chat.history.md`, overridable via `--chat-history-file`/`AIDER_CHAT_HISTORY_FILE`), not one file per session — every session in a project appends to the *same* file, delimited by a `# aider chat started at <timestamp>` heading line, with no separate session identifier Carryon could key a `sessions` row on without first inventing its own segmentation logic.
- Aider does **not** load its own history back by default — `--restore-chat-history` is off unless explicitly passed, meaning a plain `aider` resume doesn't resume from this file the way `claude --resume`/`codex resume` do.
- No lifecycle hook or notify-on-event mechanism exists at all. The only related flag is `--notifications` — a terminal bell when a response is ready, with nothing resembling `SessionEnd`.

**Why no-go:** Carryon's own invariant I3 (idempotent writes keyed by `(session_id, seq)`) and I7 (one writer per session, leases) both assume a session boundary the *agent itself* defines. Aider defines no such boundary on disk — Carryon would have to invent one (e.g. treating a gap in file-modification time as a session break), which is exactly the kind of fragile heuristic Claude Code's and Codex's own real `SessionStart`/`SessionEnd` events were built to avoid needing. Revisit only if Aider ships a real per-session file and hook model later.

### Gemini CLI — defer

`[verified directly against the official `gemini-cli` GitHub docs page]`: sessions live at `~/.gemini/tmp/<project_hash>/chats/`, project-scoped (keyed by a hash of the project root, the same idea as Claude Code's own `claudepath`-encoded project directories), with `--resume`/`--resume <id>`/`--resume <index>` support. A real `gemini hooks` CLI command exists specifically to **migrate hooks from Claude Code**, and a separate `/hooks` in-session command manages Gemini's own lifecycle hooks — both `[reported, unverified — found via search, not confirmed against an installed Gemini CLI binary in this session]`. This is a promising signal (a hook system deliberately compatible enough with Claude Code's own to warrant a migration command), but the exact file format (JSON vs JSONL, one file per session or appended) wasn't nailed down by the official docs page itself and wasn't independently verified the way Codex CLI was (no real Gemini CLI install was attempted in this timebox). **Defer**: worth a real look (install the binary, inspect `--help` and a real session file the same way this ticket did for Codex) before the next time this question comes up — not ruled out, just not investigated deeply enough yet to commit engineering time to.

## The pluggable capture-layer design (for when this is actually built)

Not built in this ticket, per its own instruction — this is the shape a future ticket should follow, based on what `internal/` already separates cleanly today:

```
internal/agents/
  claude/      the EXISTING internal/hook + internal/claudepath logic, moved here unchanged
  codex/       new: Codex's own hook payload parsing + ~/.codex/sessions/ path encoding
  <future>/
```

Every agent package implements one small interface — something like:

```go
type Agent interface {
    Name() string
    // HookEventToQueueEntry parses that agent's own hook stdin JSON
    // into the same internal/queue entry shape every agent produces,
    // so internal/agent's daemon/debounce/uploader never need to know
    // which agent originated a given queued session.
    HookEventToQueueEntry(stdin []byte) (queue.Entry, error)
    // TranscriptPath resolves an agent's own on-disk transcript
    // location for a given session id/cwd — Claude Code's own
    // claudepath encoding, Codex's YYYY/MM/DD/rollout-* layout, etc.
    TranscriptPath(sessionID, cwd string) (string, error)
}
```

`internal/hook` becomes a thin dispatcher: read stdin, sniff which agent invoked it (Codex's own hook JSON and Claude Code's own hook JSON are differently shaped, so this is a real, cheap discriminator, not a guess), hand off to that agent's `HookEventToQueueEntry`. Everything downstream of the queue — `internal/agent`'s daemon, `internal/upload`, `internal/redact`, the whole server side — is already agent-agnostic by construction (a queue entry is just session id + bytes), so none of it needs to change. The one real new surface: `register-session`/`get-manifest`'s own session metadata would need an `agent` column (`'claude-code' | 'codex'`, default `'claude-code'` for every existing row) so the web viewer and `carryon pull` know which transcript-schema parser to use when rendering/placing a pulled session — `web/src/modules/sessions/viewer/lib/parse-transcript.ts` is Claude-Code-schema-specific today and would need a matching `parse-transcript-codex.ts` sibling, selected by that same column.

## Recommendation

**Go** on Codex CLI as the second supported agent, once Phase 5 wraps and a real ticket is written for it — the hook and transcript-file shapes are close enough to Claude Code's own that the existing architecture (hook → queue → agent daemon → redact → upload) carries over with the `internal/agents/<name>` split above, not a rewrite. **No-go** on Aider as it stands today. **Defer** on Gemini CLI pending the same direct-install verification this ticket did for Codex.

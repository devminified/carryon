# FAQ

**Does Carryon slow down Claude Code, or block me while it captures a session?**

No. The hook that fires on every Claude Code event only appends a line to a local queue file — under 50ms at p99 (NFR-01) — and always exits 0, even on error. All the real work (redaction, compression, upload) happens in a separate background agent, off that path entirely. See `.claude/rules/go-conventions.md`'s "The hook path is latency-critical" section.

**What exactly leaves my laptop?**

A redacted, compressed copy of your session transcript and a lightweight git snapshot (a hidden ref, or a bundle if your remote doesn't support one) — never your working directory's raw files, and never anything matched by the current redaction rule set (`internal/redact`: ~44 hand-ported gitleaks-pattern rules plus a path denylist for things like `.env` files and private keys, checked before compression, with no opt-out). See `.claude/rules/redaction.md`.

**What if the redaction engine misses something?**

Two backstops exist, in addition to the laptop-side engine: a nightly server-side rescan re-checks recently uploaded chunks (and can be told to re-check everything after a rule-set update) and rewrites a hit in place; and the rule set itself is server-configurable (an admin can disable a rule or extend the path denylist without a new binary release). Neither backstop is a reason to loosen the laptop-side rules — see `.claude/rules/redaction.md`'s own "server-side rescan is a backstop, not the primary defense" section.

**Who can see my sessions?**

Whoever has `viewer` role or higher on the project you're working in — mirrored from your GitHub team membership, re-synced every 15 minutes. `viewer` can see the list and read transcripts; `resumer` can also pull/resume; `admin` can also delete (with a mandatory logged reason). Every role check is enforced by Postgres Row-Level Security, not just hidden in the UI — see `.claude/rules/auth.md`'s role table.

**Can I use this with an agent other than Claude Code?**

Not yet. `docs/co-509-other-coding-agents.md` is a research ticket assessing OpenAI Codex CLI, Aider, and Gemini CLI for a future pluggable capture layer (`internal/agents/<name>`) — nothing has been built against another agent yet, and the current recommendation is "go" on Codex CLI specifically, "no-go for now" on Aider, with Gemini CLI deferred. Read that doc for the full reasoning before assuming support exists.

**Does Carryon work if my team is offline or on a flaky connection?**

Yes — uploads are retried and are safe to retry (idempotent on `(session_id, seq)`), and the laptop agent tolerates extended offline periods, catching up once connectivity returns. `internal/loadtest`'s own load/soak tooling (CO-505) specifically exercises this under simulated random network drops as part of verifying no data loss under load.

**Can I self-host this instead of using Supabase Cloud?**

Yes — `deploy/selfhost/` is a Docker Compose stack running Supabase's own self-hosting reference images (Postgres, Auth, REST, Realtime, Storage, Edge Functions) plus the web app behind one Kong gateway. Point the `carryon` binary at it the same way you'd point it at any other backend. See `deploy/selfhost/README.md` for the full setup.

**Does Carryon call the Anthropic API with my raw conversation?**

No. The only thing that ever reaches Anthropic is already-redacted, already-condensed text, used solely to generate a session's title/summary (Claude Haiku) — never a raw transcript line, and never anything that bypassed the laptop's redaction engine on the way up. If no `ANTHROPIC_API_KEY` is configured, summarization is skipped entirely rather than faked.

**What happens to a session after its project is closed?**

A project can be marked closed with a retention window (`retention_days_after_close`, default 90 days); a daily sweep purges that project's sessions, chunks, snapshots, and audit rows once the window elapses — supporting a dry-run mode to preview what would be deleted first. See `docs/co-307-retention-dry-run.md`.

**I think I found a security issue — where do I report it?**

Not as a public GitHub issue — see `SECURITY.md`.

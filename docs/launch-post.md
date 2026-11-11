# Launch post draft (CO-508)

> Status: draft, not published anywhere. This is the prepared material for whoever at Devminified decides to post it — a blog, Dev.to, HN, wherever fits — not something this ticket publishes on its own.

---

## Carryon: pick up a teammate's Claude Code session exactly where they left it

Every team that's adopted Claude Code hits the same moment eventually: someone's deep into a long session — the agent has read half the codebase, tried three approaches, has a working mental model of a gnarly bug — and then they have to step away. A meeting, the end of the day, a laptop that needs to go to the shop. Whoever picks it up next starts from zero: no transcript, no code snapshot, just whatever's in the PR description if they're lucky.

We built Carryon to fix that specific problem — the **urgent handoff**, not the leisurely one. It captures every Claude Code session on a registered repo as it happens, uploads it (redacted, encrypted in transit and at rest) to your own Supabase project, and lets a teammate pull it back down later — the conversation *and* the code, including uncommitted work — and resume exactly where it left off, with Claude Code's own `--resume --fork-session`.

### What it isn't

It's not a replacement for git, for your PR process, or for actually talking to your teammate. It's the thing that exists for the gap between "I need to hand this off right now" and "we had time to write it up properly."

### How it works, briefly

- A lightweight hook captures every Claude Code event into a local queue — it never blocks you, and it's append-only, so a crash or an offline stretch never loses data.
- A background agent redacts anything that looks like a secret (an API key, a private key, a connection string — about 44 pattern rules, checked before anything leaves your laptop) and uploads the rest, chunked and content-addressed.
- Code travels as a git snapshot (a hidden ref, or a bundle if your remote can't take a push) — not a zip, not a diff pasted somewhere.
- Everything is scoped by Postgres Row-Level Security, with roles (`viewer`/`resumer`/`admin`) mirrored straight from your GitHub team membership — no second place to manage access.
- A teammate runs `carryon pull <session-id>`: it clones or locates your repo, restores the snapshot into its own worktree, rebuilds the transcript byte-for-byte, and launches Claude Code resumed, right there.

There's also a web app if you'd rather browse than use the CLI — a searchable session list, a read-only transcript viewer with redaction highlighting and the full tool-call/diff history, and an admin console for managing projects and reviewing the audit log.

### Where it stands

Phases 1 through 4 are built and in use internally: capture, upload, pull/resume, roles and RLS, laptop-side redaction with a nightly server-side rescan backstop, fleet packaging with a signed auto-update channel, the web app, AI-generated session summaries, and Slack notifications. Phase 5 — semantic search, a security review and threat model, self-hosting, and this open-source release — is what just landed.

### Self-hosting

If putting session transcripts on our Supabase Cloud project isn't an option for your team, `deploy/selfhost/` runs the whole backend (Postgres, Auth, Storage, Edge Functions, Realtime — the same self-hosting images Supabase itself publishes) plus the web app, in one `docker compose up`.

### Try it

```bash
curl -LO https://github.com/devminified/carryon/releases/latest/download/carryon_<your-platform>.tar.gz
# extract, carryon login, carryon admin project add, carryon install
```

Full install steps, architecture, and an FAQ are in the [README](https://github.com/devminified/carryon#readme) and [docs/](https://github.com/devminified/carryon/tree/main/docs). It's MIT-licensed — read it, fork it, send a PR.

---

*Open questions for whoever edits this before posting: confirm the actual release download command matches whatever the real v0.4.0 (or later) artifact naming ends up being: swap `<your-platform>` for a real example. Confirm "Devminified" is the name we want attached publicly before this goes out — that's the trademark/attribution call `docs/co-508-validation-checklist.md` flags as legal's to make, not this draft's.*

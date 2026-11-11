# Carryon docs

- **Quick start** — the top-level [README](../README.md)'s own "Install" and "Set up" sections: download or build the binary, point it at a backend, log in, register a repo, and install the hooks/agent.
- **[Architecture](architecture.md)** — the three trust zones, the pull/resume path, and the four security layers, with diagrams.
- **Self-hosting** — [`deploy/selfhost/README.md`](../deploy/selfhost/README.md): run Carryon's own backend (Postgres, Auth, Storage, Edge Functions, Realtime) plus the web app on your own infrastructure via Docker Compose.
- **[FAQ](faq.md)** — common questions about what leaves your laptop, who can see what, and what's and isn't built yet.

## Deeper reference

- `CLAUDE.md` and `.claude/rules/` — the binding engineering conventions this repo's own contributors follow (architecture, database/RLS, redaction, testing, naming, git workflow).
- `Devminified Carryon — System Design Document (SDD).md` — the full design doc: invariants, algorithms, scaling tiers, the complete security architecture.
- `Overview.md` and `Carryon-TRD-Tickets.md` — the product pitch and the full 52-ticket build plan.
- `docs/co-507-threat-model.md` — the STRIDE threat model, with real tool-run results (gosec, govulncheck, container scans).
- `docs/co-505-load-test.md` — how to run the load/soak/cost test, and how to read its report.
- `docs/co-508-validation-checklist.md` — what this open-source release prepared versus the two human-only sign-offs (legal, a real outside-developer install timing) it doesn't self-certify.
- `docs/launch-post.md` — the drafted launch post about the urgent-handoff problem (not yet published).
- Everything else under `docs/` — one doc per ticket decision worth recording (an ADR, a role matrix, a dry-run report) — browse the directory for the full list.

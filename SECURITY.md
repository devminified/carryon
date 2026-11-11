# Security Policy

## Reporting a vulnerability

**Please do not open a public GitHub issue for a security vulnerability.**

Email **development@devminified.com** with:

- A description of the issue and its impact.
- Steps to reproduce (a minimal repro is ideal — a specific request, input, or sequence of CLI/API calls).
- The affected version or commit.

We'll acknowledge your report within 3 business days and aim to have a fix or mitigation plan within 14 days for a confirmed high/critical issue. We'll credit you in the fix's release notes unless you'd rather stay anonymous.

If you believe you've found a vulnerability in a **dependency** (a Go module, an npm package, a Supabase/Postgres component) rather than in Carryon's own code, please report it to that project directly as well — we'll still want to know, since it may affect how we use it.

## What's in scope

- The `carryon` laptop binary (`cmd/`, `internal/`) — the hook, the capture agent, `pull`/`export`, redaction, auth.
- The Supabase backend — migrations, RLS policies, Edge Functions (`supabase/`).
- The web app (`web/`) — the session viewer, the admin console, auth/session handling.
- The self-hosted deployment (`deploy/selfhost/`) — the Docker Compose stack and its own hardening.

## What's already documented

`docs/co-507-threat-model.md` is a STRIDE threat model covering the areas most likely to matter here: a stolen laptop token, a malicious insider pulling sessions, a compromised Edge Function, secret leakage through a redaction gap, a supply-chain attack on the binary or update channel, and prompt injection through a pulled transcript — each mapped to a real, cited mitigation and test already in this codebase. Read it before reporting a finding that might already be a known, mitigated, and documented risk (though please still report it if you think the mitigation doesn't actually hold — that's exactly the kind of finding we want).

## Supported versions

Carryon releases a new minor version per completed build phase (see the GitHub [Releases](https://github.com/devminified/carryon/releases) page). We fix security issues on the latest release; we don't maintain long-term support branches for older minor versions at this stage of the project.

## Secret handling, if your report involves one

If your report includes something that looks like a real credential (an API key, a token) that you found while investigating — mention that you found it and where, but don't paste the raw value into the report email. We'll rotate it on our end.

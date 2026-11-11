# Architecture

This is the public-facing companion to `.claude/rules/architecture.md` (the binding engineering reference this repo's own contributors follow) and the System Design Document — written for a reader who wants the shape of the system before diving into either. The diagrams below are new for this page; no earlier diagram asset existed to reuse, so these are drawn directly from the SDD's and `architecture.md`'s own text.

## Three trust zones

Carryon's security story rests on keeping three zones distinct — a laptop binary calling the Anthropic API directly, or a web app reading a laptop's local queue file, would blur a line the whole design depends on.

```mermaid
flowchart LR
    subgraph Laptop["Laptop (sensitive work)"]
        Hook["hook"] --> Agent["capture agent"]
        Agent --> Redact["redact"]
        Redact --> Upload["upload: compress, hash, chunk"]
    end

    subgraph Server["Server / cell"]
        EdgeFn["Edge Functions\n(metadata + signed URLs only)"]
        PG["Postgres\n(RLS-enforced)"]
        Storage["Object Storage\n(encrypted chunks)"]
    end

    subgraph Third["Third parties"]
        GitHub["GitHub\n(identity + code)"]
        Anthropic["Anthropic API\n(redacted, condensed text only)"]
        Slack["Slack\n(titles + links only)"]
    end

    Upload -- "signed URL, bytes bypass the API" --> Storage
    Upload -- "metadata only" --> EdgeFn
    EdgeFn --> PG
    EdgeFn -- "GitHub App: team membership only" --> GitHub
    EdgeFn -- "AI summary job" --> Anthropic
    EdgeFn -- "alerts, digests" --> Slack
```

**Laptop** does the expensive and sensitive work — redaction, compression, hashing (`internal/redact`, `internal/upload`, `internal/snapshot`). Nothing here blocks on the network except the agent's own background retries (I1: the developer is never blocked). **Server/cell** is Postgres, Storage, Edge Functions, and scheduled jobs — stateless, horizontally scalable, never a single point of global state. **Third parties** each get only what they strictly need: GitHub sees identity and code, the Anthropic API sees redacted/condensed text, Slack sees titles and links.

## The pull/resume path

The product's own core promise — resuming a teammate's live session, code and conversation both — crosses both trust zones twice.

```mermaid
sequenceDiagram
    participant A as Developer A's laptop
    participant Srv as Server (Edge Functions + Postgres)
    participant S3 as Object Storage
    participant B as Developer B's laptop

    A->>Srv: register-chunk (metadata)
    Srv-->>A: signed upload URL
    A->>S3: PUT chunk bytes directly (I9: bytes bypass the API)
    A->>Srv: confirm-chunk
    A->>Srv: register-snapshot (git ref or bundle)

    B->>Srv: get-manifest (role check: resumer+)
    Srv-->>B: chunk list + signed download URLs
    B->>S3: GET each chunk directly
    B->>B: verify SHA-256, rebuild, rewrite session/fork IDs
    B->>Srv: confirm-pull (audit log)
    B->>B: resume: claude --resume --fork-session
```

Every chunk is content-addressed (a per-chunk SHA-256) and the whole sequence is append-only and idempotent — a retried upload or a retried pull is always safe (I2, I3). RLS on `sessions`/`session_chunks` is what actually decides whether `get-manifest` hands Developer B anything at all; a `viewer` role gets zero chunks back, not an error that leaks whether the session exists.

## The four security layers

Per the System Design Document's own "Security architecture" section, in the order they're meant to hold:

```mermaid
flowchart TD
    L1["1. Laptop-side redaction\n(internal/redact — before a byte ever leaves)"]
    L2["2. Encryption in transit and at rest\n(TLS, Supabase Storage encryption)"]
    L3["3. Row-Level Security\n(Postgres RLS — the real authorization boundary)"]
    L4["4. Audit log\n(append-only, every view/pull/delete recorded)"]
    L1 --> L2 --> L3 --> L4
```

A secret that leaves the laptop unredacted is not fixed by any amount of RLS downstream — redaction is layer one, not a backstop. See `.claude/rules/redaction.md` and `docs/co-507-threat-model.md` for how each layer is actually tested, not just asserted.

## Where to read more

- `.claude/rules/architecture.md` — the five architectural principles in full, with the anti-patterns each one rules out.
- `.claude/rules/folder-structure.md` — the full repo tree.
- `docs/co-507-threat-model.md` — the STRIDE threat model, with real tool-run results.
- `Devminified Carryon — System Design Document (SDD).md` — the original design doc these diagrams are drawn from (invariants, scaling tiers, the full security architecture section).

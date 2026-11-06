# Go Binary Conventions

Applies to everything under `cmd/carryon/` and `internal/`.

## CLI surface (`spf13/cobra`)

Commands: `install`, `uninstall`, `login`, `hook`, `agent`, `status`, `ls`, `pull`, `export`, `rename`. Each command's `RunE` stays thin — parse flags, call into the matching `internal/` package, translate errors to exit codes. Business logic never lives in `cmd/carryon/main.go` or in a cobra `RunE` closure.

## The hook path is latency-critical (I1, NFR-01)

`internal/hook` must hit p99 under 50ms and **always exit 0**, even on error — a non-zero exit or a slow hook blocks the developer's Claude Code session. Any error the hook hits (can't open the queue file, disk full) gets logged and swallowed, never surfaced to the caller. Don't add anything to this path that can block: no network calls, no synchronous disk flush beyond the append itself.

## Errors

- Wrap with `fmt.Errorf("doing x: %w", err)` so the chain survives for logging; don't print-and-continue in the middle of a package, return the error to the caller.
- A function in `internal/` returns `(T, error)`, never panics on an expected failure (bad input, network timeout, disk full). Panics are for programmer errors only (nil pointer that should be impossible), and `main.go` recovers at the top level to log and exit cleanly.
- Errors that cross the laptop/server boundary (via `internal/api`) carry enough context to name the session and the step, per the Definition of Done's "logs and errors are actionable" rule.

## Context and cancellation

Every long-running call (`internal/upload`, `internal/pull`, `internal/api`) takes a `context.Context` as its first argument and respects cancellation. The agent's daemon loop cancels its context on shutdown signal (SIGTERM from the service manager) so in-flight uploads get a chance to persist state before exit, honoring I8 (crash-safe state).

## Concurrency

- `internal/agent`'s debounce/scheduler and `internal/pull`'s bounded worker pool are the two places true concurrency lives. Everywhere else, prefer a straight-line sequential call.
- Bound every worker pool and channel explicitly (the pull worker pool defaults to 6; the reorder min-heap caps at `pool size × 2`) — see Doc §"Pull path". Don't add an unbounded goroutine-per-item loop.
- A slow or failing session must not stall others: each pipeline stage after the reader operates on one session's slice at a time (Doc §"Laptop agent internals").

## Testing

`go vet`, `golangci-lint`, and `go test -race ./...` run in CI on every PR, then the binary cross-compiles for all five release targets. See `.claude/rules/testing.md` for the full CI matrix and `.claude/rules/qa-testing.md` for chaos/soak tests tied to the NFR table.

## Naming

Package names are short, lowercase, no underscores (`redact`, not `redact_engine`). Exported identifiers are `PascalCase`; unexported are `camelCase`. A package's name should answer "what does this do," not "what ticket added this" — see `.claude/rules/naming.md`.

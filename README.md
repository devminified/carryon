# Carryon

Capture every Claude Code session on a registered repo, upload it (redacted) to Supabase as it happens, and let a teammate pull it back down — code and conversation both — and resume exactly where it left off.

See `Overview.md` for the product pitch and 52-ticket build plan, `Devminified Carryon — System Design Document (SDD).md` for the architecture and invariants, and `Carryon-TRD-Tickets.md` for the full ticket descriptions. `CLAUDE.md` and `.claude/rules/` are the binding engineering conventions for this repo.

**Current status:** Phase 1 (`CO-101`) — the Go CLI scaffold only. Every subcommand below except `version` prints `not implemented` and exits `2`; they land in later tickets.

## Quick start

```bash
git clone https://github.com/devminified/carryon.git
cd carryon
go build -o carryon ./cmd/carryon
./carryon version
```

Requires Go 1.23+.

## Commands

| Command | Status | Ticket |
| --- | --- | --- |
| `carryon version` | implemented | CO-101 |
| `carryon install` | not implemented | CO-106 |
| `carryon login` | not implemented | CO-103 |
| `carryon hook` | not implemented | CO-105 |
| `carryon agent` | not implemented | CO-107 |
| `carryon status` | not implemented | CO-113 |
| `carryon ls` | not implemented | CO-204 |
| `carryon pull` | not implemented | CO-211 |
| `carryon export` | not implemented | CO-211 |
| `carryon rename` | not implemented | CO-405 |

## Development

```bash
go build ./...
go vet ./...
go test -race ./...
golangci-lint run      # errcheck, govet, staticcheck, gosec — see .golangci.yml
goreleaser build --snapshot --clean   # cross-compiles all 5 release targets into dist/
```

CI (`.github/workflows/ci.yml`) runs the same checks, plus `go build`/`go test -race` on macOS, Linux, and Windows runners, on every pull request.

## Repository layout

See `.claude/rules/folder-structure.md` for the full tree (`cmd/carryon/`, `internal/`, `supabase/`, `web/`). This repo is a monorepo — the Go binary, the Supabase backend, and the Next.js web app all live here, not in separate repos.

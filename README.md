# Carryon

Capture every Claude Code session on a registered repo, upload it (redacted) to Supabase as it happens, and let a teammate pull it back down — code and conversation both — and resume exactly where it left off.

See `Overview.md` for the product pitch and 52-ticket build plan, `Devminified Carryon — System Design Document (SDD).md` for the architecture and invariants, and `Carryon-TRD-Tickets.md` for the full ticket descriptions. `CLAUDE.md` and `.claude/rules/` are the binding engineering conventions for this repo.

**Current status:** Phase 1, through `CO-113`. `ls`, `pull`, `export`, and `rename` still print `not implemented` and exit `2`; everything else is implemented.

## Quick start

```bash
git clone https://github.com/devminified/carryon.git
cd carryon
go build -o carryon ./cmd/carryon
./carryon version
```

Requires Go 1.23+.

To use `login`/`logout` against local Supabase, create `~/.carryon/config.toml`:

```toml
api_url  = "http://127.0.0.1:54421"   # or a real project's URL
anon_key = "sb_publishable_..."        # from `supabase status` or the project's API settings
```

`login` also needs a GitHub OAuth App configured — see `.claude/rules/auth.md` §"GitHub OAuth App setup" and `.env.example`.

## Commands

| Command | Status | Ticket |
| --- | --- | --- |
| `carryon version` | implemented | CO-101 |
| `carryon admin project add` | implemented | CO-104 |
| `carryon login` | implemented | CO-103 |
| `carryon logout` | implemented | CO-103 |
| `carryon hook` | implemented | CO-105 |
| `carryon install` | implemented | CO-106 |
| `carryon uninstall` | implemented | CO-106 |
| `carryon agent` | implemented | CO-107 |
| `carryon status` | implemented | CO-113 |
| `carryon logs` | implemented | CO-113 |
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

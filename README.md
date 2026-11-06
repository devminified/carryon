# Carryon

Capture every Claude Code session on a registered repo, upload it (redacted) to Supabase as it happens, and let a teammate pull it back down — code and conversation both — and resume exactly where it left off.

See `Overview.md` for the product pitch and 52-ticket build plan, `Devminified Carryon — System Design Document (SDD).md` for the architecture and invariants, and `Carryon-TRD-Tickets.md` for the full ticket descriptions. `CLAUDE.md` and `.claude/rules/` are the binding engineering conventions for this repo.

**Current status:** Phase 1 ("working pull" — capture, upload, retry, status/logs) is complete, through `CO-113`. `carryon ls`, `pull`, `export`, and `rename` — the pull/resume side, Phase 2 — still print `not implemented` and exit `2`.

## Install

### Download a release (recommended)

Grab the archive for your platform from the [Releases page](https://github.com/devminified/carryon/releases/latest), then extract it and put `carryon` on your `PATH`:

```bash
# macOS (Apple Silicon)
curl -LO https://github.com/devminified/carryon/releases/latest/download/carryon_darwin_arm64.tar.gz
tar -xzf carryon_darwin_arm64.tar.gz
sudo mv carryon /usr/local/bin/

# macOS (Intel)
curl -LO https://github.com/devminified/carryon/releases/latest/download/carryon_darwin_amd64.tar.gz
tar -xzf carryon_darwin_amd64.tar.gz
sudo mv carryon /usr/local/bin/

# Linux (amd64)
curl -LO https://github.com/devminified/carryon/releases/latest/download/carryon_linux_amd64.tar.gz
tar -xzf carryon_linux_amd64.tar.gz
sudo mv carryon /usr/local/bin/
```

```powershell
# Windows (amd64) — PowerShell
Invoke-WebRequest https://github.com/devminified/carryon/releases/latest/download/carryon_windows_amd64.zip -OutFile carryon.zip
Expand-Archive carryon.zip -DestinationPath .
# move carryon.exe to somewhere on your PATH
```

Verify it landed:

```bash
carryon version
```

### Build from source

```bash
git clone https://github.com/devminified/carryon.git
cd carryon
go build -o carryon ./cmd/carryon
./carryon version
```

Requires Go 1.23+.

## Set up

1. **Point carryon at a backend.** Create `~/.carryon/config.toml`:

   ```toml
   api_url  = "https://xyz.supabase.co"   # ask whoever runs your Carryon backend for this
   anon_key = "sb_publishable_..."         # and this — from `supabase status` for local dev, or the project's API settings
   ```

   Running your own instance (local dev or self-hosted) instead of pointing at a shared one? `login` also needs a GitHub OAuth App configured against it — see `.claude/rules/auth.md` §"GitHub OAuth App setup" and `.env.example`.

2. **Log in with GitHub:**

   ```bash
   carryon login
   ```

   Opens your browser for a GitHub sign-in, then stores the session in your OS keychain.

3. **Register the repo** you want captured (once per repo, by anyone — the first person to register a repo becomes its admin):

   ```bash
   carryon admin project add acme-api git@github.com:devminified/acme-api.git
   ```

4. **Install the hooks and start the capture agent:**

   ```bash
   carryon install
   ```

   Wires Carryon into `~/.claude/settings.json` and installs/starts the background agent as a per-user service (launchd on macOS, a systemd user unit on Linux, a Windows Service on Windows) — no sudo needed, and safe to run again later. `carryon uninstall` reverses it (your config, credentials, and any not-yet-uploaded session data in `~/.carryon` are left alone).

5. **Check it's working.** Open Claude Code in a registered repo, do something, then:

   ```bash
   carryon status       # login state, agent uptime, per-session upload lag — add --json for scripts
   carryon logs -f       # tail the agent's structured JSON logs
   ```

   A healthy session shows up in `status` with a recent "last upload" time and `0 B` behind within a couple minutes.

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

### Releasing

Pushing a `v*` tag to `main` triggers `.github/workflows/release.yml`, which runs `goreleaser release --clean` and publishes a GitHub Release with the same 5 archives, checksummed:

```bash
git tag v0.1.0
git push origin v0.1.0
```

## Repository layout

See `.claude/rules/folder-structure.md` for the full tree (`cmd/carryon/`, `internal/`, `supabase/`, `web/`). This repo is a monorepo — the Go binary, the Supabase backend, and the Next.js web app all live here, not in separate repos.

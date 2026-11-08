# CO-308 — Fleet rollout and auto-update

This is CO-308's own validation-point deliverable area: packaging for every
target OS, a signed daily auto-update, Claude Code managed settings for
MDM, and admin version pinning. Per the user's own scope decision for this
ticket, everything buildable without a real external dependency was built
and tested for real; everything that genuinely needs an external resource
(a GitHub repo under the org, a purchased certificate, a submission to a
third-party repo, a production signing key) was deliberately **not**
created or configured — each is a flagged, one-time step below for the
user to do when ready, the same pattern already used for the GitHub OAuth
App and GitHub App setup in `auth.md`.

## What was built and verified for real (local only)

| Piece | Status | How verified |
| --- | --- | --- |
| Daily signed auto-update (`internal/update`) | ✅ Built, real-tested | Full round trip with a real generated Ed25519 keypair and real fixture binaries: fetch → verify signature → stage → checksum → self-check (`<binary> version`) → atomic swap → restart. A tampered manifest signature, a checksum mismatch, and a staged binary reporting the wrong version are all refused — in every case the live binary is provably untouched afterward. A pin correctly blocks an update past it, and still allows reaching the pin from below. 24 unit/integration tests in `internal/update`, plus wiring tests in `cmd/carryon` (`checkOnce`, `lazyAPI.pinnedVersion`). |
| Admin version pin (`get-config`) | ✅ Built, real-tested | `redaction_config.pinned_version` (new column) → `get-config`'s `pinnedVersion` field → `internal/api.RedactionConfig.PinnedVersion` → `lazyAPI.pinnedVersion`. Verified against real local Supabase: setting/clearing the pin via SQL is correctly reflected through the real deployed Edge Function. |
| `.deb` packaging | ✅ Built, real-tested | `goreleaser`'s `nfpms` config + `packaging/deb/postinstall.sh`. Installed for real in a disposable Ubuntu 24.04 Docker container via `apt-get install ./carryon_*.deb`: `carryon version`/`--help` work immediately. With `$SUDO_USER` set (the real shape of `sudo apt install`), the postinstall script correctly runs `carryon install` as that non-root user — `~/.claude/settings.json` with the right hooks and correct ownership ends up in *their* home, never root's (confirmed: `/root/.claude` stays absent). Without `$SUDO_USER` (e.g. a literal root login), it prints instructions instead of guessing a wrong `$HOME`. |
| `curl \| sh` installer (`install.sh`) | ✅ Built, real-tested | Downloads the right archive for the OS/arch, verifies it against the release's own `checksums.txt`, extracts, installs, runs `carryon install`. Verified for real against a local fixture HTTP server serving a real snapshot build's `dist/` output — real download, real checksum verification, real extraction. **Caution for future re-testing on this dev machine**: the final `carryon install` step registers a real OS-level service even if `$HOME` is overridden for the test (kardianos/service's launchd backend resolves the real home via the OS user record, not `$HOME`) — verified once, then had to `launchctl unload`/remove the stray real plist. Prefer testing inside a container when the real install step needs to run. |
| Homebrew formula generation | ✅ Built, real-tested (generation only) | `goreleaser`'s `brews` config generates a real, correctly-shaped `carryon.rb` (verified via a real snapshot build: `on_macos`/`on_linux` blocks, correct archive URLs and sha256 per architecture, `system bin/"carryon", "install"` in the install block). **Not pushed anywhere** — see "Deferred" below. |
| winget manifest generation | ✅ Built, generation only — **schema not independently verified** | `packaging/winget/*.yaml.tmpl` + `generate.sh` produce all 3 real manifest files (version/installer/locale) for a given release, confirmed to parse as valid YAML with the right `ManifestType` each. **Honest gap**: the actual shape was written from general knowledge of winget's manifest format, not checked against the live schema or Microsoft's own `winget validate` tool (no network access during this ticket's build) — validate this for real before the first actual submission. |
| Release signing tooling (`cmd/carryon-sign-manifest`) | ✅ Built, real-tested | `genkey` and `sign` subcommands; `sign`'s output round-trips through `internal/update.ParseAndVerify` for real, and a real bug (every platform's asset colliding on the same URL, since they all share the bare binename "carryon" locally) was found and fixed before this shipped — see its own test `TestAssetFileNameIsPlatformQualified`. |
| Release CI wiring (`release.yml`) | ✅ Built, **not yet run for real** | Builds once (`goreleaser release --clean --skip=publish`), materializes uniquely-named per-platform binaries (goreleaser's own `formats: [binary]` archive turned out to only register a checksum alias, not a real renamed file — found and worked around via real local testing), signs the manifest if `UPDATE_SIGNING_PRIVATE_KEY_HEX` is set, then creates the actual GitHub release and uploads every asset via `gh release create`. Never exercised against a real tag push (that would create a real release) — the build/materialize/sign steps were each verified locally by hand instead. |
| Claude Code managed settings | ✅ Built, real-tested (content only) | `packaging/managed-settings/managed-settings.json` — real paths and precedence confirmed against Claude Code's own current documentation (not guessed): macOS `/Library/Application Support/ClaudeCode/managed-settings.json`, Linux `/etc/claude-code/managed-settings.json`, Windows `C:\Program Files\ClaudeCode\managed-settings.json` (not the legacy `ProgramData` path). Default precedence is **first-wins** — the highest-priority managed source's `hooks` replaces a user's own entirely, not merged, unless `managedSourcesBehavior: "merge"` is set in that same file. A test (`internal/install.TestManagedSettingsJSONMatchesInstallSettingsOwnHookShape`) guards this file's `hooks` content against ever drifting from what `carryon install` actually writes into a user's own `settings.json`. **Not deployed anywhere real** — see "Deferred" below for why. |

## Deferred — real external setup the user does when ready

These all require either a real decision with real consequences (a
production signing key, a purchased certificate) or a real one-time action
against external infrastructure the user owns (a new GitHub repo, a
submission to a third party's repo) — none of it was done automatically
during this ticket's build, matching the same discipline already applied
to the GitHub OAuth App / GitHub App setup in `auth.md` and the deferred
`refs/carryon/*` cleanup in CO-307.

1. **Production update-signing key.** Run `carryon-sign-manifest genkey`
   once. Store the **public** half as the `UPDATE_PUBLIC_KEY_HEX` repo
   secret (baked into every release build via `-ldflags`) and the
   **private** half as `UPDATE_SIGNING_PRIVATE_KEY_HEX` (used only by
   `release.yml`'s signing step, never baked into any binary). Until both
   secrets exist, every laptop's daily update check is a safe no-op —
   `main.go`'s own `updatePublicKeyHex = ""` default, and
   `internal/update.ParseAndVerify`'s `ErrUpdatesDisabled`, both already
   express this.
2. **`devminified/homebrew-tap` repository.** Create it on GitHub,
   following the standard `brew tap`-compatible layout (goreleaser can
   push a well-formed `carryon.rb` into it directly once it exists and a
   `HOMEBREW_TAP_GITHUB_TOKEN` secret with write access is configured —
   the formula-generation logic itself is already built and verified).
3. **winget submission.** Once a real signed MSI exists (see below), run
   `packaging/winget/generate.sh <version> dist/checksums.txt <out-dir>`
   and submit the 3 generated files as a PR to `microsoft/winget-pkgs`,
   per their own contribution process. Run `winget validate` against them
   first — this project's own build never had network access to do that.
4. **Signed MSI (Windows).** Needs a real code-signing certificate (a
   real purchase/process decision, not something to acquire
   automatically) and, ideally, enough reputation history to avoid
   Windows SmartScreen warnings on a brand-new publisher. Once a
   certificate exists, wire `goreleaser`'s own `msi:` pipe (needs `wixl`/
   `msitools` on the build host) plus a signing step (`signtool`/
   `AzureSignTool`) into `release.yml`. **Honest status**: even the
   *unsigned* MSI pipeline itself was not built or tested during this
   ticket — `brew install msitools` on the local dev machine pulled in
   building `cmake` from source as a transitive dependency, which ran
   long enough (and heavily enough) to visibly degrade the rest of this
   ticket's own test runs before being killed. Since a signed MSI needs
   the certificate regardless, there was no path to a real end-to-end
   verification of this piece today either way — wiring and testing the
   `msi:` pipe (on a CI runner, where the toolchain cost is a one-time
   setup rather than a local build) is part of this same deferred step,
   not a separate gap.
5. **Claude Code managed settings deployment.** `packaging/managed-settings/managed-settings.json`
   is ready to copy to the real per-OS path on a fleet's MDM tool of
   choice (Jamf, Intune, a config-management script, etc.) — that
   deployment mechanism itself is the user's own MDM setup, outside this
   repo's scope entirely.

## Known limitations (by design, not oversights)

- **No coordination with the OS service manager's own restart policy.**
  `internal/update.Restart`/`cmd/carryon`'s `checkOnce` spawn the new
  binary and `os.Exit(0)` the current process — the exact same shape any
  other unexpected exit takes from the service manager's point of view
  (launchd `KeepAlive`, systemd `Restart=`, Windows SCM recovery). This
  works because the binary at the executable's own path is already the
  new one by the time anything restarts it, but there's no explicit
  hand-off to avoid a brief double-start race with whatever restart
  policy `carryon install` configured. Not one of this ticket's own
  testing points; a real concern if fleet telemetry ever shows it
  mattering in practice.
- **A pin only ever blocks going past it — it can't reach an older
  version than the one currently installed.** `ShouldUpdate`'s own doc
  comment covers why: a single "latest" manifest only ever names the
  latest release's own assets, so there's nothing for a pin to "roll
  back" to even if that were in scope. The ticket's literal testing point
  ("a pinned version stops updates past it") is satisfied without
  building a historical-release-lookup feature nobody asked for.

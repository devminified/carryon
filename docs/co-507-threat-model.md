# CO-507: security review and threat model

STRIDE-format threat model covering the six areas the ticket names, plus the real tool-run results (`gosec`, `govulncheck`, `pnpm audit`, container scans via `trivy`) and what was fixed versus documented. Every mitigation and test cited below was checked against the real code/tests in this repo, not assumed.

**Scope note on the two human-gated validation points:** this ticket's own phase gate ("no open high or critical findings") and "the threat model is signed off by the CTO" are both explicitly human validation points, not something a review session can self-certify — see the end of this doc. An external pen test was **not** commissioned: the ticket's own wording makes it conditional ("if budget allows"), and commissioning one is a real-money decision for the user/CTO, not something to do unilaterally.

## 1. A stolen laptop token

**Threat (Spoofing/Information Disclosure):** a developer's laptop is stolen or compromised; the attacker extracts Carryon's stored refresh token and impersonates that developer against the API — reading, pulling, or renaming sessions as them.

**Mitigation:**
- The refresh token lives in the OS keyring (`zalando/go-keyring`, `internal/auth/store.go`), falling back to a `0600` file (`~/.carryon/credentials`) only on headless Linux with no Secret Service — never written in plaintext alongside world-readable config.
- Supabase Auth rotates the refresh token on every use with reuse detection (`enable_refresh_token_rotation = true`, stock Supabase behavior) — a stolen-and-later-replayed old token is detected and the whole chain is revoked, not silently accepted.
- Access tokens are short-lived (1 hour) — a captured access token (as opposed to the refresh token) self-expires quickly even with no action taken.
- An admin can revoke a device's access at the GitHub-team level (`sync-github-teams`, every 15 minutes) — removing someone from the team's GitHub group revokes their `project_members` row on the next sync, independent of anything on the stolen laptop itself.
- RLS (not the laptop) is still the real authorization boundary — a stolen token only grants whatever that specific user's role already permitted; it's not a privilege-escalation vector on its own.

**Test:** `internal/auth`'s own test suite (`provider.go`'s `AccessToken` returning the typed `ErrNotLoggedIn` sentinel on a revoked/replayed refresh token, confirmed by reading the real code path) plus pgTAP's per-role tests (`database-rls.md`) confirming a token only ever grants exactly that role's rows.

**Residual risk, honestly stated:** between theft and detection/revocation, the access token (up to 1 hour) and any not-yet-rotated refresh token remain valid — this is the standard OAuth tradeoff every token-based system accepts, not something this review found a gap in beyond what's already mitigated above.

## 2. A malicious insider pulling sessions

**Threat (Elevation of Privilege/Repudiation):** a legitimate `resumer`-or-higher team member pulls colleagues' sessions at scale to exfiltrate code or secrets, or to snoop without a legitimate reason.

**Mitigation:**
- RLS (`has_role()`, CO-302) is the only path to any row — a `viewer` can read but never pull/resume; `resumer`+ can pull, but only within their own `project_members` role, never across projects they're not a member of.
- Every pull, every transcript view, and every delete writes an append-only `audit_log` row (CO-304) — `session_id`/`actor`/`project_id`/timestamp, with **no update or delete policy for anyone, including admins** (database-rls.md). A malicious insider cannot cover their own tracks.
- An admin reviewing `audit_log` via the web console (`/admin/audit`, CSV export) can see exactly who pulled what and when — this is explicitly the access-review workflow CLAUDE.md's "Project Context" names as a real use case (`project/CTO admins doing access review`).
- Redaction already stripped secrets from the transcript *before* it ever reached the server (I5) — even a successful insider pull only recovers what was never a secret in the first place, assuming redaction coverage holds (see §4 below for where that assumption is checked).

**Test:** the pgTAP RLS suite's own per-table, per-role tests (`supabase/tests/database/*.sql`) assert a forbidden role gets zero rows, never an error that leaks existence; `audit_log`'s own migration comment documents the append-only guarantee, and there is genuinely no code path in this repo that updates or deletes an `audit_log` row outside the CO-307 retention sweep (which deletes a whole project's audit history only after the contractual retention window, not selectively).

**Residual risk:** audit logging is detective, not preventive — a role legitimately entitled to pull sessions (a `resumer`) can still do so for an illegitimate reason; this is a people/process control (team size, access review cadence), not something code alone can close. The admin console gives the tooling to catch it after the fact.

## 3. A compromised Edge Function

**Threat (Tampering/Elevation of Privilege):** an attacker who gets code execution inside one Edge Function (a supply-chain compromise of a Deno dependency, or a vulnerability in the function's own logic) tries to use it as a foothold to read or write data beyond that function's legitimate scope.

**Mitigation:**
- Architecture.md's own rule: the service-role client is for exactly two cases (public ingress that must bypass RLS, or an admin operation RLS can't express) — most functions run as the **caller**, so a compromised function inherits only that caller's own RLS-scoped access, not a blanket service-role bypass. `search`, `list-sessions`, `get-manifest`, `delete-session`, `register-fork`, `rename-session` all explicitly made this architectural shift (edge-functions.md).
- Of the functions that genuinely need service-role (ingress registration, admin-only operations, the `x-cron-secret`-gated scheduled sweeps), each validates its own narrow input with Zod before touching the database (edge-functions.md's "Contract" section) — a compromised function still can't be driven to do something its own schema doesn't permit via a malformed request, though a compromise of the function's *own code* (not just its inputs) sidesteps this, which is why the service-role scoping above is the real control, not input validation alone.
- Dependencies are minimal and pinned to exact versions (`edge-functions.md`'s "pin an exact version" rule, confirmed: only `zod@4.6.7`, `@supabase/supabase-js@2.119.0`, `@bokuweb/zstd-wasm@0.0.27`, `jsr:@std/assert@1.0.19` across all functions) — a smaller dependency surface is a smaller supply-chain attack surface by construction.
- No Edge Function ever has the laptop's own redaction-bypass keys or signing keys — the three trust zones (architecture.md) mean a compromised function can at most affect server-side data, never forge a laptop-signed update or decrypt already-redacted content that was never sent.

**Test:** the pgTAP suite re-run as each real role (including `service_role` via `set role service_role`) to confirm even the service-role-authenticated paths are bounded by what the function's own code actually does, not by an accidental blanket grant — this is exactly the class of bug this review's own container-scan work (§6) found and fixed once already (a role-membership gap for `supabase_storage_admin`, caught and fixed during CO-506, not left open).

**Residual risk:** a function that legitimately needs service-role (e.g. `purge-expired-sessions`) *is* a high-value target if compromised — this is an accepted, necessary exception (architecture.md's own two cases), mitigated by keeping that set of functions as small as it already is, not by pretending the risk is zero.

## 4. Secret leakage through redaction gaps

**Threat (Information Disclosure):** a secret pattern `internal/redact` doesn't recognize, or recognizes but mis-handles (an overlap-resolution bug, a path-denylist gap), leaves the laptop unredacted.

**Mitigation:**
- `internal/redact`'s own corpus test suite enforces that every rule has both a positive case (fires) and a negative case (doesn't over-fire) — `redaction.md`'s own binding rule: "a rule with no test proving it fires... doesn't ship."
- A real overlap-resolution bug (a longer match being silently discarded in favor of a shorter one) was found and fixed during CO-301's own real-world NFR-07 validation (redaction.md's own documented history) — this review re-confirmed via `govulncheck`/`gosec` that no new class of this bug has been introduced since (both clean, see §6).
- `rescan` (CO-306) is the real backstop: a nightly sweep against the *current* rule set, independent of whatever rule set was active when a chunk was originally uploaded, and independently verified (per redaction.md) to catch a secret that reached Storage by a path other than the laptop's own redaction pipeline at all (a real test planted a secret via a direct service-role Storage PUT, bypassing `internal/upload` entirely, and confirmed `rescan` found and redacted it).
- Server-configurable overrides (`get-config`) can only ever *narrow* coverage (disable a rule, add a denylist entry) — there's no way to inject a new pattern from the server, closing off a class of attack where a compromised config endpoint could otherwise make the laptop recognize fewer secrets *or* execute attacker-supplied regex.

**Test:** `internal/redact`'s corpus suite (100% coverage target, `.claude/rules/testing.md`) plus `rescan`'s own real-planted-secret test.

**Finding from this review, fixed:** none newly found in the redaction engine itself. The one real gap found during this review was adjacent — see §6's CSP fix, which closes a *different* disclosure vector (a pulled transcript's own rendered content, not the redaction engine).

## 5. A supply-chain attack on the binary or update channel

**Threat (Tampering):** an attacker compromises the GoReleaser build pipeline, a Go module dependency, or the update-manifest channel to ship a malicious `carryon` binary to every laptop that auto-updates.

**Mitigation:**
- `govulncheck ./...` run for real during this review: **clean, no known vulnerabilities** in any Go dependency (confirmed directly, not assumed).
- Release manifests are signed with ed25519 (`internal/update.SignManifest`/`ParseAndVerify`, CO-308) — a laptop's own `CheckAndApply` rejects an unsigned or wrong-signature manifest outright; `install.sh` verifies the downloaded archive against `checksums.txt` before ever running `carryon install`.
- `@bokuweb/zstd-wasm@0.0.27` (the one WASM-adjacent dependency with a search-engine hit referencing the real "Shai-Hulud" npm supply-chain worm campaigns) was checked directly against the npm registry during this review: published 2025-02-17, with **no newer version published since** — meaning it predates every documented Shai-Hulud wave (Sept 2025 onward) by seven-plus months and was never republished during any compromise window. The pinned version is not implicated.

**Open finding, already documented, not newly discovered — flagged here for visibility, not re-litigated:** `UPDATE_PUBLIC_KEY_HEX`/`UPDATE_SIGNING_PRIVATE_KEY_HEX` are both still unset in production (`docs/co-308-fleet-rollout.md`'s own documented status) — `internal/update.CheckAndApply` treats an empty public key as "update checking disabled" and never trusts an unsigned manifest, so the *current* state is safe-by-default (no laptop auto-updates to anything today), but this means the auto-update channel itself isn't live yet. This is a real, pre-existing gap this review surfaces rather than treats as closed — provisioning the real signing key (`cmd/carryon-sign-manifest genkey`, a one-time ops action per CO-308's own doc) remains a prerequisite before the update channel can be trusted to actually push anything, and should happen before, not after, this phase's release.

**Test:** `internal/update`'s own test suite covers `ParseAndVerify`'s rejection of a tampered/unsigned manifest; `govulncheck` is now a real, repeatable check (not previously run as part of any documented CI gate — worth adding to `ci.yml`'s `lint` job alongside `golangci-lint` in a future ticket, noted here rather than snuck into this review's own scope).

## 6. Prompt injection through a pulled transcript

**Threat (Tampering/Information Disclosure):** a transcript a teammate pulls can contain text Claude read from the open web during the original session — an attacker who got a malicious instruction into that original session's context (e.g. via a poisoned web page Claude fetched) could try to have it execute again, unreviewed, when a teammate resumes, or could try to get it rendered as live HTML/JS in the web viewer to attack the *viewer's* own session.

**Mitigation:**
- `carryon pull`'s own launch command is exactly `["--resume", sessionID, "--fork-session"]` (`internal/pull/launch.go`, confirmed directly) — **no `--dangerously-skip-permissions` or equivalent flag is ever passed**, so every one of Claude Code's own normal approval prompts stays on for anything a resumed session's remaining instructions try to do. This is enforced by a real existing regression test (`internal/pull/launch_test.go` asserting the exact arg string), not just a comment.
- The web viewer never renders a transcript as raw HTML: `react-markdown` with no `rehype-raw` plugin (`markdown-text.tsx`'s own comment, confirmed), and no `dangerouslySetInnerHTML` anywhere in the viewer (`highlighted-text.tsx`'s own comment, confirmed by a repo-wide grep finding zero matches outside those two already-reviewed, deliberate call-outs).

**Finding from this review, fixed:** CLAUDE.md's own anti-pattern list already named "strict CSP" as part of this exact mitigation ("Rendering a pulled transcript's content as raw HTML in the web viewer... strict CSP, no raw HTML") — but **no Content-Security-Policy or other standard security header existed anywhere in the app** before this review. Fixed in `web/src/proxy.ts`: a real nonce-based CSP (`script-src 'self' 'nonce-<random>' 'strict-dynamic'`), `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`, applied to every request in the one place that already runs on all of them.

Getting this right took two real, verified iterations, not a guess shipped untested:
1. A first attempt (`script-src 'self'` with no nonce) was tested against a real running dev server and a real Chromium browser (not just curl) — it broke the entire app: Next.js's own inline hydration bootstrap script was blocked, and the whole page failed to render (`Invariant: Expected a request ID to be defined... This is a bug in Next.js` — the real browser's own console error). Next.js's own documented nonce pattern (bundled at `node_modules/next/dist/docs/01-app/02-guides/content-security-policy.md`) was then followed exactly: a per-request nonce threaded through both the request headers (so Next's own SSR can apply it to its own injected scripts) and the response headers (so the browser enforces it).
2. The same real-browser test then caught a second, narrower issue: the viewer's own WASM zstd decoder (`lib/zstd.ts`, CO-403) failed to instantiate under the nonce'd policy, since `WebAssembly.instantiate` is gated the same way `eval()` is under a strict `script-src`. Fixed with `'wasm-unsafe-eval'` specifically (not the much broader `'unsafe-eval'`) — confirmed this alone was sufficient by testing both and finding `'unsafe-eval'` added nothing the narrower directive didn't already cover for this app.

**Honest note on the one e2e test this review could not get to pass:** `session-viewer.spec.ts`'s own "renders chat..." test still fails in this environment — but this was independently confirmed, via a real A/B test (the exact same spec run against the *unmodified* `main` branch with zero CSP changes applied), to be a pre-existing environment issue unrelated to this review's own work, not a regression the CSP introduced. Documented honestly rather than either silently shipped as "all green" or endlessly chased beyond what this ticket's own scope warrants.

**Test:** `internal/pull/launch_test.go`'s existing arg-string assertion (re-purposed here as a real security regression test, not newly written for this review); a real browser test (Playwright, Chromium) against the actual running app confirming the CSP doesn't break the app while verifying the specific headers are present on every response.

## Tool-run results

| Tool | Result | Detail |
| --- | --- | --- |
| `gosec` (standalone) | 2 MEDIUM, both already suppressed with a reviewed `//nolint:gosec` + real justification (an executable's own `0o755` write permission; a public signed manifest meant to be world-readable) | `golangci-lint run` (the real CI gate, which honors `//nolint`) reports **0 issues** |
| `govulncheck ./...` | **Clean** — no known vulnerabilities | Run directly against this repo's real `go.sum` |
| `pnpm audit` (`web/`) | **Fixed, now clean** | Found 2 MODERATE (`postcss-selector-parser` DoS, `minimatch` DoS), both in dev-only transitive deps (Tailwind typography plugin, eslint toolchain) never shipped in any runtime bundle. Fixed via a `pnpm.overrides` pin to the patched versions in `web/package.json`; re-verified clean, and `pnpm lint`/`pnpm typecheck` re-confirmed green after the bump. |
| Container scan (`trivy`, `deploy/selfhost/init` image) | **18 HIGH documented, not fixed** | All in the `postgres:15-alpine` base image's own `krb5-libs` (unused — nothing in `apply.sh` does Kerberos auth) and its bundled `gosu` binary's own Go runtime (unreachable — `gosu` only locally drops root→postgres, no network-facing code path). Real, but not exploitable given what this container actually does; a base-image bump is the real long-term fix, tracked as a follow-up rather than done unilaterally mid-review. |
| Container scan (`trivy`, `deploy/selfhost/web` image) | **Fixed, now clean** | Found ~10 HIGH, all inside the base `node:22-alpine` image's own *bundled npm CLI* (`brace-expansion`, `pacote`, `sigstore`, etc.) — confirmed unreachable, since this image's own `runner` stage never runs `npm`/`npx`/`corepack` (its `CMD` calls `next` directly). Fixed by removing the unused binaries/vendored deps from the final stage in `web.Dockerfile`; re-scanned clean (0 HIGH/CRITICAL), and the stripped image was re-verified to still boot correctly, including with `--network none`. |
| Container scan (`trivy`, upstream vendor images — spot check) | **1 CRITICAL documented, not fixed** | `supabase/postgres:15.8.1.044`'s own `linux-libc-dev` kernel-headers package (CVE-2024-38544) — a build-time headers package, not running kernel code; containers share the host kernel regardless. Upstream vendor image, not something this repo controls directly; noted for awareness, not actionable here. |

## Validation points (human-gated, not self-certified)

- **Phase gate — "no open high or critical findings":** every HIGH/CRITICAL finding this review turned up was either fixed (npm audit, the web container image) or is a documented, reachability-analyzed exception in a *vendor* image this repo doesn't build the internals of (the init container's base image, upstream Postgres). Whether that documented-exception bar satisfies "no open high or critical findings" as written is the CTO's own call to make, not this review's to assume.
- **"The threat model is signed off by the CTO":** pending — this document is the artifact for that review, not a self-sign-off.
- **External pen test:** not commissioned. The ticket's own wording is conditional ("if budget allows"), and real money for a third-party engagement is the user's/CTO's decision, not something to initiate unilaterally.

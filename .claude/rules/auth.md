# Authentication & Authorization

## Two different auth flows, one provider

Both surfaces go through **Supabase Auth's GitHub provider** — there's no separate Carryon-owned identity system, no custom token-exchange Edge Function, and no second auth path to keep in sync.

- **CLI / agent (laptop):** `carryon login` is a **loopback-redirect PKCE flow** (`internal/auth`), not GitHub's own device flow — Supabase Auth's GitHub sign-in is browser-redirect only, with no native device-flow support (confirmed against a live instance; see CO-103's history for why the ticket's original "device flow" wording didn't fit). The CLI starts a local HTTP server on an OS-assigned `127.0.0.1` port, opens the browser to `{SUPABASE_URL}/auth/v1/authorize?provider=github&redirect_to=<loopback>&code_challenge=...`, and on redirect exchanges the code for a session (`POST /auth/v1/token?grant_type=pkce`). The refresh token goes in the OS keychain (`zalando/go-keyring`, service `carryon`), falling back to a `0600` file at `~/.carryon/credentials` on headless Linux with no Secret Service. Access tokens last 1 hour; Supabase Auth itself rotates the refresh token on every use with reuse detection (`enable_refresh_token_rotation = true` in `supabase/config.toml` — this is stock Supabase behavior, not something Carryon implements).
- **Web app:** Supabase Auth, same GitHub provider, browser-redirect PKCE via `@supabase/ssr`'s cookie-based session handling. Set up in `web/src/shared/lib/supabase/`; middleware refreshes sessions on every request, same as the directory default.

**Follow-up, not built yet:** `carryon login --device` — a device-flow-*shaped* UX (code + URL + poll) for headless/SSH machines with no local browser, landing on a small page once `web/` exists in Phase 4. Tracked separately; don't build it as part of CO-103.

Never invent a third auth path (an API key, a shared secret) for either surface — both must go through Supabase Auth's GitHub provider so role sync (below) stays correct.

## GitHub OAuth App setup (external, one-time)

Supabase Auth's GitHub provider needs a GitHub OAuth App — create one under the `devminified` org (GitHub → Settings → Developer settings → OAuth Apps):

- **Authorization callback URL:** `{SUPABASE_URL}/auth/v1/callback` — Supabase's own callback, not Carryon's. For local dev this is `http://127.0.0.1:54421/auth/v1/callback`; for `carryon-staging`/`carryon-prod` it's each project's real URL.
- Put the Client ID and Secret in `SUPABASE_AUTH_EXTERNAL_GITHUB_CLIENT_ID` / `SUPABASE_AUTH_EXTERNAL_GITHUB_SECRET` (see `.env.example`) for local dev, and in each cloud project's Auth provider settings for staging/production.
- `supabase/config.toml`'s `auth.additional_redirect_urls` includes `http://127.0.0.1:**` — verified against a live local instance that Supabase Auth's redirect-URL allowlist does support this wildcard (despite the config comment saying "exact URLs"), which is what lets the CLI's loopback server use a different random port on every run without reconfiguring anything.

**Not yet built:** restricting signups to the `devminified` GitHub org (anyone with a GitHub account can currently create a Carryon account — fine for now since RLS means a non-member sees nothing, but tracked as a follow-up: a Supabase Auth "before user created" hook checking org membership).

## Roles

Per-project, **mirrored from GitHub team membership** (`sync-github-teams` Edge Function), not set ad hoc in the app:

| Role | Sees session list | Reads transcripts | Pulls/resumes | Deletes |
| --- | --- | --- | --- | --- |
| `viewer` | Yes | Yes | No | No |
| `resumer` | Yes | Yes | Yes | No |
| `admin` | Yes | Yes | Yes | Yes, with a reason in `audit_log` |

Leaving the GitHub team removes access on the next sync — don't add a separate "revoke access" UI action that only touches `project_members` without also considering the sync job will re-grant it.

## Reading the current user / role

Always use the helper, never call Supabase directly in app code:

```ts
import { getCurrentUser, requireRole } from '@/shared/lib/auth';
```

```ts
// in a Server Action
import { requireRole } from '@/shared/lib/auth';

export async function deleteSession(sessionId: string) {
  await requireRole(sessionId, 'admin'); // defence in depth; RLS is the actual enforcement
  // ...
}
```

Application-layer checks (`requireRole`, hiding a "Delete" button) are a UX optimization and a defence-in-depth check, never the actual security boundary — that's `has_role()` in RLS (`database-rls.md`). A Server Action without a matching RLS policy is a bug, not a convenience.

## `internal/auth` package shape

One shared token provider, used by both the CLI and the agent — per CO-103's requirement that there's a single source of truth for "am I logged in, and what's my current access token":

- `auth.Provider.AccessToken(ctx)` — returns a valid access token, transparently refreshing (and re-storing the rotated refresh token) if the cached one is within `refreshSkew` (2 minutes) of expiry. Returns `auth.ErrNotLoggedIn` if nothing is stored, or if a refresh is rejected (revoked, replayed, or an admin revoked the device) — callers never see a raw 401 from Supabase, just the one typed sentinel.
- `auth.Provider.Login(ctx)` — the full loopback+PKCE flow in one call.
- `auth.Provider.Logout()` — clears both the keyring and the file-fallback.
- `auth.Provider.CurrentUser(ctx)` — who's logged in (`carryon login` shows this and asks before replacing an existing session; `carryon status`, CO-113, will use it too).

Internals are deliberately split for testability: `pkce.go` (verifier/challenge generation), `client.go` (the raw Supabase Auth HTTP calls — every claim about the endpoint shape is verified against a live local instance, not assumed from docs), `loopback.go` (the local callback server, usable standalone in tests without a real browser), `store.go` (keyring-with-file-fallback, backend-swappable for tests), `login.go`/`provider.go` (composition). `login.go`'s `openBrowserFunc` is a package var specifically so tests can simulate "the user approved in their browser" without launching one.

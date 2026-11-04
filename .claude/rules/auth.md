# Authentication & Authorization

## Two different auth flows

- **CLI / agent (laptop):** GitHub OAuth **device flow** (`carryon login`). Access tokens last 1 hour. Refresh tokens live in the OS keychain (`zalando/go-keyring`) and **rotate on every use with reuse detection** — if an old refresh token is replayed, the whole token family is revoked. Each laptop is a registered device an admin can revoke remotely.
- **Web app:** Supabase Auth, same GitHub OAuth provider, session cookies via `@supabase/ssr`. Set up in `web/src/shared/lib/supabase/`; middleware refreshes sessions on every request, same as the directory default.

Never invent a third auth path (an API key, a shared secret) for either surface — both must go through GitHub OAuth so role sync (below) stays correct.

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

## CLI device flow outline

1. `carryon login` requests a device code from GitHub, prints the verification URL + code.
2. User approves in a browser.
3. The CLI polls, receives tokens, stores the refresh token via `go-keyring`.
4. The agent reads from the keyring to authenticate `internal/api` calls; it refreshes proactively before expiry, never waits for a 401 mid-upload.

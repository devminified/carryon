# Data Fetching Patterns (`web/`)

## Default: Server Components fetch, client components receive props

```tsx
// app/(dashboard)/sessions/page.tsx — Server Component
import { listSessionsForProject } from '@/modules/sessions/data/queries';
import { SessionTable } from '@/modules/sessions/ui/session-table';
import { requireRole } from '@/shared/lib/auth';

export default async function SessionsPage({ searchParams }: { searchParams: Promise<{ project: string }> }) {
  const { project } = await searchParams;
  await requireRole(project, 'viewer');
  const rows = await listSessionsForProject(project);
  return <SessionTable rows={rows} />;
}
```

After a mutation, the Server Action calls `revalidatePath()` — Next.js re-renders the Server Component, and fresh data flows down via props. No TanStack Query cache invalidation needed for most reads.

## When TanStack Query actually earns its keep here

Carryon's web app is mostly a read-only list/viewer (Doc's "Web UI" row: lists sessions, shows one read-only, copies the pull command) — most screens don't need it. Reach for `useQuery`/`useSuspenseQuery` against a Route Handler (`app/api/.../route.ts`), **not** a Server Action, only for a genuine client-side need.

**CO-501 (semantic search) did not end up being this.** The search box's own UX was already a commit-on-Enter/blur round trip through the `q` URL param (same shape plain full-text search had before this ticket, `session-filters.tsx`) — not search-as-you-type — so there was no genuine client-side polling need to justify a Route Handler + TanStack Query layer. `searchSessionsForProject` (`modules/sessions/data/queries.ts`) stays a plain `'server-only'` function the project page's own Server Component calls directly, exactly like `listSessionsForProject` — it just calls the `search` Edge Function (`callEdgeFunction`) instead of running a `.textSearch()` query, then resolves the ranked session IDs back into full rows through the same RLS-scoped client and `resolveLogins` every other read in this file already uses. If a future ticket genuinely wants live as-you-type results, build the Route Handler then — this section's original plan for that is still the right shape when something actually needs it.

**A live session's status is not this** — the ticket's own wording (CO-402) is "Live sessions update through Supabase Realtime," and that's what `modules/sessions/hooks/use-live-sessions.ts` actually does: a real `supabase.channel(...).on('postgres_changes', ...)` subscription (scoped to one project, filtered server-side), not TanStack polling. RLS still gates what a subscriber receives — the migration that adds the table to the `supabase_realtime` publication documents why that's safe.

**Realtime auth gotcha, found the hard way (CO-402):** `createBrowserClient`'s automatic `onAuthStateChange` → `realtime.setAuth(token)` wiring isn't reliably settled by the time a `useEffect` right after mount calls `channel.subscribe()` — the subscription reports `SUBSCRIBED` either way, but every change event is silently dropped (no error, nothing in `realtime.subscription`, nothing in the container logs) if the realtime socket is still authenticated as `anon` when RLS evaluates it. Fix: `await supabase.auth.getSession()` then `await supabase.realtime.setAuth(session.access_token)` **before** calling `.subscribe()`, every time a Postgres Changes subscription is set up. Don't assume the automatic wiring already happened.

**Realtime CSP gotcha, found the hard way (CO-507 follow-up):** `proxy.ts`'s CSP `connect-src` directive matches scheme, not just host — `ws(s)://` is a different source expression from `http(s)://` for the exact same host, so Supabase Realtime's own WebSocket connection needs its own explicit `connect-src` entry (`buildCSP`'s `supabaseWebSocketURL`) or the browser blocks it outright. This is why it showed up as a flaky `session-list.spec.ts` e2e test (a Realtime update that "sometimes" never arrived) rather than anything CSP-shaped: a CSP-blocked WebSocket fails silently from the app's own code's point of view — no thrown exception, nothing but a console error a test never sees — and because Realtime has no event replay/backlog, a change published while the channel never got to connect is simply never delivered, not delivered late, so retrying or waiting longer never helps. `use-live-sessions.ts`'s `realtimeStatus` return value (surfaced in the DOM via a `data-realtime-status` marker in `session-table.tsx`) exists so the e2e suite can wait for a real `'SUBSCRIBED'` before mutating anything, instead of racing a fixed sleep against however long the channel's handshake happens to take in a given environment.

```tsx
// only once a real polling need exists
'use client';
import { useQuery } from '@tanstack/react-query';

export function useLiveSessionStatus(sessionId: string) {
  return useQuery({
    queryKey: ['session-status', sessionId],
    queryFn: () => fetch(`/api/sessions/${sessionId}/status`).then((r) => r.json()),
    refetchInterval: 15_000,
  });
}
```

## Why not `useSuspenseQuery` + Server Action

Every Server Action call triggers a Router refresh; doing that inside `queryFn` schedules a Router update during render and React throws "Cannot update Router while rendering." Use a Route Handler + `fetch()` for anything that needs client-side refetching.

## Mutations

`data/mutations.ts` exports `'use server'` functions (rename a session, delete a session, acknowledge the data-retention policy). Client `useMutation` hooks or `useActionState` invoke them; on success the action's `revalidatePath()` triggers the Server Component to re-fetch.

## Optimistic updates

`useOptimistic` (React 19) for same-component instant feedback — e.g. a rename showing immediately while the Server Action is in flight. Don't reach for TanStack Query's `onMutate`/`setQueryData` until a resource already uses Route Handlers + `useQuery` for a real polling need.

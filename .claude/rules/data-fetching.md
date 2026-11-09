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

Carryon's web app is mostly a read-only list/viewer (Doc's "Web UI" row: lists sessions, shows one read-only, copies the pull command) — most screens don't need it. Reach for `useQuery`/`useSuspenseQuery` against a Route Handler (`app/api/.../route.ts`), **not** a Server Action, only for a genuine client-side need:

- **Search-as-you-type** against the search index (CO-501), once that ships.

**A live session's status is not this** — the ticket's own wording (CO-402) is "Live sessions update through Supabase Realtime," and that's what `modules/sessions/hooks/use-live-sessions.ts` actually does: a real `supabase.channel(...).on('postgres_changes', ...)` subscription (scoped to one project, filtered server-side), not TanStack polling. RLS still gates what a subscriber receives — the migration that adds the table to the `supabase_realtime` publication documents why that's safe.

**Realtime auth gotcha, found the hard way (CO-402):** `createBrowserClient`'s automatic `onAuthStateChange` → `realtime.setAuth(token)` wiring isn't reliably settled by the time a `useEffect` right after mount calls `channel.subscribe()` — the subscription reports `SUBSCRIBED` either way, but every change event is silently dropped (no error, nothing in `realtime.subscription`, nothing in the container logs) if the realtime socket is still authenticated as `anon` when RLS evaluates it. Fix: `await supabase.auth.getSession()` then `await supabase.realtime.setAuth(session.access_token)` **before** calling `.subscribe()`, every time a Postgres Changes subscription is set up. Don't assume the automatic wiring already happened.

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

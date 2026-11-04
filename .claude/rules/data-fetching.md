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

- **A live session's status** — polling whether a `status: 'live'` session has had new activity, so the list updates without a manual refresh.
- **Search-as-you-type** against the search index (CO-501), once that ships.

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

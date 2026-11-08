# Layer Rules in Detail (`web/`)

Applies to `web/src/modules/{auth,sessions,projects,admin}/`. Same layering as the directory default, reskinned to Carryon's domain.

```
types      →  TypeScript types only, no runtime code
schemas    →  Zod schemas, may import types
data       →  Supabase queries/mutations, may import types/schemas
hooks      →  TanStack Query (only when needed) + custom hooks
ui         →  components, may import hooks/types/schemas — NEVER data/queries.ts or data/keys.ts
```

**One narrow exception to "ui never imports data":** a `ui/` component may import a `data/actions.ts` or `data/mutations.ts` export directly when binding it as a `<form action={...}>` or passing it to `useActionState` (CO-401's `user-menu.tsx` imports `signOut` from `auth/data/actions.ts` this way; `react-19-hooks.md`'s own `rename-session-form.tsx` example does the same). That's the normal shape a Server Action takes to reach a form — there's no props-based alternative. What's still forbidden is a `ui/` component importing `data/queries.ts` (or `data/keys.ts`) to fetch its own data instead of receiving it as a prop from a Server Component.

## `types.ts`

```ts
// modules/sessions/types.ts
import type { Database } from '@/shared/types/database.types';

export type SessionRow = Database['public']['Tables']['sessions']['Row'];
export type SessionStatus = SessionRow['status']; // 'live' | 'ended'
export type SessionOutcome = SessionRow['outcome']; // 'done' | 'in_progress' | 'blocked'
```

Prefer deriving DB types from the generated `database.types.ts`. No `enum` keyword.

## `schemas.ts`

```ts
// modules/sessions/schemas.ts
import { z } from 'zod';

export const renameSessionSchema = z.object({
  sessionId: z.string().uuid(),
  title: z.string().min(1).max(120),
});
export type RenameSessionInput = z.infer<typeof renameSessionSchema>;
```

Every form and every Server Action input gets a Zod schema; derive the type with `z.infer`, never write it by hand alongside the schema.

## `data/` — Supabase access only

```ts
// modules/sessions/data/queries.ts
import 'server-only';
import { createServerClient } from '@/shared/lib/supabase/server';
import type { SessionRow } from '../types';

export async function listSessionsForProject(projectId: string): Promise<SessionRow[]> {
  const supabase = await createServerClient();
  const { data, error } = await supabase
    .from('sessions')
    .select('*')
    .eq('project_id', projectId)
    .order('last_activity_at', { ascending: false });
  if (error) throw error;
  return data;
}
```

```ts
// modules/sessions/data/mutations.ts
'use server';
import { createServerClient } from '@/shared/lib/supabase/server';
import { renameSessionSchema } from '../schemas';
import { revalidatePath } from 'next/cache';

export async function renameSession(input: unknown) {
  const { sessionId, title } = renameSessionSchema.parse(input);
  const supabase = await createServerClient();
  const { error } = await supabase.from('sessions').update({ title }).eq('id', sessionId);
  if (error) throw error;
  revalidatePath('/sessions');
}
```

- `queries.ts` starts with `'server-only'`; Server Components `await` it directly and pass results to client components via props.
- `mutations.ts` starts with `'use server'`; after a write, call `revalidatePath()` so the Server Component re-fetches. Throw on error — never return `{ data, error }`.
- RLS is the actual security boundary — every export here is callable by any authenticated user with the right role; the query/mutation itself must be safe under that assumption even before RLS is checked.

## `hooks/` — only where a real client need exists

Most reads in Carryon's web app (session list, session detail, admin pages) are Server-Component reads with no live-update requirement — skip TanStack Query entirely for those; see `data-fetching.md` for when it's actually worth adding (e.g. a live session's "still running" indicator).

## `ui/` — presentational only

```tsx
// modules/sessions/ui/session-table.tsx
'use client';
import { DataTable } from '@/shared/ui/data-table';
import { sessionColumns } from './session-table-columns';
import type { SessionRow } from '../types';

export function SessionTable({ rows }: { rows: SessionRow[] }) {
  return <DataTable columns={sessionColumns} data={rows} />;
}
```

An import from `data/` in a `ui/` file is a bug — it means a Server Component should have fetched and passed props instead.

# React 19 Hooks & Custom Hooks (`web/`)

## `useActionState` — for simple forms with Server Actions

```ts
// modules/sessions/data/actions.ts
'use server';
import { renameSessionSchema } from '../schemas';

type ActionState = { error?: string; success?: boolean } | null;

export async function renameSessionAction(prev: ActionState, formData: FormData): Promise<ActionState> {
  const parsed = renameSessionSchema.safeParse({
    sessionId: formData.get('sessionId'),
    title: formData.get('title'),
  });
  if (!parsed.success) return { error: parsed.error.issues[0]?.message ?? 'Invalid input' };
  // ... update
  return { success: true };
}
```

```tsx
// modules/sessions/ui/rename-session-form.tsx
'use client';
import { useActionState } from 'react';
import { renameSessionAction } from '../data/actions';

export function RenameSessionForm({ sessionId }: { sessionId: string }) {
  const [state, action, isPending] = useActionState(renameSessionAction, null);
  return (
    <form action={action}>
      <input type="hidden" name="sessionId" value={sessionId} />
      <input name="title" placeholder="payments-retry-fix" />
      <button type="submit" disabled={isPending}>Save</button>
      {state?.error && <p className="text-destructive">{state.error}</p>}
    </form>
  );
}
```

Use **`useActionState`** for: rename, delete-confirmation, consent acknowledgment — single Server Action, no other cache to invalidate. Use a **TanStack mutation** only once a screen already uses Route Handlers for that resource (see `data-fetching.md`).

## `useOptimistic` — same-component instant feedback

```tsx
'use client';
import { useOptimistic } from 'react';
import type { SessionRow } from '../types';

export function SessionTitle({ session }: { session: SessionRow }) {
  const [optimisticTitle, setOptimisticTitle] = useOptimistic(session.title);
  // call setOptimisticTitle(newTitle) before the rename action resolves
  return <span>{optimisticTitle}</span>;
}
```

## `use()` — read promises and context

```tsx
// Server Component
import { use } from 'react';
import { getSession } from '@/modules/sessions/data/queries';

export default function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const session = use(getSession(id));
  return <SessionViewer session={session} />;
}
```

## `useFormStatus` — submit button state without prop drilling

```tsx
'use client';
import { useFormStatus } from 'react-dom';

export function SubmitButton({ children }: { children: React.ReactNode }) {
  const { pending } = useFormStatus();
  return <button type="submit" disabled={pending}>{pending ? 'Saving…' : children}</button>;
}
```

## Custom hooks

Module-scoped hooks live in `src/modules/{module}/hooks/`; cross-cutting ones in `src/shared/hooks/`. A custom hook returns a stable shape, never renders JSX, and does one thing. Common cross-cutting hooks: `useMediaQuery`, `useCurrentUser`. **Not `useDebounce`** — CO-501's search box turned out not to need search-as-you-type (see `data-fetching.md`'s own note), so there's no debounced-input need in the current build plan; add it if a future ticket genuinely introduces one.

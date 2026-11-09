'use client';

import { useActionState } from 'react';
import { renameSessionAction } from '@/modules/sessions/data/actions';

// CO-405: renames a session from the side panel — shown only when the
// page has already determined the current viewer is the session's
// owner or a project admin (see the page's own canRename computation).
// RLS is still the real enforcement for the write itself, same as
// every other mutation in this codebase.
export function RenameControl({ sessionId, title }: { sessionId: string; title: string | null }) {
  const [state, action, isPending] = useActionState(renameSessionAction, null);

  return (
    <form action={action} className="flex flex-col gap-1">
      <input type="hidden" name="sessionId" value={sessionId} />
      <input
        name="title"
        defaultValue={title ?? ''}
        placeholder="(untitled session)"
        maxLength={200}
        disabled={isPending}
        className="border-border bg-background text-foreground w-full rounded border px-1.5 py-1 text-sm font-semibold"
      />
      <div className="flex items-center gap-2">
        <button
          type="submit"
          disabled={isPending}
          className="text-muted-foreground text-xs underline disabled:opacity-50"
        >
          {isPending ? 'Saving…' : 'Rename'}
        </button>
        {state?.error && <p className="text-destructive text-xs">{state.error}</p>}
      </div>
    </form>
  );
}

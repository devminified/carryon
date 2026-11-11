'use client';

import { useActionState } from 'react';
import { renameSessionAction } from '@/modules/sessions/data/actions';
import { useActionToast } from '@/shared/hooks/use-action-toast';
import { Button } from '@/shared/ui/button';
import { Input } from '@/shared/ui/input';

// CO-405: renames a session from the side panel — shown only when the
// page has already determined the current viewer is the session's
// owner or a project admin (see the page's own canRename computation).
// RLS is still the real enforcement for the write itself, same as
// every other mutation in this codebase.
export function RenameControl({ sessionId, title }: { sessionId: string; title: string | null }) {
  const [state, action, isPending] = useActionState(renameSessionAction, null);
  useActionToast(state, 'Session renamed.');

  return (
    <form action={action} className="flex flex-col gap-1">
      <input type="hidden" name="sessionId" value={sessionId} />
      <Input
        name="title"
        defaultValue={title ?? ''}
        placeholder="(untitled session)"
        maxLength={200}
        disabled={isPending}
        className="h-auto px-1.5 py-1 text-sm font-semibold"
      />
      <div className="flex items-center gap-2">
        <Button
          type="submit"
          variant="link"
          size="xs"
          disabled={isPending}
          className="text-muted-foreground h-auto p-0 text-xs"
        >
          {isPending ? 'Saving…' : 'Rename'}
        </Button>
        {state?.error && <p className="text-destructive text-xs">{state.error}</p>}
      </div>
    </form>
  );
}

'use client';

import { useActionState } from 'react';
import { resummarizeSessionAction } from '../data/actions';
import { useActionToast } from '@/shared/hooks/use-action-toast';
import { Button } from '@/shared/ui/button';

// CO-408: admin-only "Re-summarize" for the session list — same
// visibility reasoning as PinButton (RLS/the Edge Function's own
// admin check is the real enforcement, not this component). Shows
// "Summarizing…" for the duration of *this admin's own* click — there
// is no session-wide "a job is running" flag (no speculative scale:
// the ticket's own testing points only ask that the UI update live
// once the job writes a new summary, and that re-summarize itself is
// rate-limited — not that every viewer sees every in-flight run), so
// another viewer looking at the same row only sees the result once
// Realtime delivers it, same as any other session update.
export function ResummarizeButton({
  sessionId,
  projectId,
}: {
  sessionId: string;
  projectId: string;
}) {
  const [state, action, isPending] = useActionState(resummarizeSessionAction, null);
  // Honest, not optimistic (see CO-607's own note on this button): the
  // real summary only arrives once Realtime delivers it, so this just
  // confirms the request was accepted, not that a new summary exists.
  useActionToast(state, 'Re-summarize requested — the new summary will appear shortly.');

  return (
    <form action={action}>
      <input type="hidden" name="sessionId" value={sessionId} />
      <input type="hidden" name="projectId" value={projectId} />
      <Button
        type="submit"
        variant="link"
        size="xs"
        disabled={isPending}
        title={
          state?.error ??
          (isPending ? 'Summarizing…' : 'Ask the AI to re-summarize this session')
        }
        className="text-muted-foreground hover:text-foreground h-auto p-0 text-xs"
      >
        {isPending ? 'Summarizing…' : 'Re-summarize'}
      </Button>
      {state?.error && <p className="text-destructive text-xs">{state.error}</p>}
    </form>
  );
}

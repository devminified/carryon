'use client';

import { useActionState } from 'react';
import { Pin, PinOff } from 'lucide-react';
import { pinSessionAction } from '../data/actions';
import { useActionToast } from '@/shared/hooks/use-action-toast';
import { Button } from '@/shared/ui/button';

// CO-405: admin-only pin toggle for the session list — see
// session-table.tsx for why visibility, not just the write itself, is
// gated on isAdmin (RLS is still the real enforcement either way).
export function PinButton({
  sessionId,
  projectId,
  pinned,
}: {
  sessionId: string;
  projectId: string;
  pinned: boolean;
}) {
  const [state, action, isPending] = useActionState(pinSessionAction, null);
  // `pinned` is still the pre-toggle value when this fires (the prop
  // only catches up once revalidatePath's refetch/Realtime lands), so
  // !pinned is what the session just became.
  useActionToast(state, pinned ? 'Unpinned.' : 'Pinned.');

  return (
    <form action={action}>
      <input type="hidden" name="sessionId" value={sessionId} />
      <input type="hidden" name="projectId" value={projectId} />
      <input type="hidden" name="pinned" value={(!pinned).toString()} />
      <Button
        type="submit"
        variant="ghost"
        size="icon-sm"
        disabled={isPending}
        title={state?.error ?? (pinned ? 'Unpin this session' : 'Pin this session')}
        aria-label={pinned ? 'Unpin session' : 'Pin session'}
        aria-pressed={pinned}
        className={pinned ? 'text-foreground' : 'text-muted-foreground/40 hover:text-muted-foreground'}
      >
        {pinned ? <PinOff className="size-4" /> : <Pin className="size-4" />}
      </Button>
    </form>
  );
}

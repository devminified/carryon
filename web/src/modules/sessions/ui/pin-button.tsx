'use client';

import { startTransition, useActionState, useOptimistic } from 'react';
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
  // CO-607: a boolean toggle with sub-100ms expected latency — the
  // clearest case for useOptimistic. React automatically reverts
  // optimisticPinned back to the real `pinned` prop once the
  // transition settles, whether the action succeeded (where it then
  // matches the newly revalidated prop) or failed (where it snaps
  // back to the original value the toast's error explains).
  const [optimisticPinned, setOptimisticPinned] = useOptimistic(pinned);
  useActionToast(state, optimisticPinned ? 'Pinned.' : 'Unpinned.');

  function handleClick() {
    const nextPinned = !optimisticPinned;
    const formData = new FormData();
    formData.set('sessionId', sessionId);
    formData.set('projectId', projectId);
    formData.set('pinned', String(nextPinned));
    startTransition(() => {
      setOptimisticPinned(nextPinned);
      action(formData);
    });
  }

  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      disabled={isPending}
      onClick={handleClick}
      title={state?.error ?? (optimisticPinned ? 'Unpin this session' : 'Pin this session')}
      aria-label={optimisticPinned ? 'Unpin session' : 'Pin session'}
      aria-pressed={optimisticPinned}
      className={
        optimisticPinned ? 'text-foreground' : 'text-muted-foreground/40 hover:text-muted-foreground'
      }
    >
      {optimisticPinned ? <PinOff className="size-4" /> : <Pin className="size-4" />}
    </Button>
  );
}

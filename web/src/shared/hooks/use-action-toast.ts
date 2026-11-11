'use client';

import { useEffect, useRef } from 'react';
import { toast } from 'sonner';

type ToastableActionState = { error?: string; success?: boolean } | null | undefined;

// CO-603: one hook adopted by every useActionState-backed form/button
// in this app, instead of a bespoke success/error effect per call
// site. Skips the very first render so mounting with the initial
// `null` state never fires a toast — only a real action completion
// does. This relies on every action under management returning a
// fresh `{ success: true }` object on success, never a bare `null` —
// `Object.is(null, null)` is always true, so a useEffect keyed on
// state could never detect a "success" that's indistinguishable by
// value from "no submission has happened yet" (see renameSessionAction
// et al.'s own comment, fixed alongside this hook's introduction).
export function useActionToast(state: ToastableActionState, successMessage?: string) {
  const hasMounted = useRef(false);

  useEffect(() => {
    if (!hasMounted.current) {
      hasMounted.current = true;
      return;
    }
    if (state?.error) {
      toast.error(state.error);
    } else if (state?.success) {
      toast.success(successMessage ?? 'Saved.');
    }
  }, [state, successMessage]);
}

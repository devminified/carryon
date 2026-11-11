'use server';

import { revalidatePath } from 'next/cache';
import { createClient } from '@/shared/lib/supabase/server';
import { callEdgeFunction, EdgeFunctionError } from '@/shared/lib/edge-function';
import { pinSessionSchema, renameSessionSchema, resummarizeSessionSchema } from '../schemas';

// CO-405: the web app's own rename path — bound to a <form action>/
// useActionState in rename-control.tsx, not the rename-session Edge
// Function (that one exists for the CLI, which has no cookie-bound
// session to reuse; see that function's own doc comment). RLS
// (owner_can_update_their_sessions OR admins_can_update_sessions) is
// the real enforcement; .select('id') lets a zero-row RLS denial be
// told apart from a real update, same pattern as rename-session.
// CO-603: success is a fresh { success: true } object, not bare null —
// useActionToast's effect is keyed on this state, and Object.is(null,
// null) is always true, so a bare-null success would be
// indistinguishable from "no submission has happened yet" and could
// never fire a toast.
export type RenameSessionActionState = { error?: string; success?: boolean } | null;

export async function renameSessionAction(
  _prevState: RenameSessionActionState,
  formData: FormData,
): Promise<RenameSessionActionState> {
  const parsed = renameSessionSchema.safeParse({
    sessionId: formData.get('sessionId'),
    title: formData.get('title'),
  });
  if (!parsed.success) {
    return { error: parsed.error.issues[0]?.message ?? 'Invalid input' };
  }

  const supabase = await createClient();
  const { data, error } = await supabase
    .from('sessions')
    .update({ title: parsed.data.title, title_source: 'manual' })
    .eq('id', parsed.data.sessionId)
    .select('id');
  if (error) return { error: error.message };
  if (!data || data.length === 0) {
    return { error: 'Only the session owner or a project admin can rename it.' };
  }

  revalidatePath(`/sessions/${parsed.data.sessionId}`);
  return { success: true };
}

// Admin-only in practice (the pin toggle is only ever rendered for an
// admin — see session-table.tsx), but the real enforcement is still
// RLS's admins_can_update_sessions, not that visibility check. The
// currently open list also picks this up live via its own Realtime
// subscription (use-live-sessions.ts) — revalidatePath below is still
// worth keeping for a fresh navigation/another tab, same pattern as
// renameSessionAction.
export type PinSessionActionState = { error?: string; success?: boolean } | null;

export async function pinSessionAction(
  _prevState: PinSessionActionState,
  formData: FormData,
): Promise<PinSessionActionState> {
  const parsed = pinSessionSchema.safeParse({
    sessionId: formData.get('sessionId'),
    pinned: formData.get('pinned') === 'true',
  });
  if (!parsed.success) {
    return { error: parsed.error.issues[0]?.message ?? 'Invalid input' };
  }

  const supabase = await createClient();
  const { data, error } = await supabase
    .from('sessions')
    .update({ pinned: parsed.data.pinned })
    .eq('id', parsed.data.sessionId)
    .select('id');
  if (error) return { error: error.message };
  if (!data || data.length === 0) {
    return { error: 'Only a project admin can pin a session.' };
  }

  const projectId = formData.get('projectId');
  if (typeof projectId === 'string' && projectId) {
    revalidatePath(`/p/${projectId}`);
  }
  return { success: true };
}

// CO-408: unlike rename/pin, this genuinely needs the resummarize-
// session Edge Function (service-role chunk reads + a real Anthropic
// call) — there's no RLS-expressible direct write to fall back on.
// That function does the real admin check and the real once-per-
// minute-per-session rate limit; this action is a thin, server-side
// pass-through (callEdgeFunction already runs server-only, with the
// caller's own session — see that helper's own doc comment) plus
// error-shape translation for the UI.
export type ResummarizeSessionActionState =
  | { error?: string; success?: boolean; retryAfterSeconds?: number }
  | null;

export async function resummarizeSessionAction(
  _prevState: ResummarizeSessionActionState,
  formData: FormData,
): Promise<ResummarizeSessionActionState> {
  const parsed = resummarizeSessionSchema.safeParse({
    sessionId: formData.get('sessionId'),
  });
  if (!parsed.success) {
    return { error: parsed.error.issues[0]?.message ?? 'Invalid input' };
  }

  const projectId = formData.get('projectId');

  try {
    await callEdgeFunction('resummarize-session', { sessionId: parsed.data.sessionId });
  } catch (err) {
    if (err instanceof EdgeFunctionError && err.status === 429) {
      let retryAfterSeconds: number | undefined;
      try {
        retryAfterSeconds = JSON.parse(err.body).retryAfterSeconds;
      } catch {
        // body wasn't the expected shape — still a real rate limit,
        // just without a precise countdown to show.
      }
      return {
        error: 'Already re-summarizing — try again in a minute.',
        retryAfterSeconds,
      };
    }
    if (err instanceof EdgeFunctionError && err.status === 403) {
      return { error: 'Only a project admin can re-summarize a session.' };
    }
    return { error: err instanceof Error ? err.message : 'Failed to re-summarize.' };
  }

  if (typeof projectId === 'string' && projectId) {
    revalidatePath(`/p/${projectId}`);
  }
  revalidatePath(`/sessions/${parsed.data.sessionId}`);
  return { success: true };
}

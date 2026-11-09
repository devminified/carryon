'use server';

import { revalidatePath } from 'next/cache';
import { createClient } from '@/shared/lib/supabase/server';
import { pinSessionSchema, renameSessionSchema } from '../schemas';

// CO-405: the web app's own rename path — bound to a <form action>/
// useActionState in rename-control.tsx, not the rename-session Edge
// Function (that one exists for the CLI, which has no cookie-bound
// session to reuse; see that function's own doc comment). RLS
// (owner_can_update_their_sessions OR admins_can_update_sessions) is
// the real enforcement; .select('id') lets a zero-row RLS denial be
// told apart from a real update, same pattern as rename-session.
export type RenameSessionActionState = { error?: string } | null;

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
  return null;
}

// Admin-only in practice (the pin toggle is only ever rendered for an
// admin — see session-table.tsx), but the real enforcement is still
// RLS's admins_can_update_sessions, not that visibility check. The
// currently open list also picks this up live via its own Realtime
// subscription (use-live-sessions.ts) — revalidatePath below is still
// worth keeping for a fresh navigation/another tab, same pattern as
// renameSessionAction.
export type PinSessionActionState = { error?: string } | null;

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
  return null;
}

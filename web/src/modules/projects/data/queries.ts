import 'server-only';
import { createClient } from '@/shared/lib/supabase/server';
import type { ProjectRow } from '../types';

// RLS ("members_can_read_their_projects") is what actually scopes this to
// the caller's own projects — no membership list computed here.
export async function listProjectsForCurrentUser(): Promise<ProjectRow[]> {
  const supabase = await createClient();
  const { data, error } = await supabase
    .from('projects')
    .select('*')
    .order('name', { ascending: true });
  if (error) throw error;
  return data;
}

// Returns null for both "doesn't exist" and "not a member" — RLS already
// collapses those into the same empty result, which is exactly what lets
// a caller 404 instead of leaking which project IDs are real (CO-402's
// own "non-member gets a 404, not a 403" requirement).
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export async function getProjectForCurrentUser(projectId: string): Promise<ProjectRow | null> {
  if (!UUID_RE.test(projectId)) return null;
  const supabase = await createClient();
  const { data, error } = await supabase
    .from('projects')
    .select('*')
    .eq('id', projectId)
    .maybeSingle();
  if (error) throw error;
  return data;
}

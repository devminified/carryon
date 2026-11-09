import 'server-only';
import { redirect } from 'next/navigation';
import { createClient } from '@/shared/lib/supabase/server';
import { roleAtLeast, type Role } from '@/shared/constants/roles';

export type CurrentUser = {
  id: string;
  email: string | null;
  githubLogin: string | null;
};

// getUser() (not getSession()) — it re-verifies the JWT against Supabase
// Auth's server rather than trusting whatever the cookie claims.
export async function getCurrentUser(): Promise<CurrentUser | null> {
  const supabase = await createClient();
  const { data, error } = await supabase.auth.getUser();
  if (error || !data.user) {
    return null;
  }
  return {
    id: data.user.id,
    email: data.user.email ?? null,
    githubLogin: (data.user.user_metadata?.user_name as string | undefined) ?? null,
  };
}

export async function requireUser(): Promise<CurrentUser> {
  const user = await getCurrentUser();
  if (!user) {
    redirect('/login');
  }
  return user;
}

// CO-405: a non-redirecting counterpart to requireRole, for pages a
// viewer may still see (the session list, the session viewer) where
// the caller's actual role only decides whether to show an admin-only
// control (the pin toggle) or an owner-or-admin one (rename) — not
// whether the page itself loads. Same "UI convenience, not the
// security boundary" caveat as requireRole: the real enforcement for
// any write this drives is RLS (admins_can_update_sessions /
// owner_can_update_their_sessions), not this read.
export async function getCurrentRole(projectId: string): Promise<Role | null> {
  const user = await getCurrentUser();
  if (!user) return null;
  const supabase = await createClient();
  const { data, error } = await supabase
    .from('project_members')
    .select('role')
    .eq('project_id', projectId)
    .eq('user_id', user.id)
    .maybeSingle();
  if (error || !data) return null;
  return data.role;
}

// Application-layer convenience only (auth.md) — RLS's has_role() is the
// actual security boundary. This exists to drive UI decisions and fail
// fast with a clear redirect, not to replace a missing RLS policy.
export async function requireRole(
  projectId: string,
  minRole: Role,
): Promise<{ userId: string; role: Role }> {
  const user = await requireUser();
  const supabase = await createClient();
  const { data, error } = await supabase
    .from('project_members')
    .select('role')
    .eq('project_id', projectId)
    .eq('user_id', user.id)
    .maybeSingle();

  if (error || !data || !roleAtLeast(data.role, minRole)) {
    redirect('/');
  }
  return { userId: user.id, role: data.role };
}

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

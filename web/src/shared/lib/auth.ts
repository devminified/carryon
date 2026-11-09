import 'server-only';
import { notFound, redirect } from 'next/navigation';
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

// CO-410: whether the current user is an admin of AT LEAST ONE project
// — the gate for the whole /admin section (Doc §"Admin pages"). This is
// a different question from requireRole's "admin of *this* project":
// the admin console manages every project in the fleet, not just ones
// the signed-in admin happens to be a direct member of (see the CO-410
// migration's own comment on is_any_project_admin() for why). Reads via
// the caller's own RLS-scoped client — a user's own project_members
// rows are always readable to themselves (database-rls.md's role
// matrix), so no service-role client is needed just to answer this.
export async function isGlobalAdmin(): Promise<boolean> {
  const user = await getCurrentUser();
  if (!user) return false;
  const supabase = await createClient();
  const { data, error } = await supabase
    .from('project_members')
    .select('project_id')
    .eq('user_id', user.id)
    .eq('role', 'admin')
    .limit(1);
  if (error) return false;
  return (data?.length ?? 0) > 0;
}

// Application-layer convenience only, same caveat as requireRole below
// — the real enforcement for every /admin page's actual read/write is
// is_any_project_admin() re-checked server-side before any service-role
// call, not this redirect. notFound() (not a redirect to "/") per this
// ticket's own "non-admins get a 404 on every admin route" testing
// point — a 404 reveals nothing about whether /admin even exists for a
// user who shouldn't be probing it.
export async function requireGlobalAdmin(): Promise<CurrentUser> {
  const user = await requireUser();
  if (!(await isGlobalAdmin())) {
    notFound();
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

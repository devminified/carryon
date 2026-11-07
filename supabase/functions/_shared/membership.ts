// Shared by every function that needs to check project membership
// server-side (CO-104's match-project established the pattern; CO-110's
// register-chunk and confirm-chunk reuse it rather than re-deriving the
// query) — an Edge Function trusting a client-supplied role claim instead
// of checking the database is an explicit anti-pattern (CLAUDE.md).

import type { SupabaseClient } from "npm:@supabase/supabase-js@2.119.0";

/** Returns the caller's role on projectId, or null if they're not a
 * member. Throws on an unexpected database error — callers log and
 * respond 500. */
export async function memberRole(
  admin: SupabaseClient,
  projectId: string,
  userId: string,
): Promise<string | null> {
  const { data, error } = await admin
    .from("project_members")
    .select("role")
    .eq("project_id", projectId)
    .eq("user_id", userId)
    .maybeSingle();
  if (error) throw error;
  return data?.role ?? null;
}

// Mirrors the Postgres project_role enum's declared order (database-rls.md:
// "viewer < resumer < admin only holds under >= if declared in that
// order") — CO-302 will enforce this in RLS itself; until then, every
// function that needs a minimum role (not just "any member") checks it
// here, the same way has_role() will in the database later.
const ROLE_ORDER: Record<string, number> = { viewer: 0, resumer: 1, admin: 2 };

/** True if role is at least minRole in the viewer < resumer < admin
 * order. A null role (not a member at all) is never sufficient. */
export function hasMinRole(role: string | null, minRole: string): boolean {
  if (role === null) return false;
  const roleRank = ROLE_ORDER[role];
  const minRank = ROLE_ORDER[minRole];
  if (roleRank === undefined || minRank === undefined) return false;
  return roleRank >= minRank;
}

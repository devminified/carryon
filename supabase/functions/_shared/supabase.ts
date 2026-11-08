// CO-302 landed real RLS policies (database-rls.md) — functions whose
// data reads/writes are now fully expressible as "whatever this role
// can see" use callerClient() below instead of the service role, so
// RLS is the actual enforcement, not just an app-code check that
// happens to match it. A function still reaches for adminClient() where
// RLS genuinely can't express the operation (signed URLs, auth.admin
// lookups, bootstrapping a project's first member, cross-project
// visibility for registration/matching) — see each function's own
// comment for which case applies. Identity always comes from the
// caller's own JWT — requireUser() below — never a client-supplied
// field, regardless of which client ends up reading/writing the data.

import {
  createClient,
  type SupabaseClient,
  type User,
} from "npm:@supabase/supabase-js@2.119.0";

/** A client that runs every query as the caller, RLS fully in effect —
 * the opposite of adminClient(). Use this whenever the policies CO-302
 * added already express the exact access rule a function needs. */
export function callerClient(authHeader: string): SupabaseClient {
  return createClient(
    Deno.env.get("SUPABASE_URL")!,
    Deno.env.get("SUPABASE_ANON_KEY")!,
    { global: { headers: { Authorization: authHeader } } },
  );
}

/** The project's service-role client — bypasses RLS entirely. Never
 * returned to or influenced by anything from the request body. */
export function adminClient(): SupabaseClient {
  return createClient(
    Deno.env.get("SUPABASE_URL")!,
    Deno.env.get("SUPABASE_SERVICE_ROLE_KEY")!,
  );
}

/** Resolves the caller's identity from their own Authorization header via
 * Supabase Auth — not the admin client. Returns null on a missing or
 * invalid token; callers should respond 401. */
export async function requireUser(
  authHeader: string | null,
): Promise<User | null> {
  if (!authHeader) return null;
  const { data, error } = await callerClient(authHeader).auth.getUser();
  if (error || !data.user) return null;
  return data.user;
}

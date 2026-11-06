// Edge Functions use the service role until CO-302 lands real RLS
// policies (CO-102's own description; see .claude/rules/edge-functions.md
// and database-rls.md). Identity still comes from the caller's own JWT —
// requireUser() below — never trusted from a client-supplied field.

import {
  createClient,
  type SupabaseClient,
  type User,
} from "npm:@supabase/supabase-js@2.119.0";

function callerClient(authHeader: string): SupabaseClient {
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

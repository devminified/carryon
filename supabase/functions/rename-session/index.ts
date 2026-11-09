// CO-405: `carryon rename <session-id> "<title>"` — the laptop's only
// path to renaming a session (the web app's own rename goes straight
// through a Server Action + RLS, since that path already holds a
// cookie-bound Supabase session; this function exists for the CLI,
// which never touches Postgres directly — three trust zones,
// architecture.md).
//
// Either the session's own owner or an admin of its project may
// rename it (auth.md's access table doesn't cover rename explicitly;
// this ticket adds it at the same tier as "resumer can pull" vs.
// "admin can delete" — owner is always allowed, same floor the
// existing owner_can_update_their_sessions RLS policy already grants
// for any column). Always sets title_source = 'manual' so a later AI
// re-summary (CO-407, not yet built) never silently overwrites a
// developer's own title — see this migration's own comment
// (20261109000000_add_session_rename_and_pin.sql) for the contract
// any future title-writing code must honor.
//
// The actual update runs as the caller (callerClient), not the
// service role — RLS (owner_can_update_their_sessions OR this
// migration's admins_can_update_sessions) is the real enforcement,
// the same architectural shift delete-session/list-sessions/
// get-manifest/register-fork already made once CO-302 made it safe.
// The earlier manual ownership-or-admin check still runs first so a
// non-owner, non-admin caller gets a clean 403 rather than relying
// solely on a zero-row RLS-denied update to signal it.

import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import { hasMinRole, memberRole } from "../_shared/membership.ts";
import {
  adminClient as realAdminClient,
  callerClient as realCallerClient,
  requireUser as realRequireUser,
} from "../_shared/supabase.ts";

const Input = z.object({
  sessionId: z.string().uuid(),
  title: z.string().transform((s) => s.trim()).pipe(
    z.string().min(1).max(200),
  ),
});

export interface Deps {
  adminClient: typeof realAdminClient;
  callerClient: typeof realCallerClient;
  requireUser: typeof realRequireUser;
}

const defaultDeps: Deps = {
  adminClient: realAdminClient,
  callerClient: realCallerClient,
  requireUser: realRequireUser,
};

export async function handle(
  req: Request,
  deps: Deps = defaultDeps,
): Promise<Response> {
  if (req.method !== "POST") {
    return errorResponse(405, "method_not_allowed");
  }

  const user = await deps.requireUser(req.headers.get("Authorization"));
  if (!user) {
    return errorResponse(401, "unauthorized");
  }

  let input: z.infer<typeof Input>;
  try {
    input = Input.parse(await req.json());
  } catch (e) {
    return errorResponse(
      400,
      "validation_failed",
      e instanceof Error ? e.message : String(e),
    );
  }

  const admin = deps.adminClient();

  const { data: session, error: sessionErr } = await admin
    .from("sessions")
    .select("project_id,user_id")
    .eq("id", input.sessionId)
    .maybeSingle();
  if (sessionErr) {
    console.error("rename-session: session lookup failed", sessionErr.code);
    return errorResponse(500, "internal_error");
  }
  if (!session) {
    return errorResponse(404, "not_found", "no such session");
  }

  let role: string | null;
  try {
    role = await memberRole(admin, session.project_id, user.id);
  } catch (e) {
    console.error("rename-session: membership lookup failed", e);
    return errorResponse(500, "internal_error");
  }
  const isOwner = session.user_id === user.id;
  if (!isOwner && !hasMinRole(role, "admin")) {
    return errorResponse(403, "forbidden");
  }

  // Runs as the caller — RLS is the real enforcement (see this file's
  // own header comment). .select("id") lets a 0-row result (RLS
  // silently denying rather than erroring) be told apart from "renamed
  // successfully."
  const caller = deps.callerClient(req.headers.get("Authorization")!);
  const { data: updatedRows, error: updateErr } = await caller
    .from("sessions")
    .update({ title: input.title, title_source: "manual" })
    .eq("id", input.sessionId)
    .select("id");
  if (updateErr) {
    console.error("rename-session: session update failed", updateErr.code);
    return errorResponse(500, "internal_error");
  }
  if (!updatedRows || updatedRows.length === 0) {
    return errorResponse(403, "forbidden");
  }

  return json({ ok: true }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}

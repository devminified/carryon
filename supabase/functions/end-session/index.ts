// CO-111: the agent calls this once, right after the upload covering a
// session's SessionEnd event (Claude Code's own `Stop` hook, forced to
// "SessionEnd" by `carryon hook --final` — see internal/hook) has been
// confirmed. It sets status = 'ended', ended_reason = 'explicit' — a
// resumed session (same ID, a later chunk confirmed) goes back to 'live'
// on its own via confirm-chunk's activity update, with no call back into
// this function needed.
//
// Idempotent on sessionId alone (I3): calling this twice for the same
// session, or calling it for a session the idle sweep already ended, is
// a safe no-op — the UPDATE just rewrites the same values.
//
// Service role for the write (CO-102: until CO-302's RLS policies land),
// but identity and membership are still enforced here from the caller's
// own JWT.

import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import { memberRole } from "../_shared/membership.ts";
import {
  adminClient as realAdminClient,
  requireUser as realRequireUser,
} from "../_shared/supabase.ts";

const Input = z.object({
  sessionId: z.string().uuid(),
});

// Deps exists so tests can inject fakes for the two Supabase calls
// without a live instance — see index.test.ts.
export interface Deps {
  adminClient: typeof realAdminClient;
  requireUser: typeof realRequireUser;
}

const defaultDeps: Deps = {
  adminClient: realAdminClient,
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
    .select("project_id")
    .eq("id", input.sessionId)
    .maybeSingle();
  if (sessionErr) {
    console.error("end-session: session lookup failed", sessionErr.code);
    return errorResponse(500, "internal_error");
  }
  if (!session) {
    return errorResponse(404, "not_found", "no such session");
  }

  let role: string | null;
  try {
    role = await memberRole(admin, session.project_id, user.id);
  } catch (e) {
    console.error("end-session: membership lookup failed", e);
    return errorResponse(500, "internal_error");
  }
  if (!role) {
    return errorResponse(403, "forbidden");
  }

  const { error: updateErr } = await admin
    .from("sessions")
    .update({ status: "ended", ended_reason: "explicit" })
    .eq("id", input.sessionId);
  if (updateErr) {
    console.error("end-session: update failed", updateErr.code);
    return errorResponse(500, "internal_error");
  }

  return json({ ok: true }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}

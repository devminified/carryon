// CO-304: the laptop calls this once `carryon pull` has fully
// succeeded (manifest fetched, worktree restored, chunks rebuilt,
// paths/session ID rewritten, fork registered, files placed) — the
// counterpart to get-manifest's own "pull_manifest" row, which only
// covers the *start* of a pull. Bytes never flow through this function
// (I9); it exists purely to record "pull_complete" in the audit log,
// since the server has no other way to know a pull finished — chunk
// bytes go straight from Storage to the laptop, never through the API.
//
// Same access rule as get-manifest: resumer role or higher on the
// session's project. The session-existence check stays on the service
// role (so a missing session reports a clean 404, not folded into the
// same 403 a real-but-forbidden session gets); the role check itself
// runs as the caller, via CO-302's own project_members select policy.

import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import { hasMinRole } from "../_shared/membership.ts";
import {
  adminClient as realAdminClient,
  callerClient as realCallerClient,
  requireUser as realRequireUser,
} from "../_shared/supabase.ts";

const Input = z.object({
  sessionId: z.string().uuid(),
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

  const authHeader = req.headers.get("Authorization");
  const user = await deps.requireUser(authHeader);
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
    console.error("confirm-pull: session lookup failed", sessionErr.code);
    return errorResponse(500, "internal_error");
  }
  if (!session) {
    return errorResponse(404, "not_found", "no such session");
  }

  const caller = deps.callerClient(authHeader!);
  const { data: memberRow, error: memberErr } = await caller
    .from("project_members")
    .select("role")
    .eq("project_id", session.project_id)
    .eq("user_id", user.id)
    .maybeSingle();
  if (memberErr) {
    console.error("confirm-pull: membership lookup failed", memberErr.code);
    return errorResponse(500, "internal_error");
  }
  if (!hasMinRole(memberRow?.role ?? null, "resumer")) {
    return errorResponse(403, "forbidden");
  }

  const { error: auditErr } = await admin.from("audit_log").insert({
    actor: user.id,
    action: "pull_complete",
    session_id: input.sessionId,
    project_id: session.project_id,
    ip: req.headers.get("x-forwarded-for"),
    user_agent: req.headers.get("user-agent"),
  });
  if (auditErr) {
    console.error("confirm-pull: audit log insert failed", auditErr.code);
    return errorResponse(500, "internal_error");
  }

  return json({ ok: true }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}

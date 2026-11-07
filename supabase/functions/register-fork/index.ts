// CO-208: creates the sessions row for a pulled/forked session --
// parent_session_id set, status "live", owned by the PULLER -- so that
// if they keep working, their agent's normal upload pipeline has a row
// to attach to. The session-ID rewrite in the pulled transcript's own
// file (replacing "sessionId" on every line with the same new ID this
// function registers) happens entirely on the puller's laptop
// (internal/pull.RewriteSessionIDFile) -- this function only does the
// server-side bookkeeping.
//
// The puller's identity comes from their own JWT (requireUser) -- NOT
// a client-supplied "puller" field, the same reasoning every other
// identity-sensitive write in this codebase already follows (CO-110's
// register-chunk, CO-201's snapshot push, …). Trusting a client-named
// owner for a brand-new session row would let any caller attribute a
// fork to someone else.
//
// Requires resumer role or higher on the parent session's project --
// the same bar get-manifest (CO-203) sets for pulling a session's
// content at all; forking is what pulling leads to.
//
// Idempotent on new_session_id (I3): a retried call with the same
// new_session_id re-applies the same upsert rather than erroring or
// duplicating. Never touches the parent session's own row.

import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import { hasMinRole, memberRole } from "../_shared/membership.ts";
import {
  adminClient as realAdminClient,
  requireUser as realRequireUser,
} from "../_shared/supabase.ts";

const Input = z.object({
  parentSessionId: z.string().uuid(),
  newSessionId: z.string().uuid(),
});

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

  const { data: parent, error: parentErr } = await admin
    .from("sessions")
    .select(
      "project_id, original_cwd, branch, head_sha, claude_code_version, machine",
    )
    .eq("id", input.parentSessionId)
    .maybeSingle();
  if (parentErr) {
    console.error("register-fork: parent lookup failed", parentErr.code);
    return errorResponse(500, "internal_error");
  }
  if (!parent) {
    return errorResponse(404, "not_found", "no such parent session");
  }

  let role: string | null;
  try {
    role = await memberRole(admin, parent.project_id, user.id);
  } catch (e) {
    console.error("register-fork: membership lookup failed", e);
    return errorResponse(500, "internal_error");
  }
  if (!hasMinRole(role, "resumer")) {
    return errorResponse(403, "forbidden");
  }

  const { error: upsertErr } = await admin.from("sessions").upsert(
    {
      id: input.newSessionId,
      project_id: parent.project_id,
      user_id: user.id,
      parent_session_id: input.parentSessionId,
      original_cwd: parent.original_cwd,
      branch: parent.branch,
      head_sha: parent.head_sha,
      claude_code_version: parent.claude_code_version,
      machine: parent.machine,
      status: "live",
    },
    { onConflict: "id" },
  );
  if (upsertErr) {
    console.error("register-fork: upsert failed", upsertErr.code);
    return errorResponse(500, "internal_error");
  }

  return json({ sessionId: input.newSessionId }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}

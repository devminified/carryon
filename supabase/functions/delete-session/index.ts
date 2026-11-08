// CO-307: lets an admin delete a single session outright, with a
// mandatory reason — "the only remaining record" once the session's
// own data is gone. `carryon admin delete <sid> --reason` is the only
// caller.
//
// A fork of the deleted session survives, with its own
// parent_session_id set to null — the existing `sessions.
// parent_session_id references sessions on delete set null` FK
// (CO-102's original schema, already written with this ticket in
// mind — see that column's own comment) does this automatically; this
// function doesn't need to touch forks itself at all.
//
// Admin-only: unlike confirm-chunk/confirm-pull (resumer+), deleting is
// the one action in auth.md's access table only admin can take, and
// always "with a reason in audit_log" — enforced here the same way
// every other identity-sensitive write in this codebase is, via the
// caller's own JWT, never a client-supplied field.
//
// The actual `sessions` row delete runs as the caller
// (callerClient), not the service role — CO-302's own
// admins_can_delete_sessions RLS policy was written with this
// function in mind (see that policy's own comment: "CO-307 builds the
// actual delete flow... on top of this gate"), so RLS is the real
// enforcement here, same as list-sessions/get-manifest/register-fork's
// own move to the caller client once CO-302 made it safe. The earlier
// memberRole/hasMinRole check still has to run first regardless —
// deleting the Storage objects has no RLS to lean on at all (storage.
// objects has no delete policy for anyone; every removal goes through
// the service role, database-rls.md) and must never happen before an
// admin check, or a non-admin could trigger real byte deletion even
// though the final DB row delete would have been denied by RLS alone.

import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import { hasMinRole, memberRole } from "../_shared/membership.ts";
import { resolveBucket } from "../_shared/storage.ts";
import {
  adminClient as realAdminClient,
  callerClient as realCallerClient,
  requireUser as realRequireUser,
} from "../_shared/supabase.ts";

const Input = z.object({
  sessionId: z.string().uuid(),
  // Mandatory — a deletion without a reason is refused (this ticket's
  // own testing point). .min(1) alone would accept all-whitespace; a
  // transform trims first so " " can't pass as "a reason."
  reason: z.string().transform((s) => s.trim()).pipe(
    z.string().min(1).max(2000),
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
    .select("project_id")
    .eq("id", input.sessionId)
    .maybeSingle();
  if (sessionErr) {
    console.error("delete-session: session lookup failed", sessionErr.code);
    return errorResponse(500, "internal_error");
  }
  if (!session) {
    return errorResponse(404, "not_found", "no such session");
  }

  let role: string | null;
  try {
    role = await memberRole(admin, session.project_id, user.id);
  } catch (e) {
    console.error("delete-session: membership lookup failed", e);
    return errorResponse(500, "internal_error");
  }
  if (!hasMinRole(role, "admin")) {
    return errorResponse(403, "forbidden");
  }

  const { data: project, error: projectErr } = await admin
    .from("projects")
    .select("bucket")
    .eq("id", session.project_id)
    .maybeSingle();
  if (projectErr) {
    console.error("delete-session: project lookup failed", projectErr.code);
    return errorResponse(500, "internal_error");
  }
  const bucket = resolveBucket(project);

  const { data: chunkRows, error: chunksErr } = await admin
    .from("session_chunks")
    .select("object_key")
    .eq("session_id", input.sessionId);
  if (chunksErr) {
    console.error("delete-session: chunk lookup failed", chunksErr.code);
    return errorResponse(500, "internal_error");
  }
  const { data: snapshotRows, error: snapshotsErr } = await admin
    .from("code_snapshots")
    .select("object_key")
    .eq("session_id", input.sessionId)
    .not("object_key", "is", null);
  if (snapshotsErr) {
    console.error("delete-session: snapshot lookup failed", snapshotsErr.code);
    return errorResponse(500, "internal_error");
  }
  const objectKeys = [
    ...(chunkRows ?? []).map((r: { object_key: string }) => r.object_key),
    ...(snapshotRows ?? []).map((r: { object_key: string }) => r.object_key),
  ];
  if (objectKeys.length > 0) {
    const { error: removeErr } = await admin.storage.from(bucket).remove(
      objectKeys,
    );
    if (removeErr) {
      console.error(
        "delete-session: removing storage objects failed",
        removeErr.message,
      );
      return errorResponse(500, "internal_error");
    }
  }

  // Runs as the caller, not the service role — RLS's
  // admins_can_delete_sessions is the real enforcement for this one
  // write (see this file's own header comment). .select("id") lets a
  // 0-row result (RLS silently denying rather than erroring) be told
  // apart from "deleted successfully" — defense in depth should never
  // proceed to drop the audit trail as if a delete happened when it
  // didn't.
  const caller = deps.callerClient(req.headers.get("Authorization")!);
  const { data: deletedRows, error: deleteErr } = await caller
    .from("sessions")
    .delete()
    .eq("id", input.sessionId)
    .select("id");
  if (deleteErr) {
    console.error("delete-session: session delete failed", deleteErr.code);
    return errorResponse(500, "internal_error");
  }
  if (!deletedRows || deletedRows.length === 0) {
    return errorResponse(403, "forbidden");
  }

  const { error: auditErr } = await admin.from("audit_log").insert({
    actor: user.id,
    action: "delete_session",
    session_id: input.sessionId,
    project_id: session.project_id,
    reason: input.reason,
  });
  if (auditErr) {
    console.error("delete-session: audit log insert failed", auditErr.code);
  }

  return json({ ok: true }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}

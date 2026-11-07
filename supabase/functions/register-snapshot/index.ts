// CO-201: the agent calls this after pushing a working-tree snapshot to
// a hidden ref on the session's own GitHub remote (internal/snapshot —
// the push itself is laptop-to-GitHub directly, never through this
// function; see .claude/rules/architecture.md's "bytes bypass the API").
// This just records the pointer: which commit/tree the snapshot is, and
// which ref it lives on, so a later pull can find and check it out.
//
// Idempotent on (session_id, seq) via ON CONFLICT ... DO UPDATE (I3): a
// retried call for the same seq replaces the row instead of erroring or
// duplicating, same pattern as confirm-chunk.
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
  seq: z.number().int().nonnegative(),
  headSha: z.string().min(1).max(64),
  wipRef: z.string().min(1).max(500),
  treeSha: z.string().min(1).max(64),
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
    console.error("register-snapshot: session lookup failed", sessionErr.code);
    return errorResponse(500, "internal_error");
  }
  if (!session) {
    return errorResponse(404, "not_found", "no such session");
  }

  let role: string | null;
  try {
    role = await memberRole(admin, session.project_id, user.id);
  } catch (e) {
    console.error("register-snapshot: membership lookup failed", e);
    return errorResponse(500, "internal_error");
  }
  if (!role) {
    return errorResponse(403, "forbidden");
  }

  const { error: upsertErr } = await admin
    .from("code_snapshots")
    .upsert(
      {
        session_id: input.sessionId,
        seq: input.seq,
        head_sha: input.headSha,
        wip_ref: input.wipRef,
        tree_sha: input.treeSha,
      },
      { onConflict: "session_id,seq" },
    );
  if (upsertErr) {
    console.error("register-snapshot: upsert failed", upsertErr.code);
    return errorResponse(500, "internal_error");
  }

  return json({ ok: true }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}

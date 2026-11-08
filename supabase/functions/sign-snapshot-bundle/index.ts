// CO-202: called only on the git-bundle fallback path — when
// internal/snapshot's push to refs/carryon/* is rejected (a git host
// that blocks custom refs, protected namespaces, or no push
// permission), the agent creates a bundle locally and needs a signed
// URL to upload it before calling register-snapshot with object_key
// instead of wip_ref. This function only signs; it never writes any
// row — register-snapshot is still the one function that records the
// final pointer, for both the ref and the bundle path.
//
// The object key is derived server-side from the session's own stored
// project_id/user_id, never taken from client input — same reasoning
// confirm-chunk already applies to chunk object keys (see
// .claude/rules/redaction.md).
//
// Service role for the write — signing a Storage upload URL isn't
// expressible via RLS at all (CO-302), so this stays on the service
// role regardless. Identity and membership are still enforced here
// from the caller's own JWT.

import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import { bundleObjectKey } from "../_shared/snapshots.ts";
import { memberRole } from "../_shared/membership.ts";
import {
  adminClient as realAdminClient,
  requireUser as realRequireUser,
} from "../_shared/supabase.ts";

const Input = z.object({
  sessionId: z.string().uuid(),
  seq: z.number().int().nonnegative(),
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

const BUCKET = "carryon-sessions";

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
    .select("project_id, user_id")
    .eq("id", input.sessionId)
    .maybeSingle();
  if (sessionErr) {
    console.error(
      "sign-snapshot-bundle: session lookup failed",
      sessionErr.code,
    );
    return errorResponse(500, "internal_error");
  }
  if (!session) {
    return errorResponse(404, "not_found", "no such session");
  }

  let role: string | null;
  try {
    role = await memberRole(admin, session.project_id, user.id);
  } catch (e) {
    console.error("sign-snapshot-bundle: membership lookup failed", e);
    return errorResponse(500, "internal_error");
  }
  if (!role) {
    return errorResponse(403, "forbidden");
  }

  const objectKey = bundleObjectKey(
    session.project_id,
    session.user_id,
    input.sessionId,
    input.seq,
  );

  // upsert:true (x-upsert header — see .claude/rules/redaction.md):
  // without it, re-signing an existing object key 409s, which would
  // break retrying an upload whose register-snapshot call failed even
  // though the PUT itself succeeded.
  const { data: signed, error: signErr } = await admin.storage
    .from(BUCKET)
    .createSignedUploadUrl(objectKey, { upsert: true });
  if (signErr || !signed) {
    console.error("sign-snapshot-bundle: signing upload url failed", signErr);
    return errorResponse(500, "internal_error");
  }

  // Only the token is returned, never signedUrl — it bakes in this
  // function's own internal SUPABASE_URL (see redaction.md); the Go
  // client builds the actual upload URL from its own base URL instead.
  return json({ uploadToken: signed.token, objectKey }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}

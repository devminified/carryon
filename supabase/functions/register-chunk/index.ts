// CO-110: the agent calls this before uploading one compressed chunk. It
// checks the caller is a member of the session's project, upserts the
// sessions row on the first chunk for a given session_id — including
// CO-111's machine/branch/head_sha/claude_code_version, gathered on the
// laptop since this function can't exec git or read the machine's own
// hostname — and returns a signed Storage upload URL for the chunk's
// object key. confirm-chunk (CO-111) owns last_activity_at/total_bytes/
// status on every confirmed chunk.
//
// Service role for the writes — CO-302's session_chunks/sessions
// policies exist now, but this function legitimately still needs the
// service role for two things RLS can't do: signing the chunk's
// upload URL, and (on the very first chunk) creating the sessions row
// itself before any session-scoped policy has a row to apply to.
// Identity and membership are still enforced here from the caller's
// own JWT — never trusted from a client-supplied field.
//
// The signed URL is requested with upsert:true (x-upsert header — see
// .claude/rules/redaction.md): without it, registering a signed URL for an
// object key that already exists 409s, which would break retrying a chunk
// whose confirm-chunk call failed even though the PUT itself succeeded
// (I3). Its expiry is a fixed 2 hours set by Supabase Storage itself —
// verified against a live instance that createSignedUploadUrl has no
// expiresIn option, documented in redaction.md as the one justified
// exception to this project's normal 5-minute signed-URL rule.

import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import { chunkObjectKey } from "../_shared/chunks.ts";
import { memberRole } from "../_shared/membership.ts";
import {
  adminClient as realAdminClient,
  requireUser as realRequireUser,
} from "../_shared/supabase.ts";

const Input = z.object({
  sessionId: z.string().uuid(),
  projectId: z.string().uuid(),
  originalCwd: z.string().min(1).max(4096),
  // "main" for the session's own transcript, or a subagent's agent_id for
  // one of its child streams (CO-109) — see _shared/chunks.ts.
  streamId: z.string().min(1).max(200),
  // CO-111 session-identifying fields, gathered on the laptop — optional
  // because gathering any of them is best-effort there (no git repo
  // for head_sha/branch, "claude" not on PATH for the version), and a
  // missing one must never block the chunk upload itself.
  machine: z.string().max(255).optional(),
  branch: z.string().max(255).optional(),
  headSha: z.string().max(64).optional(),
  claudeCodeVersion: z.string().max(64).optional(),
  seq: z.number().int().nonnegative(),
  byteStart: z.number().int().nonnegative(),
  byteEnd: z.number().int().positive(),
  sha256: z.string().length(64),
  size: z.number().int().positive(),
}).refine((v) => v.byteEnd > v.byteStart, {
  message: "byteEnd must be greater than byteStart",
});
// CO-301: size is no longer required to equal byteEnd - byteStart.
// byteStart/byteEnd describe the *original* (pre-redaction) file's byte
// range; redaction can change a line's length, so size — the actual
// uploaded, compressed object's input length — is independent of that
// range once anything was actually redacted. See redaction.md.

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

  let role: string | null;
  try {
    role = await memberRole(admin, input.projectId, user.id);
  } catch (e) {
    console.error("register-chunk: membership lookup failed", e);
    return errorResponse(500, "internal_error");
  }
  if (!role) {
    return errorResponse(403, "forbidden");
  }

  // Insert-if-missing only (ignoreDuplicates) — a session's identifying
  // fields are set once, on its first chunk, and never overwritten by a
  // later one. confirm-chunk (CO-111) owns the fields that genuinely
  // change over a session's life: last_activity_at, total_bytes, status.
  const { error: sessionErr } = await admin
    .from("sessions")
    .upsert(
      {
        id: input.sessionId,
        project_id: input.projectId,
        user_id: user.id,
        original_cwd: input.originalCwd,
        machine: input.machine,
        branch: input.branch,
        head_sha: input.headSha,
        claude_code_version: input.claudeCodeVersion,
      },
      { onConflict: "id", ignoreDuplicates: true },
    );
  if (sessionErr) {
    console.error("register-chunk: session upsert failed", sessionErr.code);
    return errorResponse(500, "internal_error");
  }

  const objectKey = chunkObjectKey(
    input.projectId,
    user.id,
    input.sessionId,
    input.streamId,
    input.seq,
  );

  const { data: signed, error: signErr } = await admin.storage
    .from(BUCKET)
    .createSignedUploadUrl(objectKey, { upsert: true });
  if (signErr || !signed) {
    console.error("register-chunk: signing upload url failed", signErr);
    return errorResponse(500, "internal_error");
  }

  // signed.signedUrl bakes in this function's own internal SUPABASE_URL
  // (e.g. "http://kong:8000" inside the local Docker network) — correct
  // for a call made from *inside* the project, useless for the laptop
  // client making the actual PUT from outside it. signed.token itself
  // carries no host claim (just the object path, scope and expiry — see
  // redaction.md), so the client builds its own upload URL from its own
  // already-correct api_url instead of trusting this one. Verified
  // against a live local instance, not assumed — found while building
  // CO-110, when a real PUT from outside Docker failed DNS resolution on
  // "kong".
  return json({ uploadToken: signed.token, objectKey }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}

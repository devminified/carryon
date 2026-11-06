// CO-110: the agent calls this after successfully PUTting one compressed
// chunk to its signed upload URL. It independently verifies the object
// actually landed in Storage (a PUT can "succeed" over a flaky connection
// without the object being fully there — never trust the client's say-so
// for something this cheap to check server-side), then inserts the
// session_chunks row. Idempotent on (session_id, stream_id, seq) via
// ON CONFLICT ... DO UPDATE (I3): retrying this call for the same chunk —
// e.g. after a network failure on the *response* to a confirm that
// actually landed — replaces the row instead of erroring or duplicating.
//
// CO-111: every confirmed chunk also bumps the session's
// last_activity_at, recomputes total_bytes (summed fresh from
// session_chunks rather than incremented, so a retried confirm of an
// already-recorded chunk never double-counts it — the idempotency this
// function already needs for the chunk row itself), and sets
// status = 'live' (clearing any prior ended_reason) — this is what makes
// a resumed session (same ID, new chunks after 'ended') go back to
// 'live' with no special-cased "is this a resume" branch.
//
// Service role for the writes (CO-102: until CO-302's RLS policies land),
// but identity and membership are still enforced here from the caller's
// own JWT. The object key is derived server-side from the session's own
// stored project_id/user_id, never taken from client input — a
// client-supplied key could otherwise be pointed at an unrelated object.

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
  streamId: z.string().min(1).max(200),
  seq: z.number().int().nonnegative(),
  byteStart: z.number().int().nonnegative(),
  byteEnd: z.number().int().positive(),
  sha256: z.string().length(64),
}).refine((v) => v.byteEnd > v.byteStart, {
  message: "byteEnd must be greater than byteStart",
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
    console.error("confirm-chunk: session lookup failed", sessionErr.code);
    return errorResponse(500, "internal_error");
  }
  if (!session) {
    return errorResponse(404, "not_found", "no such session");
  }

  let role: string | null;
  try {
    role = await memberRole(admin, session.project_id, user.id);
  } catch (e) {
    console.error("confirm-chunk: membership lookup failed", e);
    return errorResponse(500, "internal_error");
  }
  if (!role) {
    return errorResponse(403, "forbidden");
  }

  const objectKey = chunkObjectKey(
    session.project_id,
    session.user_id,
    input.sessionId,
    input.streamId,
    input.seq,
  );

  const { error: infoErr } = await admin.storage.from(BUCKET).info(
    objectKey,
  );
  if (infoErr) {
    return errorResponse(
      409,
      "object_missing",
      `chunk object ${objectKey} was not found in storage — the upload may have failed`,
    );
  }

  const { error: chunkErr } = await admin
    .from("session_chunks")
    .upsert(
      {
        session_id: input.sessionId,
        stream_id: input.streamId,
        seq: input.seq,
        byte_start: input.byteStart,
        byte_end: input.byteEnd,
        object_key: objectKey,
        sha256: input.sha256,
      },
      { onConflict: "session_id,stream_id,seq" },
    );
  if (chunkErr) {
    console.error("confirm-chunk: chunk insert failed", chunkErr.code);
    return errorResponse(500, "internal_error");
  }

  const { data: chunkRows, error: sumErr } = await admin
    .from("session_chunks")
    .select("byte_start, byte_end")
    .eq("session_id", input.sessionId);
  if (sumErr) {
    console.error("confirm-chunk: total_bytes recompute failed", sumErr.code);
    return errorResponse(500, "internal_error");
  }
  const totalBytes = (chunkRows ?? []).reduce(
    (sum, row) => sum + (row.byte_end - row.byte_start),
    0,
  );

  const { error: activityErr } = await admin
    .from("sessions")
    .update({
      last_activity_at: new Date().toISOString(),
      total_bytes: totalBytes,
      status: "live",
      ended_reason: null,
    })
    .eq("id", input.sessionId);
  if (activityErr) {
    console.error(
      "confirm-chunk: session activity update failed",
      activityErr.code,
    );
    return errorResponse(500, "internal_error");
  }

  return json({ ok: true }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}

// CO-410: the agent's own periodic "I'm alive" check-in, backing the
// web admin "Agents" page (internal/api.Client.Heartbeat, called from
// cmd/carryon's runHeartbeatLoop every 5 minutes). Caller-JWT
// authenticated like any other user-initiated write — the agent
// always has the signed-in developer's own access token (internal/
// auth), never a separate machine credential.
//
// Runs as the CALLER, not the service role — CO-410's own migration
// added self_can_upsert_own_heartbeat/self_can_update_own_heartbeat
// (user_id = auth.uid()) specifically so RLS is the real enforcement
// here, same architectural shift rename-session/delete-session/
// resummarize-session already made: user_id always comes from the
// caller's own JWT, never the request body, so there's nothing for a
// client to lie about even before RLS is checked.
//
// CO-504 added os/bytesBehind/lastUploadAt/uploadAttempts/
// uploadErrors/redactionsApplied — all best-effort operational
// numbers the agent computes locally (internal/upload.Uploader.
// TakeCounts, internal/status.BytesBehind), never transcript content
// (this ticket's own "heartbeats contain no transcript data" testing
// point — nothing here is or derives from session text).

import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import {
  callerClient as realCallerClient,
  requireUser as realRequireUser,
} from "../_shared/supabase.ts";

const Input = z.object({
  machineId: z.string().min(1).max(100),
  hostname: z.string().min(1).max(255),
  version: z.string().min(1).max(50),
  blockedSessions: z.number().int().nonnegative(),
  os: z.string().min(1).max(50).optional(),
  bytesBehind: z.number().int().nonnegative().optional(),
  lastUploadAt: z.string().datetime().optional(),
  uploadAttempts: z.number().int().nonnegative().optional(),
  uploadErrors: z.number().int().nonnegative().optional(),
  redactionsApplied: z.number().int().nonnegative().optional(),
});

export interface Deps {
  callerClient: typeof realCallerClient;
  requireUser: typeof realRequireUser;
}

const defaultDeps: Deps = {
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

  const caller = deps.callerClient(authHeader!);
  const { error } = await caller.from("agent_heartbeats").upsert({
    user_id: user.id,
    machine_id: input.machineId,
    hostname: input.hostname,
    version: input.version,
    blocked_sessions: input.blockedSessions,
    last_seen_at: new Date().toISOString(),
    // Optional (an older agent binary hasn't shipped these fields
    // yet) — null/0 rather than silently dropping the column from the
    // upsert, so an upgraded admin dashboard reads a real "unknown"
    // state instead of whatever this machine's previous row happened
    // to hold.
    os: input.os ?? null,
    bytes_behind: input.bytesBehind ?? 0,
    last_upload_at: input.lastUploadAt ?? null,
    upload_attempts: input.uploadAttempts ?? 0,
    upload_errors: input.uploadErrors ?? 0,
    redactions_applied: input.redactionsApplied ?? 0,
  }, { onConflict: "user_id,machine_id" });

  if (error) {
    console.error("heartbeat: upsert failed", error.code);
    return errorResponse(500, "internal_error");
  }

  return json({ ok: true }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}

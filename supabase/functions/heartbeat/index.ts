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

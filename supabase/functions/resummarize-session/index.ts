// CO-408: the web app's own "Re-summarize" button — admin-only,
// rate-limited to once per minute per session (this ticket's own
// testing point). Unlike summarize-session's cron sweep (x-cron-secret,
// no caller JWT at all), this is a real user-initiated action, so it
// goes through the caller's own JWT, the same identity-check shape
// every other admin-only write in this codebase uses (delete-session,
// rename-session).
//
// The rate-limit marker (sessions.last_resummarize_requested_at) is
// set *before* calling summarizeSession, not after — it throttles
// repeated clicks, not repeated successful runs, so a slow or failed
// run still correctly starts the next cooldown from the moment of the
// request. That write runs as the caller: CO-405's own
// admins_can_update_sessions RLS policy already covers it, the same
// architectural shift delete-session/rename-session/list-sessions
// already made once CO-302 made it safe.
//
// The ANTHROPIC_API_KEY check deliberately runs *last*, right before
// the one call that actually needs it — not up front. The admin
// check, the rate limit, and the marker write are all real,
// independent guarantees that hold regardless of whether the key
// happens to be configured; checking it first would mean a
// misconfigured environment could never even exercise (or test) the
// rate limit at all.

import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import { hasMinRole, memberRole } from "../_shared/membership.ts";
import {
  adminClient as realAdminClient,
  callerClient as realCallerClient,
  requireUser as realRequireUser,
} from "../_shared/supabase.ts";
import { callAnthropic as realCallAnthropic } from "../_shared/anthropic.ts";
import {
  realNow,
  realSleep,
  summarizeSession as realSummarizeSession,
} from "../_shared/summarize.ts";
import { embedText as realEmbedText } from "../_shared/embed.ts";

const Input = z.object({
  sessionId: z.string().uuid(),
});

const RATE_LIMIT_MS = 60_000;

export interface Deps {
  adminClient: typeof realAdminClient;
  callerClient: typeof realCallerClient;
  requireUser: typeof realRequireUser;
  callAnthropic: typeof realCallAnthropic;
  summarizeSession: typeof realSummarizeSession;
  sleep: (ms: number) => Promise<void>;
  now: () => Date;
}

const defaultDeps: Deps = {
  adminClient: realAdminClient,
  callerClient: realCallerClient,
  requireUser: realRequireUser,
  callAnthropic: realCallAnthropic,
  summarizeSession: realSummarizeSession,
  sleep: realSleep,
  now: realNow,
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
    .select("project_id, last_resummarize_requested_at")
    .eq("id", input.sessionId)
    .maybeSingle();
  if (sessionErr) {
    console.error(
      "resummarize-session: session lookup failed",
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
    console.error("resummarize-session: membership lookup failed", e);
    return errorResponse(500, "internal_error");
  }
  if (!hasMinRole(role, "admin")) {
    return errorResponse(403, "forbidden");
  }

  const now = deps.now();
  if (session.last_resummarize_requested_at) {
    const elapsedMs = now.getTime() -
      new Date(session.last_resummarize_requested_at).getTime();
    if (elapsedMs < RATE_LIMIT_MS) {
      const retryAfterSeconds = Math.ceil((RATE_LIMIT_MS - elapsedMs) / 1000);
      return json({ error: "rate_limited", retryAfterSeconds }, 429);
    }
  }

  const caller = deps.callerClient(authHeader!);
  const { data: updatedRows, error: updateErr } = await caller
    .from("sessions")
    .update({ last_resummarize_requested_at: now.toISOString() })
    .eq("id", input.sessionId)
    .select("id");
  if (updateErr) {
    console.error(
      "resummarize-session: rate-limit marker update failed",
      updateErr.code,
    );
    return errorResponse(500, "internal_error");
  }
  if (!updatedRows || updatedRows.length === 0) {
    // RLS matched zero rows — the manual admin check above already
    // confirmed this caller is an admin, so this would only mean the
    // session vanished between that check and this write.
    return errorResponse(403, "forbidden");
  }

  const anthropicApiKey = Deno.env.get("ANTHROPIC_API_KEY");
  if (!anthropicApiKey) {
    console.error("resummarize-session: ANTHROPIC_API_KEY is not configured");
    return errorResponse(500, "internal_error");
  }

  const outcome = await deps.summarizeSession(
    {
      adminClient: admin,
      callAnthropic: deps.callAnthropic,
      anthropicApiKey,
      sleep: deps.sleep,
      now: deps.now,
      embedText: realEmbedText,
    },
    input.sessionId,
  );

  return json({ outcome: outcome.status }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}

// CO-111: the agent calls this once, right after the upload covering a
// session's SessionEnd event (Claude Code's own `Stop` hook, forced to
// "SessionEnd" by `carryon hook --final` — see internal/hook) has been
// confirmed. It sets status = 'ended', ended_reason = 'explicit' — a
// resumed session (same ID, a later chunk confirmed) goes back to 'live'
// on its own via confirm-chunk's activity update, with no call back into
// this function needed.
//
// Idempotent on sessionId alone (I3): calling this twice for the same
// session, or calling it for a session the idle sweep already ended, is
// a safe no-op — the UPDATE just rewrites the same values.
//
// CO-302's "owner_can_update_their_sessions" policy is the real
// enforcement now: only the session's own owner may end it (not just
// any project member, which the old stopgap check allowed) — ending a
// session is the capturing agent's own act, not a teammate's. The
// update runs as the caller; RLS silently matches zero rows for anyone
// but the owner, which this function maps to the same 403 a non-member
// used to get. The existence check stays on the service role so a
// missing session is still reported as 404 rather than folded into the
// same 403 a non-owner gets.
//
// CO-407: once the status update actually succeeds, this also triggers
// the session's final AI summary — in-process, via a direct call into
// _shared/summarize.ts's summarizeSession(), not an HTTP round trip to
// the summarize-session Edge Function (that one exists only for its
// own 30-minute cron sweep over *live* sessions; see its own header
// comment). Awaited, not fire-and-forget: summarizeSession's own
// try/catch-free design already can't throw in a way that should fail
// this response (every real failure mode returns a status instead —
// api_failure, invalid_response, budget_exceeded, see that module),
// but this handler still wraps the call in its own try/catch as a
// last line of defense, since a session ending successfully must
// never be undone by a summarization bug.

import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
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

// Deps exists so tests can inject fakes for the two Supabase calls
// without a live instance — see index.test.ts.
export interface Deps {
  adminClient: typeof realAdminClient;
  callerClient: typeof realCallerClient;
  requireUser: typeof realRequireUser;
  callAnthropic: typeof realCallAnthropic;
  summarizeSession: typeof realSummarizeSession;
}

const defaultDeps: Deps = {
  adminClient: realAdminClient,
  callerClient: realCallerClient,
  requireUser: realRequireUser,
  callAnthropic: realCallAnthropic,
  summarizeSession: realSummarizeSession,
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
    .select("id")
    .eq("id", input.sessionId)
    .maybeSingle();
  if (sessionErr) {
    console.error("end-session: session lookup failed", sessionErr.code);
    return errorResponse(500, "internal_error");
  }
  if (!session) {
    return errorResponse(404, "not_found", "no such session");
  }

  const caller = deps.callerClient(authHeader!);
  const { data: updated, error: updateErr } = await caller
    .from("sessions")
    .update({ status: "ended", ended_reason: "explicit" })
    .eq("id", input.sessionId)
    .select("id");
  if (updateErr) {
    console.error("end-session: update failed", updateErr.code);
    return errorResponse(500, "internal_error");
  }
  if (!updated || updated.length === 0) {
    // RLS matched zero rows — the session exists (checked above) but
    // this caller isn't its owner.
    return errorResponse(403, "forbidden");
  }

  const anthropicApiKey = Deno.env.get("ANTHROPIC_API_KEY");
  if (!anthropicApiKey) {
    console.error(
      "end-session: ANTHROPIC_API_KEY is not configured — skipping final summary",
    );
  } else {
    try {
      await deps.summarizeSession(
        {
          adminClient: admin,
          callAnthropic: deps.callAnthropic,
          anthropicApiKey,
          sleep: realSleep,
          now: realNow,
          embedText: realEmbedText,
        },
        input.sessionId,
      );
    } catch (e) {
      console.error(
        `end-session: final summary failed for session ${input.sessionId}`,
        e instanceof Error ? e.message : String(e),
      );
    }
  }

  return json({ ok: true }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}

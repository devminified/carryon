// CO-407: the AI title/summary job's own 30-minute live-session sweep
// — a pg_cron job calls this via net.http_post (config.toml's
// verify_jwt = false for this function, same shape as rescan/
// sync-github-teams/purge-expired-sessions), authenticated by a
// shared x-cron-secret header rather than a caller JWT.
//
// This endpoint is NOT the "triggered on end-session" half of the
// ticket — that path never comes through here at all. end-session
// imports and calls _shared/summarize.ts's summarizeSession()
// directly, in-process, once a session it just ended needs its final
// summary; no HTTP hop, no auth to check, since there's no separate
// caller to authenticate (see end-session/index.ts's own comment).
//
// Every LIVE session is a candidate — not just ones session_chunks
// says have new chunks past summarized_through_seq. Pre-filtering
// that in SQL would need a per-session MAX(seq) join Supabase-js's
// query builder doesn't express directly, and at this project's
// actual scale (a small team's live sessions, not thousands) the
// cost of calling summarizeSession() on a session with nothing new is
// one cheap, empty session_chunks SELECT — it returns `{status:
// "skipped", reason: "no_new_chunks"}` immediately, no Anthropic call
// made (CLAUDE.md: no speculative scale). If live-session counts ever
// make that sweep itself too slow, that's the ticket to add the join.
//
// A session summarized just fine right before this cron run (end-
// session's own call) isn't double-charged: summarized_through_seq
// already reflects its latest chunk by then, so this sweep's own
// readSessionMainChunks call for it also comes back empty.

import { errorResponse, json } from "../_shared/http.ts";
import { adminClient as realAdminClient } from "../_shared/supabase.ts";
import { callAnthropic as realCallAnthropic } from "../_shared/anthropic.ts";
import {
  realNow,
  realSleep,
  type SummarizeOutcome,
  summarizeSession,
} from "../_shared/summarize.ts";
import { embedText as realEmbedText } from "../_shared/embed.ts";

const BATCH_LIMIT = 200;

export interface Deps {
  adminClient: typeof realAdminClient;
  callAnthropic: typeof realCallAnthropic;
  sleep: (ms: number) => Promise<void>;
  now: () => Date;
}

const defaultDeps: Deps = {
  adminClient: realAdminClient,
  callAnthropic: realCallAnthropic,
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

  const cronSecret = Deno.env.get("SUMMARIZE_CRON_SECRET");
  if (!cronSecret || req.headers.get("x-cron-secret") !== cronSecret) {
    return errorResponse(401, "unauthorized");
  }

  const anthropicApiKey = Deno.env.get("ANTHROPIC_API_KEY");
  if (!anthropicApiKey) {
    console.error("summarize-session: ANTHROPIC_API_KEY is not configured");
    return errorResponse(500, "internal_error");
  }

  const admin = deps.adminClient();

  const { data: liveSessions, error: liveErr } = await admin
    .from("sessions")
    .select("id")
    .eq("status", "live")
    .limit(BATCH_LIMIT);
  if (liveErr) {
    console.error(
      "summarize-session: live session lookup failed",
      liveErr.code,
    );
    return errorResponse(500, "internal_error");
  }

  const counts: Record<SummarizeOutcome["status"], number> = {
    summarized: 0,
    skipped: 0,
    gave_up: 0,
  };

  for (const session of liveSessions ?? []) {
    try {
      const outcome = await summarizeSession(
        {
          adminClient: admin,
          callAnthropic: deps.callAnthropic,
          anthropicApiKey,
          sleep: deps.sleep,
          now: deps.now,
          embedText: realEmbedText,
        },
        session.id,
      );
      counts[outcome.status]++;
    } catch (e) {
      // One session's unexpected failure (a real bug, not one of
      // summarizeSession's own handled outcomes) must never abort the
      // whole sweep — every other live session still deserves its
      // turn this run.
      console.error(
        `summarize-session: unexpected error summarizing session ${session.id}`,
        e instanceof Error ? e.message : String(e),
      );
      counts.gave_up++;
    }
  }

  return json(counts, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}

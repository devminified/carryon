// CO-409: the opt-in daily per-developer digest (notifications.md) —
// one Slack message per project, listing how many sessions each
// developer had activity on in the last 24 hours. Opt-in on top of a
// configured webhook: a project with slack_webhook_url set but
// notify_digest = false (the default) gets blocked-session alerts
// (summarize.ts) and rescan hits (rescan/index.ts) but never this
// message — a team that only wants the urgent alert shouldn't get a
// daily one it never asked for.
//
// Called by a pg_cron job via net.http_post, not a browser or the
// CLI — same shape as sync-github-teams/rescan/purge-expired-
// sessions/summarize-session above it (edge-functions.md's
// "scheduled Edge Functions" section), authenticated by a shared
// x-cron-secret header (DIGEST_CRON_SECRET) instead of a caller JWT.
//
// A project with zero session activity in the window sends nothing —
// an empty "nobody did anything" message every single day would
// train a team to ignore the channel entirely, and this ticket's own
// "projects with notifications off send nothing" testing point reads
// naturally as "nothing to report" too, not just "no webhook
// configured."

import { errorResponse, json } from "../_shared/http.ts";
import { adminClient as realAdminClient } from "../_shared/supabase.ts";
import {
  buildDailyDigestMessage,
  type DigestEntry,
  postToSlackWithRetry as realPostToSlackWithRetry,
} from "../_shared/slack.ts";
import {
  buildDailyDigestMessage as buildMattermostDailyDigestMessage,
  postToMattermostWithRetry as realPostToMattermostWithRetry,
} from "../_shared/mattermost.ts";

const DIGEST_WINDOW_HOURS = 24;

export interface Deps {
  adminClient: typeof realAdminClient;
  postToSlack?: typeof realPostToSlackWithRetry;
  // Follow-up to CO-409 (#113): same optional-with-a-real-default
  // shape, parallel to postToSlack above.
  postToMattermost?: typeof realPostToMattermostWithRetry;
  now: () => Date;
}

const defaultDeps: Deps = {
  adminClient: realAdminClient,
  now: () => new Date(),
};

interface ProjectRow {
  project_id: string;
  slack_webhook_url: string | null;
  mattermost_webhook_url: string | null;
}

export async function handle(
  req: Request,
  deps: Deps = defaultDeps,
): Promise<Response> {
  if (req.method !== "POST") {
    return errorResponse(405, "method_not_allowed");
  }

  const cronSecret = Deno.env.get("DIGEST_CRON_SECRET");
  if (!cronSecret || req.headers.get("x-cron-secret") !== cronSecret) {
    return errorResponse(401, "unauthorized");
  }

  const admin = deps.adminClient();
  const sendSlack = deps.postToSlack ?? realPostToSlackWithRetry;
  const sendMattermost = deps.postToMattermost ??
    realPostToMattermostWithRetry;

  const { data: optedInRows, error: optedInErr } = await admin
    .from("project_notification_settings")
    .select("project_id, slack_webhook_url, mattermost_webhook_url")
    .eq("notify_digest", true);
  if (optedInErr) {
    console.error(
      "send-daily-digest: notification settings lookup failed",
      optedInErr.code,
    );
    return errorResponse(500, "internal_error");
  }
  // "at least one webhook configured" — not just Slack's, so a
  // project with only Mattermost set still gets its digest (#113's
  // own "a project with only Mattermost configured gets no Slack
  // calls at all, and vice versa" testing point, applied to this
  // function's own opt-in filter, not just the per-channel send).
  // Filtered in JS rather than a second `.or(...)` DB clause — same
  // "dev stays the post-fetch-in-JS filter" precedent list-sessions
  // already established (edge-functions.md) — notify_digest=true
  // rows are few, and this keeps the test double's query-builder
  // stub from needing to grow an .or() it otherwise never uses.
  const optedIn = ((optedInRows ?? []) as ProjectRow[]).filter((r) =>
    r.slack_webhook_url || r.mattermost_webhook_url
  );
  if (optedIn.length === 0) {
    return json({ projectsNotified: 0 }, 200);
  }

  const since = new Date(
    deps.now().getTime() - DIGEST_WINDOW_HOURS * 60 * 60 * 1000,
  ).toISOString();

  let projectsNotified = 0;

  for (const row of optedIn) {
    const { data: projectRow, error: projectErr } = await admin
      .from("projects")
      .select("name")
      .eq("id", row.project_id)
      .maybeSingle();
    if (projectErr || !projectRow) {
      console.error(
        `send-daily-digest: project lookup failed for ${row.project_id}`,
        projectErr?.code,
      );
      continue;
    }

    const { data: sessionRows, error: sessionsErr } = await admin
      .from("sessions")
      .select("user_id")
      .eq("project_id", row.project_id)
      .gte("last_activity_at", since);
    if (sessionsErr) {
      console.error(
        `send-daily-digest: session lookup failed for project ${row.project_id}`,
        sessionsErr.code,
      );
      continue;
    }
    const rows = (sessionRows ?? []) as { user_id: string }[];
    if (rows.length === 0) continue; // nothing happened — send nothing

    const countByUser = new Map<string, number>();
    for (const r of rows) {
      countByUser.set(r.user_id, (countByUser.get(r.user_id) ?? 0) + 1);
    }

    const entries: DigestEntry[] = [];
    for (const [userId, sessionCount] of countByUser) {
      const { data: owner, error: ownerErr } = await admin.auth.admin
        .getUserById(userId);
      if (ownerErr) {
        console.error(
          `send-daily-digest: owner lookup failed for user ${userId}`,
          ownerErr,
        );
      }
      entries.push({
        developerLogin: owner?.user?.user_metadata?.user_name ?? "someone",
        sessionCount,
      });
    }
    entries.sort((a, b) =>
      b.sessionCount - a.sessionCount ||
      a.developerLogin.localeCompare(b.developerLogin)
    );

    // Independent fan-out — a failed Slack post must never block
    // Mattermost, or vice versa.
    const sends: Promise<boolean>[] = [];
    if (row.slack_webhook_url) {
      sends.push(
        sendSlack(
          row.slack_webhook_url,
          buildDailyDigestMessage({ projectName: projectRow.name, entries }),
        ),
      );
    }
    if (row.mattermost_webhook_url) {
      sends.push(
        sendMattermost(
          row.mattermost_webhook_url,
          buildMattermostDailyDigestMessage({
            projectName: projectRow.name,
            entries,
          }),
        ),
      );
    }
    await Promise.all(sends);
    projectsNotified++;
  }

  return json({ projectsNotified }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}

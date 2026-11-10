// CO-504: the fleet-wide observability sweep. Called by a pg_cron job
// via net.http_post every 30 minutes — same shape as sync-github-
// teams/rescan/purge-expired-sessions/send-daily-digest above it
// (edge-functions.md's "scheduled Edge Functions" section),
// authenticated by a shared x-cron-secret header
// (CHECK_ALERTS_CRON_SECRET) instead of a caller JWT.
//
// Does two things each run:
//   1. Snapshots today's real per-project storage total
//      (project_storage_totals) into storage_snapshots, so
//      "abnormal_storage_growth_project_ids" always has a real
//      yesterday-vs-today baseline to compare against.
//   2. Evaluates the four alert conditions this ticket names (a
//      stale agent on a workday, lag p95, the fleet upload error
//      rate, and per-project storage growth) against
//      observability_alert_state, posting to Slack only on the
//      false -> true transition and silently clearing on true ->
//      false — never re-notifying every run while something stays
//      bad, same "fire once, clear quietly" shape
//      _shared/summarize.ts's own blocked-session alert already
//      established.

import { errorResponse, json } from "../_shared/http.ts";
import { adminClient as realAdminClient } from "../_shared/supabase.ts";
import {
  buildObservabilityAlertMessage,
  postToSlackWithRetry as realPostToSlackWithRetry,
} from "../_shared/slack.ts";

// NFR-02's own 2-minute SLA, read generously (10x) for an *alert*
// threshold rather than the SLA itself — this is "something is
// clearly wrong," not "slightly over budget."
const LAG_P95_ALERT_SECONDS = 10 * 60;
const ERROR_RATE_ALERT_THRESHOLD = 0.05;

export interface Deps {
  adminClient: typeof realAdminClient;
  postToSlack: typeof realPostToSlackWithRetry;
  today: () => string; // YYYY-MM-DD, injectable so tests don't depend on real wall-clock date
}

const defaultDeps: Deps = {
  adminClient: realAdminClient,
  postToSlack: realPostToSlackWithRetry,
  today: () => new Date().toISOString().slice(0, 10),
};

// deno-lint-ignore no-explicit-any
type AdminClient = any;

async function snapshotStorage(
  admin: AdminClient,
  today: string,
): Promise<void> {
  const { data: totals, error } = await admin.rpc("project_storage_totals");
  if (error) {
    console.error(
      "check-observability-alerts: project_storage_totals failed",
      error.code,
    );
    return;
  }
  for (const row of totals ?? []) {
    const { error: upsertErr } = await admin.from("storage_snapshots").upsert(
      {
        project_id: row.project_id,
        snapshotted_at: today,
        total_bytes: row.total_bytes,
      },
      { onConflict: "project_id,snapshotted_at" },
    );
    if (upsertErr) {
      console.error(
        "check-observability-alerts: storage snapshot upsert failed",
        row.project_id,
        upsertErr.code,
      );
    }
  }
}

// Writes the new is_firing state and, only on a false -> true
// transition, posts to Slack. A no-op if firing hasn't changed since
// the last run — this is the whole "fire once, clear quietly" rule in
// one place, reused by every alert key below.
async function applyAlertTransition(
  admin: AdminClient,
  postToSlack: typeof realPostToSlackWithRetry,
  webhookUrl: string | null,
  key: string,
  firing: boolean,
  summary: string,
  detail?: string,
): Promise<void> {
  const { data: existing, error: readErr } = await admin
    .from("observability_alert_state")
    .select("is_firing")
    .eq("alert_key", key)
    .maybeSingle();
  if (readErr) {
    console.error(
      "check-observability-alerts: alert state read failed",
      key,
      readErr.code,
    );
    return;
  }
  const wasFiring = existing?.is_firing ?? false;
  if (firing === wasFiring) return;

  const now = new Date().toISOString();
  const { error: writeErr } = await admin.from("observability_alert_state")
    .upsert(
      {
        alert_key: key,
        is_firing: firing,
        ...(firing ? { last_fired_at: now } : { last_cleared_at: now }),
        updated_at: now,
      },
      { onConflict: "alert_key" },
    );
  if (writeErr) {
    console.error(
      "check-observability-alerts: alert state write failed",
      key,
      writeErr.code,
    );
    return;
  }

  if (firing && webhookUrl) {
    await postToSlack(
      webhookUrl,
      buildObservabilityAlertMessage({ summary, detail }),
    );
  }
}

export async function handle(
  req: Request,
  deps: Deps = defaultDeps,
): Promise<Response> {
  if (req.method !== "POST") {
    return errorResponse(405, "method_not_allowed");
  }

  const cronSecret = Deno.env.get("CHECK_ALERTS_CRON_SECRET");
  if (!cronSecret || req.headers.get("x-cron-secret") !== cronSecret) {
    return errorResponse(401, "unauthorized");
  }

  const admin = deps.adminClient();
  const webhookUrl = Deno.env.get("OBSERVABILITY_SLACK_WEBHOOK_URL") ?? null;

  await snapshotStorage(admin, deps.today());

  const [
    stale,
    p95,
    errorRate,
    growingProjectIds,
    firingGrowthAlerts,
    firingStaleAgentAlerts,
  ] = await Promise.all([
    admin.rpc("stale_agents_on_a_workday"),
    admin.rpc("lag_p95_seconds"),
    admin.rpc("fleet_upload_error_rate"),
    admin.rpc("abnormal_storage_growth_project_ids"),
    admin.from("observability_alert_state").select("alert_key").eq(
      "is_firing",
      true,
    ).like(
      "alert_key",
      "storage_growth:%",
    ),
    admin.from("observability_alert_state").select("alert_key").eq(
      "is_firing",
      true,
    ).like(
      "alert_key",
      "stale_agent:%",
    ),
  ]);

  // Per-machine, same set-difference shape as storage_growth below —
  // the ticket's own wording is "an agent hasn't checked in," i.e. a
  // specific one, not a single fleet-wide boolean (see
  // stale_agents_on_a_workday's own migration comment for why that
  // mattered enough to fix).
  if (stale.error || firingStaleAgentAlerts.error) {
    console.error(
      "check-observability-alerts: stale agent check failed",
      stale.error?.code ?? firingStaleAgentAlerts.error?.code,
    );
  } else {
    interface StaleAgentRow {
      user_id: string;
      machine_id: string;
      hostname: string;
    }
    const nowStale = (stale.data ?? []) as StaleAgentRow[];
    const nowStaleKeys = new Set(
      nowStale.map((r) => `${r.user_id}:${r.machine_id}`),
    );
    const previouslyFiring = new Set(
      ((firingStaleAgentAlerts.data ?? []) as { alert_key: string }[]).map(
        (r) => r.alert_key.slice("stale_agent:".length),
      ),
    );
    for (const row of nowStale) {
      await applyAlertTransition(
        admin,
        deps.postToSlack,
        webhookUrl,
        `stale_agent:${row.user_id}:${row.machine_id}`,
        true,
        `${row.hostname} hasn't checked in for 24 hours on a workday.`,
      );
    }
    for (const key of previouslyFiring) {
      if (!nowStaleKeys.has(key)) {
        await applyAlertTransition(
          admin,
          deps.postToSlack,
          webhookUrl,
          `stale_agent:${key}`,
          false,
          "",
        );
      }
    }
  }

  if (p95.error) {
    console.error(
      "check-observability-alerts: lag_p95_seconds failed",
      p95.error.code,
    );
  } else {
    const seconds = p95.data as number | null;
    await applyAlertTransition(
      admin,
      deps.postToSlack,
      webhookUrl,
      "lag_p95",
      seconds !== null && seconds > LAG_P95_ALERT_SECONDS,
      "Cloud lag p95 exceeds 10 minutes.",
      seconds !== null
        ? `p95 is currently ${Math.round(seconds / 60)} minutes.`
        : undefined,
    );
  }

  if (errorRate.error) {
    console.error(
      "check-observability-alerts: fleet_upload_error_rate failed",
      errorRate.error.code,
    );
  } else {
    const rate = errorRate.data as number | null;
    await applyAlertTransition(
      admin,
      deps.postToSlack,
      webhookUrl,
      "error_rate",
      rate !== null && rate > ERROR_RATE_ALERT_THRESHOLD,
      "Fleet-wide upload error rate exceeds 5%.",
      rate !== null ? `Currently ${(rate * 100).toFixed(1)}%.` : undefined,
    );
  }

  if (growingProjectIds.error || firingGrowthAlerts.error) {
    console.error(
      "check-observability-alerts: storage growth check failed",
      growingProjectIds.error?.code ?? firingGrowthAlerts.error?.code,
    );
  } else {
    const nowGrowing = new Set((growingProjectIds.data ?? []) as string[]);
    const previouslyFiring = new Set(
      (firingGrowthAlerts.data ?? []).map((r: { alert_key: string }) =>
        r.alert_key.slice("storage_growth:".length)
      ),
    );
    for (const projectId of nowGrowing) {
      await applyAlertTransition(
        admin,
        deps.postToSlack,
        webhookUrl,
        `storage_growth:${projectId}`,
        true,
        `Project ${projectId} storage grew abnormally in the last day.`,
      );
    }
    for (const projectId of previouslyFiring) {
      if (!nowGrowing.has(projectId)) {
        await applyAlertTransition(
          admin,
          deps.postToSlack,
          webhookUrl,
          `storage_growth:${projectId}`,
          false,
          "",
        );
      }
    }
  }

  return json({ ok: true }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}

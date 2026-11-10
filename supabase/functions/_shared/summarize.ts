// CO-407: the AI title/summary job's own core logic — shared between
// summarize-session's cron-triggered batch sweep (live sessions with
// new chunks, every 30 minutes) and end-session's direct, in-process
// call (no HTTP hop — see that function's own comment) when a session
// ends. Both ultimately call summarizeSession() for one session at a
// time.
//
// Three distinct kinds of failure, handled differently, matching the
// ticket's own testing points:
//   - over budget: skipped before ever calling Anthropic, logged as a
//     warning, not an error — this is expected, routine behavior once
//     a project's daily cap is reached, not a bug.
//   - a transport/5xx failure calling Anthropic: retried with backoff
//     (MAX_API_ATTEMPTS, exponential), then given up — the old
//     summary is left untouched, a warning is logged.
//   - a response that doesn't parse as valid JSON matching the
//     schema (including an over-long title): the API is called again
//     ONCE more with a corrective prompt; if that's also invalid, the
//     old summary is left untouched, a warning is logged.
// In every "gave up" case, nothing is written to `sessions` at all —
// a partial/wrong write would be worse than no write.

import { z } from "npm:zod@4.6.7";
import type { SupabaseClient } from "npm:@supabase/supabase-js@2.119.0";
import {
  type AnthropicResult,
  callAnthropic as realCallAnthropic,
  estimateCostUSD,
  MODEL,
} from "./anthropic.ts";
import {
  condenseSession,
  readSessionMainChunks,
} from "./condense-transcript.ts";
import { resolveBucket } from "./storage.ts";
import {
  buildBlockedSessionMessage,
  buildSessionUrl,
  postToSlackWithRetry as realPostToSlackWithRetry,
} from "./slack.ts";
import { embedText as realEmbedText } from "./embed.ts";

const MAX_API_ATTEMPTS = 3;
const BASE_BACKOFF_MS = 500;
const MAX_TITLE_WORDS = 8;

const ResultSchema = z.object({
  title: z.string().trim().min(1),
  summary: z.string().trim().min(1),
  outcome: z.enum(["done", "in_progress", "blocked"]),
  blocked_reason: z.string().trim().min(1).nullable().optional(),
  files_touched: z.array(z.string()),
});

type ParsedResult = z.infer<typeof ResultSchema>;

function wordCount(s: string): number {
  return s.trim().split(/\s+/).filter((w) => w.length > 0).length;
}

/** Parses and validates one Anthropic response's text against the
 * ticket's own schema, including the "title is at most 8 words" rule
 * a plain Zod type can't express as a length constraint on word
 * count. Returns null (not a throw) on anything invalid — a bad
 * response is an expected outcome this module retries once, not a
 * programmer error. */
function parseResult(text: string): ParsedResult | null {
  let raw: unknown;
  try {
    // The model is asked for raw JSON with no markdown fence, but a
    // stray ```json fence is a cheap, real-world failure mode worth
    // stripping before giving up on it outright.
    const stripped = text.replace(/^```(?:json)?\s*/i, "").replace(
      /```\s*$/i,
      "",
    ).trim();
    raw = JSON.parse(stripped);
  } catch {
    return null;
  }
  const parsed = ResultSchema.safeParse(raw);
  if (!parsed.success) return null;
  if (wordCount(parsed.data.title) > MAX_TITLE_WORDS) return null;
  if (parsed.data.outcome === "blocked" && !parsed.data.blocked_reason) {
    return null;
  }
  return parsed.data;
}

function buildSystemPrompt(): string {
  return [
    "You summarize a software developer's Claude Code session from a condensed transcript.",
    "Respond with ONLY a single JSON object, no markdown fence, no commentary, matching exactly this shape:",
    '{"title": string (at most 8 words), "summary": string (3-5 sentences: what was asked, what was done, where it stopped), "outcome": "done" | "in_progress" | "blocked", "blocked_reason": string | null (required, non-null, when outcome is "blocked"; null otherwise), "files_touched": string[] }',
  ].join("\n");
}

function buildUserPrompt(
  condensedText: string,
  previousSummary: string | null,
): string {
  const parts: string[] = [];
  if (previousSummary) {
    parts.push(
      `Here is the summary of this session so far:\n${previousSummary}`,
    );
    parts.push("Here is what has happened since then:");
  } else {
    parts.push("Here is the condensed session transcript:");
  }
  parts.push(condensedText);
  return parts.join("\n\n");
}

/** Real sleep/clock — both real call sites (end-session,
 * summarize-session) use these; tests inject a no-op sleep and a
 * fixed clock instead, so backoff and budget-window tests run
 * instantly and deterministically. */
export async function realSleep(ms: number): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, ms));
}
export const realNow = (): Date => new Date();

export interface SummarizeDeps {
  adminClient: SupabaseClient;
  callAnthropic: typeof realCallAnthropic;
  anthropicApiKey: string;
  sleep: (ms: number) => Promise<void>;
  now: () => Date;
  // CO-409: optional so every existing call site (end-session,
  // summarize-session's cron sweep, resummarize-session) keeps
  // working unchanged — real callers fall back to the real Slack
  // sender below; tests inject a stub to assert on what would have
  // been sent, without ever making a real network call.
  postToSlack?: typeof realPostToSlackWithRetry;
  // CO-501: same optional-with-a-real-default shape — real callers
  // fall back to the real gte-small embedder; tests inject a small
  // deterministic fake instead. Required here, not optional with a
  // silent no-op default: unlike Slack (truly optional per project),
  // every session gets embedded, so a test that forgets to pass one
  // should fail loudly rather than silently calling the real
  // Supabase.ai global, which is undefined outside the real edge
  // runtime (see embed.ts's own comment).
  embedText: typeof realEmbedText;
}

export type SummarizeOutcome =
  | { status: "summarized" }
  | {
    status: "skipped";
    reason: "no_new_chunks" | "not_live_or_ended" | "budget_exceeded";
  }
  | { status: "gave_up"; reason: "api_failure" | "invalid_response" };

/** Calls Anthropic with backoff on a transport/5xx failure
 * (AnthropicAPIError, or a network-level throw) — exhausting
 * MAX_API_ATTEMPTS re-throws the last error for the caller to log and
 * give up on, same "leave the old summary in place" contract either
 * kind of failure shares. */
async function callWithRetry(
  deps: SummarizeDeps,
  systemPrompt: string,
  userPrompt: string,
): Promise<AnthropicResult> {
  let lastErr: unknown;
  for (let attempt = 0; attempt < MAX_API_ATTEMPTS; attempt++) {
    try {
      return await deps.callAnthropic(
        deps.anthropicApiKey,
        systemPrompt,
        userPrompt,
      );
    } catch (e) {
      lastErr = e;
      if (attempt < MAX_API_ATTEMPTS - 1) {
        await deps.sleep(BASE_BACKOFF_MS * 2 ** attempt);
      }
    }
  }
  throw lastErr;
}

async function logUsage(
  deps: SummarizeDeps,
  projectId: string,
  sessionId: string,
  usage: AnthropicResult["usage"],
): Promise<void> {
  const { error } = await deps.adminClient.from("ai_usage").insert({
    project_id: projectId,
    session_id: sessionId,
    input_tokens: usage.inputTokens,
    output_tokens: usage.outputTokens,
    cost_usd: estimateCostUSD(usage),
    model: MODEL,
  });
  if (error) {
    console.error("summarizeSession: ai_usage insert failed", error.code);
  }
}

async function todaysTokenUsage(
  deps: SummarizeDeps,
  projectId: string,
): Promise<number> {
  const startOfDay = new Date(deps.now());
  startOfDay.setUTCHours(0, 0, 0, 0);

  const { data, error } = await deps.adminClient
    .from("ai_usage")
    .select("input_tokens, output_tokens")
    .eq("project_id", projectId)
    .gte("created_at", startOfDay.toISOString());
  if (error) throw error;

  return (data ?? []).reduce(
    (sum: number, row: { input_tokens: number; output_tokens: number }) =>
      sum + row.input_tokens + row.output_tokens,
    0,
  );
}

/** Summarizes one session: reads its new main-stream chunks since
 * summarized_through_seq, condenses them (CO-406), calls Anthropic
 * with that plus the existing summary as context, validates the
 * result (retrying once on an invalid response), and writes
 * title/summary/outcome/blocked_reason/files_touched —
 * never overwriting a manual title (title_source = 'manual',
 * CO-405's own contract this function exists to honor). */
export async function summarizeSession(
  deps: SummarizeDeps,
  sessionId: string,
): Promise<SummarizeOutcome> {
  const startedAt = deps.now();
  const outcome = await summarizeSessionCore(deps, sessionId);
  await recordJobRun(
    deps,
    sessionId,
    outcome,
    deps.now().getTime() - startedAt.getTime(),
  );
  return outcome;
}

// CO-504: one row per summarizeSession call (skipped, gave up, or a
// real success), independent of ai_usage (which only ever logs a
// successful Anthropic call, never a failure or a skip) — this is
// what the observability dashboard's "summary job latency and
// failures" tiles actually read. Best-effort: a logging failure here
// is reported but never turns a real summarization outcome into an
// error of its own.
async function recordJobRun(
  deps: SummarizeDeps,
  sessionId: string,
  outcome: SummarizeOutcome,
  durationMs: number,
): Promise<void> {
  const { error } = await deps.adminClient.from("summarize_job_runs").insert({
    session_id: sessionId,
    status: outcome.status,
    reason: "reason" in outcome ? outcome.reason : null,
    duration_ms: Math.max(0, Math.round(durationMs)),
  });
  if (error) {
    console.error(
      `summarizeSession: recording the job run failed for session ${sessionId}`,
      error.code,
    );
  }
}

async function summarizeSessionCore(
  deps: SummarizeDeps,
  sessionId: string,
): Promise<SummarizeOutcome> {
  const { data: session, error: sessionErr } = await deps.adminClient
    .from("sessions")
    .select(
      "id, project_id, user_id, status, title, title_source, summary, outcome, summarized_through_seq, files_touched, original_cwd",
    )
    .eq("id", sessionId)
    .maybeSingle();
  if (sessionErr) throw sessionErr;
  if (!session || (session.status !== "live" && session.status !== "ended")) {
    return { status: "skipped", reason: "not_live_or_ended" };
  }

  const { data: project, error: projectErr } = await deps.adminClient
    .from("projects")
    .select("bucket, daily_token_budget")
    .eq("id", session.project_id)
    .maybeSingle();
  if (projectErr) throw projectErr;

  const usedToday = await todaysTokenUsage(deps, session.project_id);
  if (usedToday >= (project?.daily_token_budget ?? 0)) {
    console.warn(
      `summarizeSession: project ${session.project_id} over its daily token budget (${usedToday}/${project?.daily_token_budget}) — skipping session ${sessionId}`,
    );
    return { status: "skipped", reason: "budget_exceeded" };
  }

  const bucket = resolveBucket(project);
  const { texts, lastSeq } = await readSessionMainChunks(
    deps.adminClient,
    bucket,
    sessionId,
    session.summarized_through_seq !== null
      ? { afterSeq: session.summarized_through_seq }
      : {},
  );
  if (texts.length === 0) {
    return { status: "skipped", reason: "no_new_chunks" };
  }

  // CO-502: normalizes files_touched relative to the session's own
  // original_cwd, so the same repo file matches across every
  // developer's machine regardless of where they cloned it locally.
  const { text: condensedText, turns, filesTouched } = condenseSession(
    texts.join(""),
    undefined,
    session.original_cwd,
  );
  const systemPrompt = buildSystemPrompt();
  const userPrompt = buildUserPrompt(condensedText, session.summary);

  let apiResult: AnthropicResult;
  try {
    apiResult = await callWithRetry(deps, systemPrompt, userPrompt);
  } catch (e) {
    console.warn(
      `summarizeSession: Anthropic API call failed for session ${sessionId} after ${MAX_API_ATTEMPTS} attempts — leaving the existing summary in place`,
      e instanceof Error ? e.message : String(e),
    );
    return { status: "gave_up", reason: "api_failure" };
  }

  let parsed = parseResult(apiResult.text);
  if (!parsed) {
    // Retry once with a corrective prompt — a single extra call, not
    // another full backoff loop; a repeatedly-invalid response is far
    // more likely a prompt/schema problem than a transient one.
    const correctivePrompt =
      `${userPrompt}\n\nYour previous response was not valid JSON matching the required shape. Respond with ONLY the JSON object described above.`;
    try {
      const retryResult = await deps.callAnthropic(
        deps.anthropicApiKey,
        systemPrompt,
        correctivePrompt,
      );
      await logUsage(deps, session.project_id, sessionId, retryResult.usage);
      apiResult = {
        text: retryResult.text,
        usage: {
          inputTokens: apiResult.usage.inputTokens +
            retryResult.usage.inputTokens,
          outputTokens: apiResult.usage.outputTokens +
            retryResult.usage.outputTokens,
        },
      };
      parsed = parseResult(retryResult.text);
    } catch (e) {
      console.warn(
        `summarizeSession: retry-on-invalid-JSON call itself failed for session ${sessionId} — leaving the existing summary in place`,
        e instanceof Error ? e.message : String(e),
      );
      await logUsage(deps, session.project_id, sessionId, apiResult.usage);
      return { status: "gave_up", reason: "api_failure" };
    }
  }

  await logUsage(deps, session.project_id, sessionId, apiResult.usage);

  if (!parsed) {
    console.warn(
      `summarizeSession: session ${sessionId}'s response was still invalid after one retry — leaving the existing summary in place`,
    );
    return { status: "gave_up", reason: "invalid_response" };
  }

  // files_touched is the union of the session's own existing list
  // (earlier incremental runs), what the AI itself reported for this
  // run, and what condense-transcript (CO-406) mechanically extracted
  // from this run's own new chunks — the running list has to survive
  // across incremental calls since each call only ever sees its own
  // new chunks; the AI's own list honors the ticket's literal ask
  // (files_touched is part of the requested JSON shape); condense-
  // transcript's list is the deterministic floor that can never miss
  // a file the AI's own judgment happened to skip.
  const update: Record<string, unknown> = {
    summary: parsed.summary,
    outcome: parsed.outcome,
    blocked_reason: parsed.outcome === "blocked" ? parsed.blocked_reason : null,
    files_touched: [
      ...new Set([
        ...(session.files_touched ?? []),
        ...parsed.files_touched,
        ...filesTouched,
      ]),
    ].sort(),
    summarized_through_seq: lastSeq,
    // CO-408: the web app's "AI-generated, <relative time>" badge
    // reads this — distinct from summarized_through_seq (chunk/byte
    // progress, not a human-readable moment).
    summarized_at: deps.now().toISOString(),
  };
  // CO-405's own contract: a manual rename is never overwritten by
  // this job. title_source itself is left untouched either way — a
  // title this job writes stays attributable as 'ai' only because the
  // column's own default already is; this job never sets it to 'ai'
  // explicitly, since doing so would incorrectly flip a session that
  // somehow has title_source = 'manual' with no title (shouldn't
  // happen, but this guard costs nothing to keep correct either way).
  if (session.title_source !== "manual") {
    update.title = parsed.title;
  }

  // CO-501: computed BEFORE the sessions write and merged into the
  // same `update` object — one write, not two — specifically so a
  // failed embed call can never prevent the summary itself from being
  // written, and so this doesn't become a second, separate
  // `sessions.update()` call that would race or overwrite the first
  // in a caller only tracking "the last patch written." Best-effort:
  // caught and logged, never thrown — a slow or failed embed must
  // never turn an otherwise-successful summarization into a failed
  // one.
  try {
    update.embedding = await deps.embedText(
      [
        (update.title as string | undefined) ?? session.title ?? "",
        parsed.summary,
        (update.files_touched as string[]).join(" "),
      ].join("\n"),
    );
  } catch (e) {
    console.error(
      `summarizeSession: session embedding failed for session ${sessionId}`,
      e instanceof Error ? e.message : String(e),
    );
  }

  const { error: updateErr } = await deps.adminClient
    .from("sessions")
    .update(update)
    .eq("id", sessionId);
  if (updateErr) throw updateErr;

  // Passages go to their own table (session_passages), so unlike the
  // session-level embedding above this is a genuinely separate write
  // — turns are append-only (I2) once Claude Code writes them, so
  // there's nothing to merge into the sessions update the way the
  // session-level embedding is. Best-effort, same reasoning.
  await embedNewPassages(deps, sessionId, turns);

  // CO-409: notify Slack only on the transition INTO 'blocked', not
  // on every subsequent sweep call that still finds the session
  // blocked (the 30-minute cron sweep, or a later re-summarize,
  // would otherwise re-notify the same blocker repeatedly). A missing
  // project_notification_settings row (no project_id match at all)
  // means notifications are off for this project — not an error.
  if (parsed.outcome === "blocked" && session.outcome !== "blocked") {
    const title = (update.title as string | undefined) ?? session.title ??
      "a session";
    await notifyBlockedSession(
      deps,
      session,
      sessionId,
      title,
      parsed.blocked_reason!,
    );
  }

  return { status: "summarized" };
}

/** CO-501: embeds this run's own new turns and appends them to
 * session_passages, keyed by a running per-session seq (the existing-
 * passage count at the start of this call) so incremental runs never
 * collide with or overwrite an earlier run's passages — turns are
 * append-only (I2) once Claude Code writes them, unlike the session-
 * level embedding (which genuinely gets replaced every run, merged
 * into the main sessions.update() call above instead). Every failure
 * here is logged and swallowed, same reasoning as notifyBlockedSession
 * below: a slow or failed embed must never turn a successful
 * summarization into a failed one. */
async function embedNewPassages(
  deps: SummarizeDeps,
  sessionId: string,
  turns: string[],
): Promise<void> {
  if (turns.length === 0) return;
  try {
    const { count, error: countErr } = await deps.adminClient
      .from("session_passages")
      .select("*", { count: "exact", head: true })
      .eq("session_id", sessionId);
    if (countErr) {
      console.error(
        "summarizeSession: passage count lookup failed",
        countErr.code,
      );
      return;
    }

    let seq = count ?? 0;
    const rows: {
      session_id: string;
      seq: number;
      text: string;
      embedding: number[];
    }[] = [];
    for (const turn of turns) {
      const embedding = await deps.embedText(turn);
      rows.push({ session_id: sessionId, seq: seq++, text: turn, embedding });
    }
    const { error: passagesErr } = await deps.adminClient
      .from("session_passages")
      .upsert(rows, { onConflict: "session_id,seq" });
    if (passagesErr) {
      console.error(
        "summarizeSession: passage upsert failed",
        passagesErr.code,
      );
    }
  } catch (e) {
    console.error(
      `summarizeSession: passage embedding failed for session ${sessionId}`,
      e instanceof Error ? e.message : String(e),
    );
  }
}

/** Looks up the project's own Slack config and the session's owner's
 * GitHub login, then sends the blocked-session alert — logged and
 * swallowed on any failure (a notification is never allowed to turn
 * a successful summarization into a failed one). */
async function notifyBlockedSession(
  deps: SummarizeDeps,
  session: { project_id: string; user_id: string },
  sessionId: string,
  sessionTitle: string,
  blockedReason: string,
): Promise<void> {
  try {
    const { data: settings, error: settingsErr } = await deps.adminClient
      .from("project_notification_settings")
      .select("slack_webhook_url")
      .eq("project_id", session.project_id)
      .maybeSingle();
    if (settingsErr) {
      console.error(
        "summarizeSession: notification settings lookup failed",
        settingsErr.code,
      );
      return;
    }
    const webhookUrl = settings?.slack_webhook_url;
    if (!webhookUrl) return;

    const { data: owner, error: ownerErr } = await deps.adminClient.auth.admin
      .getUserById(session.user_id);
    if (ownerErr) {
      console.error(
        "summarizeSession: session owner lookup failed for notification",
        ownerErr,
      );
    }
    const developerLogin = owner?.user?.user_metadata?.user_name ?? null;

    const message = buildBlockedSessionMessage({
      sessionTitle,
      developerLogin,
      blockedReason,
      sessionUrl: buildSessionUrl(sessionId),
    });
    const sendSlack = deps.postToSlack ?? realPostToSlackWithRetry;
    await sendSlack(webhookUrl, message);
  } catch (e) {
    console.error(
      `summarizeSession: blocked-session notification failed for session ${sessionId}`,
      e instanceof Error ? e.message : String(e),
    );
  }
}

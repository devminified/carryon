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
  const { data: session, error: sessionErr } = await deps.adminClient
    .from("sessions")
    .select(
      "id, project_id, status, title, title_source, summary, summarized_through_seq, files_touched",
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

  const { text: condensedText, filesTouched } = condenseSession(texts.join(""));
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

  const { error: updateErr } = await deps.adminClient
    .from("sessions")
    .update(update)
    .eq("id", sessionId);
  if (updateErr) throw updateErr;

  return { status: "summarized" };
}

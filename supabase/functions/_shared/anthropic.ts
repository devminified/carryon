// CO-407: the one real third-party AI provider call in this codebase
// (edge-functions.md's own "summarize-session and the Anthropic API"
// section) — Claude Haiku, per CLAUDE.md's locked stack. Receives
// already-redacted, already-condensed text only (_shared/condense-
// transcript.ts, CO-406) — never a raw transcript line.

const ANTHROPIC_API_URL = "https://api.anthropic.com/v1/messages";
const ANTHROPIC_VERSION = "2023-06-01";

// Default to the latest and most capable Haiku model, per this
// project's own "Anthropic API (Claude Haiku) for titles/summaries"
// line in the tech stack table.
export const MODEL = "claude-haiku-4-5-20251001";

const MAX_RESPONSE_TOKENS = 1024;

export class AnthropicAPIError extends Error {
  constructor(public status: number, message: string) {
    super(message);
    this.name = "AnthropicAPIError";
  }
}

export interface AnthropicUsage {
  inputTokens: number;
  outputTokens: number;
}

export interface AnthropicResult {
  text: string;
  usage: AnthropicUsage;
}

/** One real Messages API call — no retry logic here (that's
 * _shared/summarize.ts's job, since what counts as "retry-worthy"
 * differs between a transport failure and an invalid-JSON response).
 * Throws AnthropicAPIError on any non-2xx response; never logs the
 * API key or the request/response body (structured logs only,
 * edge-functions.md's own rule — this file's callers log the session
 * ID and outcome, not this call's own text). */
export async function callAnthropic(
  apiKey: string,
  systemPrompt: string,
  userPrompt: string,
): Promise<AnthropicResult> {
  const res = await fetch(ANTHROPIC_API_URL, {
    method: "POST",
    headers: {
      "x-api-key": apiKey,
      "anthropic-version": ANTHROPIC_VERSION,
      "content-type": "application/json",
    },
    body: JSON.stringify({
      model: MODEL,
      max_tokens: MAX_RESPONSE_TOKENS,
      system: systemPrompt,
      messages: [{ role: "user", content: userPrompt }],
    }),
  });

  if (!res.ok) {
    // The body can carry a rate-limit or validation message worth
    // surfacing in a log line — but never assume it's safe to log
    // verbatim at the call site without redaction-equivalent care;
    // callers here only ever log res.status, never this text.
    const bodyText = await res.text().catch(() => "");
    throw new AnthropicAPIError(
      res.status,
      `Anthropic API responded ${res.status}: ${bodyText.slice(0, 500)}`,
    );
  }

  const data = await res.json();
  const text = Array.isArray(data.content)
    ? data.content
      .filter((b: { type?: string }) => b?.type === "text")
      .map((b: { text?: string }) => b.text ?? "")
      .join("")
    : "";

  return {
    text,
    usage: {
      inputTokens: typeof data.usage?.input_tokens === "number"
        ? data.usage.input_tokens
        : 0,
      outputTokens: typeof data.usage?.output_tokens === "number"
        ? data.usage.output_tokens
        : 0,
    },
  };
}

// Approximate, *not* authoritative — Anthropic's own pricing page is
// the source of truth and can change without this file being updated
// in lockstep. cost_usd (ai_usage, CO-407's migration) is logged for
// rough cost visibility only; the daily budget cap itself is enforced
// in real input+output *tokens*, not dollars, specifically so a
// pricing change can't silently widen or narrow what the cap actually
// limits.
const APPROX_INPUT_COST_PER_MTOK = 1.0;
const APPROX_OUTPUT_COST_PER_MTOK = 5.0;

export function estimateCostUSD(usage: AnthropicUsage): number {
  return (
    (usage.inputTokens / 1_000_000) * APPROX_INPUT_COST_PER_MTOK +
    (usage.outputTokens / 1_000_000) * APPROX_OUTPUT_COST_PER_MTOK
  );
}

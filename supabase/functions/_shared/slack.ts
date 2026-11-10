// CO-409: the one shared Slack-sending primitive in this codebase.
// summarize.ts (a session transitioning to outcome 'blocked'),
// rescan/index.ts (a secret-rescan hit) and send-daily-digest/index.ts
// (the opt-in daily per-developer digest) all build a message with
// one of the builders below and post it through postToSlackWithRetry
// — the same retry-with-backoff shape anthropic.ts's callWithRetry
// uses, but this one never rethrows: a Slack outage must never break
// the summary job, the rescan sweep, or the digest run (this ticket's
// own "a failed webhook is retried and logged without breaking the
// summary job" testing point). Every failure is caught, retried up to
// MAX_ATTEMPTS times, then just logged — callers get a boolean back,
// never an exception.
//
// Messages are built as Slack Block Kit blocks (this ticket's own
// "each message type renders correctly in the Slack Block Kit
// preview" testing point) with a plain-text `text` fallback field
// (Slack's own recommendation for notifications/screen readers).
// Never transcript content — title, summary text already generated
// by summarize-session, and a link only (notifications.md's own
// rule); each caller is responsible for not handing this module
// anything more sensitive than that.

export interface SlackBlock {
  type: string;
  text?: { type: "mrkdwn" | "plain_text"; text: string };
}

export interface SlackPayload {
  text: string;
  blocks: SlackBlock[];
}

const MAX_ATTEMPTS = 3;
const BASE_BACKOFF_MS = 500;

function mrkdwn(text: string): SlackBlock {
  return { type: "section", text: { type: "mrkdwn", text } };
}

/** One real POST to a Slack incoming webhook — throws on a non-2xx
 * response or a network-level failure. No retry logic here, same
 * split anthropic.ts's callAnthropic/callWithRetry already uses
 * between "one call" and "the retry loop". */
async function postOnce(
  webhookUrl: string,
  payload: SlackPayload,
): Promise<void> {
  const res = await fetch(webhookUrl, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (!res.ok) {
    const bodyText = await res.text().catch(() => "");
    throw new Error(
      `Slack webhook responded ${res.status}: ${bodyText.slice(0, 300)}`,
    );
  }
}

/** Real sleep — the one real call site (every caller's default
 * deps) uses this; tests inject a no-op instead, same pattern
 * summarize.ts's realSleep already establishes. */
export async function realSleep(ms: number): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, ms));
}

export interface PostToSlackDeps {
  post: (webhookUrl: string, payload: SlackPayload) => Promise<void>;
  sleep: (ms: number) => Promise<void>;
}

const defaultDeps: PostToSlackDeps = { post: postOnce, sleep: realSleep };

/** Retries a failed Slack post with exponential backoff, then gives
 * up and logs a structured warning — this function itself never
 * throws. Returns true on success, false once every attempt has
 * failed, so a caller can log its own context if it wants to, but
 * is never forced to handle an exception just to stay safe. */
export async function postToSlackWithRetry(
  webhookUrl: string,
  payload: SlackPayload,
  deps: PostToSlackDeps = defaultDeps,
): Promise<boolean> {
  let lastErr: unknown;
  for (let attempt = 0; attempt < MAX_ATTEMPTS; attempt++) {
    try {
      await deps.post(webhookUrl, payload);
      return true;
    } catch (e) {
      lastErr = e;
      if (attempt < MAX_ATTEMPTS - 1) {
        await deps.sleep(BASE_BACKOFF_MS * 2 ** attempt);
      }
    }
  }
  console.error(
    "postToSlackWithRetry: giving up after retries",
    lastErr instanceof Error ? lastErr.message : String(lastErr),
  );
  return false;
}

/** Builds a session's web-viewer URL from WEB_APP_URL (an Edge
 * Function secret, see .env.example) — null when it isn't configured,
 * so every message builder below degrades to plain text instead of a
 * broken or locally-hosted link. */
export function buildSessionUrl(sessionId: string): string | null {
  const base = Deno.env.get("WEB_APP_URL");
  if (!base) return null;
  return `${base.replace(/\/+$/, "")}/sessions/${sessionId}`;
}

export function buildBlockedSessionMessage(input: {
  sessionTitle: string;
  developerLogin: string | null;
  blockedReason: string;
  sessionUrl: string | null;
}): SlackPayload {
  const who = input.developerLogin ? `${input.developerLogin}'s` : "A";
  const linkedTitle = input.sessionUrl
    ? `<${input.sessionUrl}|${input.sessionTitle}>`
    : input.sessionTitle;
  const text =
    `${who} session "${input.sessionTitle}" is blocked: ${input.blockedReason}`;
  const mrkdwnText =
    `${who} session *${linkedTitle}* is blocked: ${input.blockedReason}`;
  return { text, blocks: [mrkdwn(mrkdwnText)] };
}

export function buildRescanHitMessage(input: {
  sessionUrl: string | null;
}): SlackPayload {
  const text =
    "Secret rescan found a possible secret in a session chunk — rotate the affected credential.";
  const link = input.sessionUrl ? ` <${input.sessionUrl}|View session>` : "";
  return { text, blocks: [mrkdwn(`⚠️ ${text}${link}`)] };
}

// CO-503: this is Carryon's own team notification (the nightly
// workflow's own run, not a per-project event) — never routed through
// project_notification_settings' own webhook lookup, a separate
// COMPAT_CI_SLACK_WEBHOOK_URL secret instead (see record-compat-run's
// own doc comment).
export function buildCompatCIFailureMessage(input: {
  carryonVersion: string;
  claudeCodeVersion: string;
  versionLabel: "pinned" | "latest";
  os: string;
  failureReason: string | null;
  runUrl: string | null;
}): SlackPayload {
  const text =
    `Claude Code compatibility CI failed: ${input.carryonVersion} vs. ${input.versionLabel} Claude Code ${input.claudeCodeVersion} on ${input.os}`;
  const reason = input.failureReason ? `\n${input.failureReason}` : "";
  const link = input.runUrl ? ` <${input.runUrl}|View run>` : "";
  return { text, blocks: [mrkdwn(`🔴 ${text}${reason}${link}`)] };
}

export interface DigestEntry {
  developerLogin: string;
  sessionCount: number;
}

export function buildDailyDigestMessage(input: {
  projectName: string;
  entries: DigestEntry[];
}): SlackPayload {
  const lines = input.entries
    .map((e) =>
      `• ${e.developerLogin}: ${e.sessionCount} session${
        e.sessionCount === 1 ? "" : "s"
      }`
    )
    .join("\n");
  const text = `Daily digest for ${input.projectName}`;
  return {
    text,
    blocks: [
      {
        type: "header",
        text: {
          type: "plain_text",
          text: `Daily digest — ${input.projectName}`,
        },
      },
      mrkdwn(lines || "No session activity in the last 24 hours."),
    ],
  };
}

// Follow-up to CO-409 (GitHub issue #113): Mattermost's own
// incoming-webhook sending primitive, parallel to slack.ts rather than
// a shared abstraction over both — verified against a real local
// Mattermost instance (mattermost/mattermost-preview) that the two
// platforms are NOT payload-compatible the way the issue's own design
// sketch worried they might not be:
//
//   - Mattermost's webhook endpoint silently ignores Slack's Block Kit
//     `blocks` array entirely — a message sent with slack.ts's exact
//     payload shape renders as nothing but the plain `text` fallback
//     field, losing every bit of the real formatting (bold, the
//     clickable link). Confirmed by posting that exact payload and
//     reading the stored post back via the Mattermost REST API.
//   - Mattermost DOES understand Slack's older "attachments" shape
//     (`{text, color}` — rendered server-side as `type:
//     "slack_attachment"`), so the richer formatting isn't lost, it
//     just needs a second, Mattermost-native rendering built from the
//     same underlying data each caller already has, using
//     Mattermost's own Markdown dialect (`**bold**`, `[text](url)`
//     links) rather than Slack's legacy mrkdwn (`*bold*`, `<url|text>`)
//     slack.ts's own builders use — confirmed the same way, by posting
//     both link syntaxes and reading back which one Mattermost's API
//     stores/expects.
//
// Keeping this as its own file (not a shared chat.ts) is deliberate:
// the two platforms' payload shapes and markup dialects genuinely
// differ, and at this codebase's real scale (three message types, two
// channels) forcing a shared abstraction over that difference would
// be premature (CLAUDE.md's "wait for the third occurrence" — there
// is no third chat platform in scope today). Every call site fans out
// to Slack and Mattermost independently — see summarize.ts,
// rescan/index.ts, send-daily-digest/index.ts — so a failure on one
// channel never blocks the other, same reasoning slack.ts's own
// postToSlackWithRetry already documents.

export interface MattermostAttachment {
  text: string;
  color?: string;
}

export interface MattermostPayload {
  text: string;
  attachments?: MattermostAttachment[];
}

const MAX_ATTEMPTS = 3;
const BASE_BACKOFF_MS = 500;

/** One real POST to a Mattermost incoming webhook — throws on a
 * non-2xx response or a network-level failure. Same split as
 * slack.ts's postOnce/postToSlackWithRetry. */
async function postOnce(
  webhookUrl: string,
  payload: MattermostPayload,
): Promise<void> {
  const res = await fetch(webhookUrl, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (!res.ok) {
    const bodyText = await res.text().catch(() => "");
    throw new Error(
      `Mattermost webhook responded ${res.status}: ${bodyText.slice(0, 300)}`,
    );
  }
}

async function realSleep(ms: number): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, ms));
}

export interface PostToMattermostDeps {
  post: (webhookUrl: string, payload: MattermostPayload) => Promise<void>;
  sleep: (ms: number) => Promise<void>;
}

const defaultDeps: PostToMattermostDeps = { post: postOnce, sleep: realSleep };

/** Retries a failed Mattermost post with exponential backoff, then
 * gives up and logs a structured warning — never throws. Returns
 * true on success, false once every attempt has failed. Identical
 * shape to slack.ts's postToSlackWithRetry on purpose, so a caller
 * fanning out to both channels can treat them the same way. */
export async function postToMattermostWithRetry(
  webhookUrl: string,
  payload: MattermostPayload,
  deps: PostToMattermostDeps = defaultDeps,
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
    "postToMattermostWithRetry: giving up after retries",
    lastErr instanceof Error ? lastErr.message : String(lastErr),
  );
  return false;
}

const COLOR_WARNING = "#cc8400";
const COLOR_ALERT = "#cc0000";

export function buildBlockedSessionMessage(input: {
  sessionTitle: string;
  developerLogin: string | null;
  blockedReason: string;
  sessionUrl: string | null;
}): MattermostPayload {
  const who = input.developerLogin ? `${input.developerLogin}'s` : "A";
  const linkedTitle = input.sessionUrl
    ? `[${input.sessionTitle}](${input.sessionUrl})`
    : input.sessionTitle;
  const text =
    `${who} session "${input.sessionTitle}" is blocked: ${input.blockedReason}`;
  const body =
    `${who} session **${linkedTitle}** is blocked: ${input.blockedReason}`;
  return { text, attachments: [{ text: body, color: COLOR_WARNING }] };
}

export function buildRescanHitMessage(input: {
  sessionUrl: string | null;
}): MattermostPayload {
  const text =
    "Secret rescan found a possible secret in a session chunk — rotate the affected credential.";
  const link = input.sessionUrl ? ` [View session](${input.sessionUrl})` : "";
  return {
    text,
    attachments: [{ text: `⚠️ ${text}${link}`, color: COLOR_ALERT }],
  };
}

export interface DigestEntry {
  developerLogin: string;
  sessionCount: number;
}

export function buildDailyDigestMessage(input: {
  projectName: string;
  entries: DigestEntry[];
}): MattermostPayload {
  const lines = input.entries
    .map((e) =>
      `* ${e.developerLogin}: ${e.sessionCount} session${
        e.sessionCount === 1 ? "" : "s"
      }`
    )
    .join("\n");
  const text = `Daily digest for ${input.projectName}`;
  const body = `**Daily digest — ${input.projectName}**\n${
    lines || "No session activity in the last 24 hours."
  }`;
  return { text, attachments: [{ text: body }] };
}

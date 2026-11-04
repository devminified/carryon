# Notifications

Carryon's primary notification channel is **Slack** (CO-409), not email — there's no payroll-style transactional email need. Supabase Auth's built-in email (magic link, if ever enabled) is the only auth-related email; everything else goes through the `notify` queue.

## Slack — the `notify` queue

`confirm-chunk`/`end-session`/`summarize-session` write an outbox row; the `notify` queue worker posts to Slack (title + link to the web viewer) when a session ends with a fresh AI summary, or when an admin deletes a session with an audit reason. Coalesced by event, so a flurry of chunk confirmations doesn't spam Slack — only the terminal event (session end) triggers a message. See `.claude/rules/edge-functions.md` for the outbox pattern.

```ts
// supabase/functions/_shared/slack.ts
import { z } from 'npm:zod';

const NotifyInput = z.object({
  sessionId: z.string().uuid(),
  title: z.string(),
  summary: z.string().max(500),
  url: z.string().url(),
});

export async function postSessionSummary(input: z.infer<typeof NotifyInput>) {
  const parsed = NotifyInput.parse(input);
  await fetch(Deno.env.get('SLACK_WEBHOOK_URL')!, {
    method: 'POST',
    body: JSON.stringify({ text: `*${parsed.title}* ended — ${parsed.summary}\n${parsed.url}` }),
  });
}
```

Never post transcript content to Slack — title, AI summary, and a link only. The summary itself is already AI-generated from redacted, condensed text (see `edge-functions.md`'s note on `summarize-session`), but Slack messages are the most widely-visible surface in the whole system, so treat them as the strictest filter, not a pass-through.

## Fallback email/SMS

If a future need arises (e.g. a digest email, an urgent security alert), follow the directory default: Supabase Edge Functions + Resend for email, Twilio via Edge Function for SMS. Not needed for anything currently in scope — don't add either until a ticket actually calls for it.

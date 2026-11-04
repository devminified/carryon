# Notifications

Carryon's primary notification channel is **Slack** (CO-409), not email — there's no payroll-style transactional email need. Supabase Auth's built-in email (magic link, if ever enabled) is the only auth-related email; everything else is Slack, sent directly by the Edge Function that caused the event.

## Slack (CO-409)

Per Doc: `summarize-session` (CO-407) calls the Slack webhook directly when a session it just summarized has outcome `blocked`, naming the session and linking to the web viewer. Coalesced naturally — only the terminal summarize call for a session triggers a message, not every chunk confirmation. Other message types (daily digest, opt-in; secret-rescan hits from CO-306 to admins) are likewise sent directly by the function that produces them. See `.claude/rules/edge-functions.md` for why there's no queue/outbox here in the current build plan.

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

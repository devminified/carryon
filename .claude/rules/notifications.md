# Notifications

Carryon's primary notification channel is **Slack** (CO-409), not email — there's no payroll-style transactional email need. Supabase Auth's built-in email (magic link, if ever enabled) is the only auth-related email; everything else is Slack, sent directly by the Edge Function that caused the event.

## Slack (CO-409, as actually built)

Per-project, optional, opt-in config lives in its own `project_notification_settings` table (`project_id`, `slack_webhook_url`, `notify_digest`) — **not** new columns on `projects`, unlike `bucket`/`daily_token_budget`. Those are readable by any viewer (`members_can_read_their_projects`); a Slack webhook URL lets its holder *post* into the team's channel, which is a materially different risk, so this table's own RLS is admin-only in every direction (select/insert/update/delete) — see `database-rls.md` and the migration's own comment. A missing row means "notifications off" for that project, not an error — every reader here treats `maybeSingle()` coming back null that way. There's no admin UI for setting this yet (CO-410's own planned page); until then it's set by hand via SQL, same manual-tuning shape `bucket`/`daily_token_budget`/`closed_at` already have.

Three message types, each built by its own function in `supabase/functions/_shared/slack.ts` and sent through that file's `postToSlackWithRetry` (exponential backoff, `MAX_ATTEMPTS = 3`, never throws — a Slack outage must never break the job that triggered it):

- **Blocked-session alert** — `_shared/summarize.ts`'s own `summarizeSession()` sends this itself, right after a successful write, but *only* on the transition **into** `outcome = 'blocked'` (the session's previous `outcome` wasn't already `'blocked'`) — otherwise the 30-minute cron sweep (or a CO-408 re-summarize) would re-send the same alert every time it still finds the session blocked. Named by the session's owner's GitHub login (`auth.admin.getUserById`, same lookup `list-sessions` already uses), the session's title, the blocked reason, and a link built from `WEB_APP_URL` (an Edge Function secret — see `.env.example`; the message degrades to no link, not a broken one, if it's unset).
- **Secret-rescan hit** — `rescan/index.ts` sends one message per hit, right after its own `audit_log` insert, reusing the same project's `project_notification_settings` lookup it already does for `bucket`. Never includes the matched secret value — only "rotate the affected credential" and a session link.
- **Daily digest (opt-in)** — a fourth scheduled Edge Function, `send-daily-digest`, same `x-cron-secret` + `pg_cron`/`net.http_post`/Vault shape as `rescan`/`sync-github-teams`/`purge-expired-sessions`/`summarize-session` (`edge-functions.md`), running once daily. Only for a project with `notify_digest = true` **and** a configured webhook — opt-in on top of the webhook, since a team that only wants blocked-session alerts shouldn't get a daily message it never asked for. Groups the last 24 hours' sessions by developer (resolved the same way as the blocked alert); a project with zero activity in the window sends nothing at all, same reasoning as "projects with notifications off send nothing" — an empty digest every day trains a team to ignore the channel.

```ts
// supabase/functions/_shared/slack.ts (shape, not the full file)
export async function postToSlackWithRetry(
  webhookUrl: string,
  payload: { text: string; blocks: unknown[] }, // Slack Block Kit
  deps = defaultDeps, // real fetch + sleep; tests inject stubs
): Promise<boolean> { /* retries, then logs and returns false — never throws */ }
```

Never post transcript content to Slack — title, AI summary, and a link only. The summary itself is already AI-generated from redacted, condensed text (see `edge-functions.md`'s note on `summarize-session`), but Slack messages are the most widely-visible surface in the whole system, so treat them as the strictest filter, not a pass-through.

## Fallback email/SMS

If a future need arises (e.g. a digest email, an urgent security alert), follow the directory default: Supabase Edge Functions + Resend for email, Twilio via Edge Function for SMS. Not needed for anything currently in scope — don't add either until a ticket actually calls for it.

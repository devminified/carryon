-- CO-409: Slack notifications. A project's own, optional Slack
-- incoming-webhook config lives in its own table rather than as new
-- columns on `projects` — unlike `bucket`/`daily_token_budget`
-- (readable by any project viewer, database-rls.md), a webhook URL
-- lets its holder *post* into the team's Slack channel, not just read
-- something. Keeping it out of `projects` means the existing
-- "members_can_read_their_projects" policy (viewer+) never has to
-- widen to cover it — only an admin can read or change it, matching
-- CO-410's own planned admin page ("set retention, bucket and
-- Slack" — the admin-only page is this table's only future writer
-- besides a human running SQL by hand, same manual-tuning shape
-- bucket/daily_token_budget/closed_at already have today).
--
-- A missing row means "notifications off" for that project — there's
-- no need to backfill one per existing project; every reader here
-- treats `maybeSingle()` coming back null as "disabled", not an
-- error.
create table project_notification_settings (
  project_id uuid primary key references projects on delete cascade,
  slack_webhook_url text,
  -- The daily per-developer digest (notifications.md) is opt-in even
  -- once a webhook is configured — a project that only wants blocked-
  -- session alerts (CO-407's own trigger) shouldn't get a daily
  -- message it never asked for.
  notify_digest boolean not null default false
);

alter table project_notification_settings enable row level security;

-- api.auto_expose_new_tables is off (config.toml) — every new table
-- needs this explicit grant regardless of RLS, same as every other
-- table in this codebase (found the hard way for ai_usage, CO-407).
grant select, insert, update, delete on project_notification_settings to anon, authenticated;
grant select, insert, update, delete on project_notification_settings to service_role;

create policy "admins_can_read_project_notification_settings" on project_notification_settings
  for select to authenticated
  using (has_role(project_id, 'admin'));

create policy "admins_can_insert_project_notification_settings" on project_notification_settings
  for insert to authenticated
  with check (has_role(project_id, 'admin'));

create policy "admins_can_update_project_notification_settings" on project_notification_settings
  for update to authenticated
  using (has_role(project_id, 'admin'))
  with check (has_role(project_id, 'admin'));

create policy "admins_can_delete_project_notification_settings" on project_notification_settings
  for delete to authenticated
  using (has_role(project_id, 'admin'));

-- send-daily-digest (CO-409) is a fourth scheduled Edge Function, same
-- pg_cron + net.http_post + Vault shape as sync-github-teams/rescan/
-- purge-expired-sessions/summarize-session above it (edge-
-- functions.md's "scheduled Edge Functions" section) — a human has to
-- set this secret to match the function's own DIGEST_CRON_SECRET
-- (supabase secrets set DIGEST_CRON_SECRET=... for staging/
-- production; supabase/functions/.env locally) via:
--   select vault.update_secret(
--     (select id from vault.secrets where name = 'send_daily_digest_cron_secret'),
--     'the-same-value-as-DIGEST_CRON_SECRET'
--   );
select vault.create_secret(
  'REPLACE-ME-see-this-migrations-own-comment',
  'send_daily_digest_cron_secret'
);

select cron.schedule(
  'send-daily-digest',
  '0 13 * * *', -- once daily at 13:00 UTC — no time-of-day is specified by the ticket itself
  $$
  select net.http_post(
    url := 'http://kong:8000/functions/v1/send-daily-digest',
    headers := jsonb_build_object(
      'Content-Type', 'application/json',
      'x-cron-secret', (
        select decrypted_secret from vault.decrypted_secrets
        where name = 'send_daily_digest_cron_secret'
      )
    ),
    body := '{}'::jsonb
  );
  $$
);

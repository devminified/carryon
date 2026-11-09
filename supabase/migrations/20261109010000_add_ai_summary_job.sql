-- CO-407: AI title/summary job schema — the `summarize-session`
-- function this enables lives in supabase/functions/summarize-session,
-- its shared logic in supabase/functions/_shared/summarize.ts.

-- outcome's own third value ('blocked') already existed (CO-102);
-- blocked_reason is new — only ever set alongside outcome = 'blocked'.
alter table sessions add column blocked_reason text;

-- Tracks how far into the session's main stream the job has already
-- condensed and summarized (CO-406's own "incremental mode" — a later
-- run only reads chunks after this seq). Null means "never
-- summarized yet." Not reset by a manual rename (title_source =
-- 'manual') — a developer's own title doesn't mean the AI should stop
-- tracking progress on summary/outcome/files_touched, which it still
-- owns.
alter table sessions add column summarized_through_seq int;

-- Each project's own daily token budget (input + output tokens
-- combined, simpler than tracking two separate caps — CLAUDE.md: no
-- speculative scale) for the summary job. 200,000 tokens/day is a
-- deliberately generous *starting* default for a small team (reviewed
-- and tuned per project once real usage data exists, via UPDATE —
-- there's no admin UI for this yet, same manual-tuning shape as
-- projects.retention_days_after_close); the job's own warning log
-- when a project hits its cap is what surfaces that it needs
-- revisiting.
alter table projects add column daily_token_budget int not null default 200000;

-- Append-only usage log (I2) — one row per real Anthropic API call
-- the job makes, successful or not (a failed call's tokens still
-- count against the budget actually spent calling Anthropic, even
-- though no summary was written from it). cost_usd is informational
-- only, computed from a hardcoded, documented-as-approximate rate
-- (_shared/anthropic.ts) — the real token counts are what the daily
-- budget check actually sums, not this column.
create table ai_usage (
  id bigserial primary key,
  project_id uuid not null references projects on delete cascade,
  session_id uuid references sessions on delete set null,
  input_tokens int not null,
  output_tokens int not null,
  cost_usd numeric(10, 6) not null,
  model text not null,
  created_at timestamptz not null default now()
);

alter table ai_usage enable row level security;

-- PostgREST connects as anon/authenticated, and api.auto_expose_new_tables
-- is off (config.toml) — initial_schema.sql's own comment on why every
-- new table needs this GRANT explicitly, found the hard way for
-- redaction_config (CO-306) and now applied up front here instead of
-- waiting to rediscover it the same way. RLS is still what actually
-- limits a row to the right admin — this grant alone returns zero
-- rows, same reasoning as every other table.
grant select, insert, update, delete on ai_usage to anon, authenticated;
grant select, insert, update, delete on ai_usage to service_role;

create index ai_usage_project_created_idx on ai_usage (project_id, created_at);

-- Admins can see their own project's spend (a future admin page's own
-- concern, not built here); no insert/update/delete policy for
-- anyone — every write is the summarize-session job's own service
-- role, append-only.
create policy "admins_can_read_ai_usage" on ai_usage
  for select to authenticated
  using (has_role(project_id, 'admin'));

-- summarize-session (CO-407) is called by a pg_cron job via
-- net.http_post for its own 30-minute live-session sweep, the same
-- shape as sync-github-teams/rescan/purge-expired-sessions above it —
-- see those migrations' own comments for why Vault and a function
-- secret are two separate systems a human has to keep in sync by
-- hand, and edge-functions.md's "scheduled Edge Functions" section
-- for the full net.http_post wiring. This function's *other* trigger
-- (end-session) never goes through this cron path at all — see
-- config.toml's own comment on this function.
select vault.create_secret(
  'REPLACE-ME-see-this-migrations-own-comment',
  'summarize_session_cron_secret'
);

select cron.schedule(
  'summarize-session',
  '*/30 * * * *',
  $$
  select net.http_post(
    url := 'http://kong:8000/functions/v1/summarize-session',
    headers := jsonb_build_object(
      'Content-Type', 'application/json',
      'x-cron-secret', (
        select decrypted_secret from vault.decrypted_secrets
        where name = 'summarize_session_cron_secret'
      )
    ),
    body := '{}'::jsonb
  );
  $$
);

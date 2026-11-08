-- CO-303: each project maps to up to three GitHub teams (viewer/
-- resumer/admin), stored by their STABLE numeric team ID, never a
-- slug — the sync job always queries GitHub's /teams/{id}/members
-- endpoint, so a team rename never breaks the mapping (there's nothing
-- to "detect," since nothing here ever depended on the slug).
alter table projects add column team_viewer text;
alter table projects add column team_resumer text;
alter table projects add column team_admin text;

-- A project_members row is either hand-set by an admin ('manual') or
-- derived from GitHub team membership by the sync job ('sync'). The
-- sync job only ever writes/removes 'sync' rows — a manual override
-- survives every sync, by construction, because the job never touches
-- a row it didn't itself create.
alter table project_members add column source text not null default 'manual'
  check (source in ('manual', 'sync'));

-- sync-github-teams (CO-303) is called by a pg_cron job via net.http_post,
-- not a browser or the CLI — there's no caller JWT to check, so Kong's
-- JWT gate is disabled for this one function (supabase/config.toml's
-- functions.sync-github-teams.verify_jwt = false) and the function
-- authenticates the call itself against this shared secret instead
-- (the same "narrowly scoped role for queue workers" exception
-- edge-functions.md's own rules already carve out).
--
-- vault.create_secret's value here is a placeholder, not a real
-- secret — every environment (local dev, staging, production) needs a
-- human to replace it with a real random value that ALSO matches that
-- environment's CRON_SECRET function secret (`supabase secrets set
-- CRON_SECRET=<value>`, or supabase/functions/.env for local dev), via:
--
--   select vault.update_secret(
--     (select id from vault.secrets where name = 'sync_github_teams_cron_secret'),
--     new_secret := '<the same value as CRON_SECRET>'
--   );
--
-- This is the same "external, one-time setup" shape as the GitHub
-- OAuth App in auth.md — Vault and an Edge Function secret are two
-- separate systems with no automatic bridge between them, so a human
-- has to be the one who makes them agree.
select vault.create_secret(
  'REPLACE-ME-see-this-migrations-own-comment',
  'sync_github_teams_cron_secret'
);

select cron.schedule(
  'sync-github-teams',
  '*/15 * * * *',
  $$
  select net.http_post(
    url := 'http://kong:8000/functions/v1/sync-github-teams',
    headers := jsonb_build_object(
      'Content-Type', 'application/json',
      'x-cron-secret', (
        select decrypted_secret from vault.decrypted_secrets
        where name = 'sync_github_teams_cron_secret'
      )
    ),
    body := '{}'::jsonb
  );
  $$
);

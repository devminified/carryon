-- CO-307: retention and deletion.
--
-- closed_at is null for every active project — the retention sweep
-- (purge-expired-sessions) never touches those, by construction
-- (this ticket's own "active projects are untouched" testing point).
-- Setting closed_at is a manual, one-time admin action for now — the
-- same "external, one-time setup" shape as projects.bucket (CO-305)
-- and the GitHub team columns (CO-303): there's no web admin UI yet
-- (Phase 4) to drive this from, and this ticket doesn't ask for one.
-- A future admin UI/CLI action can set this column directly once it
-- exists; nothing about the sweep itself depends on how it got set.
alter table projects add column closed_at timestamptz;
alter table projects add column retention_days_after_close integer not null default 90;

-- The mandatory reason an admin gives for deleting a single session
-- (carryon admin delete <sid> --reason) — this ticket's own "kept in
-- the audit log as the only remaining record." Nullable because every
-- other action type that already writes audit_log (upload,
-- pull_manifest, pull_complete, role_change, rescan_hit) has no reason
-- of its own.
alter table audit_log add column reason text;

-- purge-expired-sessions is called by a pg_cron job via net.http_post,
-- same shape as sync-github-teams (CO-303) and rescan (CO-306) — no
-- caller JWT, authenticated via a shared x-cron-secret header instead
-- (config.toml's functions.purge-expired-sessions.verify_jwt = false).
--
-- vault.create_secret's value here is a placeholder — every
-- environment needs a human to replace it with a real random value
-- that ALSO matches that environment's PURGE_CRON_SECRET function
-- secret (`supabase secrets set PURGE_CRON_SECRET=<value>`, or
-- supabase/functions/.env for local dev), via:
--
--   select vault.update_secret(
--     (select id from vault.secrets where name = 'purge_expired_sessions_cron_secret'),
--     new_secret := '<the same value as PURGE_CRON_SECRET>'
--   );
select vault.create_secret(
  'REPLACE-ME-see-this-migrations-own-comment',
  'purge_expired_sessions_cron_secret'
);

select cron.schedule(
  'purge-expired-sessions',
  '0 4 * * *', -- nightly at 04:00 UTC
  $$
  select net.http_post(
    url := 'http://kong:8000/functions/v1/purge-expired-sessions',
    headers := jsonb_build_object(
      'Content-Type', 'application/json',
      'x-cron-secret', (
        select decrypted_secret from vault.decrypted_secrets
        where name = 'purge_expired_sessions_cron_secret'
      )
    ),
    body := '{}'::jsonb
  );
  $$
);

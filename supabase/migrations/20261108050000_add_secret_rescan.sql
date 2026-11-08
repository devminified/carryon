-- CO-306: server-side secret rescan — a backstop for anything that
-- slipped past the laptop's own redaction (I5) because the rule set
-- has since grown, or a secret planted directly in Storage, bypassing
-- the laptop entirely (this ticket's own testing point).
--
-- last_rescanned_at, together with redaction_config.updated_at (CO-301
-- — already has an updated_at column), is rescan's whole "what still
-- needs checking" predicate: a chunk needs a (re)scan whenever
-- last_rescanned_at is null, or is older than the last time the rule
-- set itself changed. That single predicate serves both the nightly
-- job (scoped further to chunks uploaded in the last 24 hours) and a
-- manual full rescan after a rule update (no time scope at all) —
-- see rescan/index.ts's own doc comment for why this needs no separate
-- cursor/offset to be resumable: a chunk this run already updated
-- drops out of every later run's WHERE clause by construction, so a
-- batch that times out partway through just leaves the remainder for
-- the next invocation (the next night's cron, or a retried manual
-- call) to pick up — nothing is lost, nothing is redone twice.
alter table session_chunks add column last_rescanned_at timestamptz;

-- CO-301's own redaction_config migration never granted service_role
-- access to it — the same 20261106002756_grant_service_role.sql gap its
-- own comment describes (CO-102's migration granted anon/authenticated
-- but missed service_role; BYPASSRLS exempts RLS checks but GRANT is a
-- separate, still-required privilege), just not caught until now
-- because get-config had never actually been exercised against a real
-- local instance end-to-end before this ticket's own real testing hit
-- `42501 insufficient_privilege` reading this table with the service
-- role. Fixed here rather than a separate migration since it's this
-- ticket's own real-world test that found it, and rescan needs this
-- same read.
grant select on redaction_config to service_role;

-- rescan is called by a pg_cron job via net.http_post, not a browser or
-- the CLI — same shape as sync-github-teams (CO-303): no caller JWT to
-- check, so Kong's JWT gate is disabled for this function
-- (supabase/config.toml's functions.rescan.verify_jwt = false) and the
-- function authenticates the call itself against this shared secret.
--
-- vault.create_secret's value here is a placeholder, not a real
-- secret — every environment needs a human to replace it with a real
-- random value that ALSO matches that environment's RESCAN_CRON_SECRET
-- function secret (`supabase secrets set RESCAN_CRON_SECRET=<value>`,
-- or supabase/functions/.env for local dev), via:
--
--   select vault.update_secret(
--     (select id from vault.secrets where name = 'rescan_cron_secret'),
--     new_secret := '<the same value as RESCAN_CRON_SECRET>'
--   );
--
-- Same "external, one-time setup" shape as CO-303's own GitHub App
-- secret — Vault and an Edge Function secret are two separate systems
-- with no automatic bridge, so a human has to be the one who makes
-- them agree. A manual full rescan (after a rule update) is triggered
-- the same way — a direct call to rescan carrying the same
-- x-cron-secret header and `{"full": true}` as the body — there's no
-- separate UI/endpoint for it.
select vault.create_secret(
  'REPLACE-ME-see-this-migrations-own-comment',
  'rescan_cron_secret'
);

select cron.schedule(
  'secret-rescan',
  '0 3 * * *', -- nightly at 03:00 UTC
  $$
  select net.http_post(
    url := 'http://kong:8000/functions/v1/rescan',
    headers := jsonb_build_object(
      'Content-Type', 'application/json',
      'x-cron-secret', (
        select decrypted_secret from vault.decrypted_secrets
        where name = 'rescan_cron_secret'
      )
    ),
    body := '{}'::jsonb
  );
  $$
);

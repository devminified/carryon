-- CO-506: belt-and-suspenders extension bootstrap for a self-hosted
-- stack. The real supabase/postgres image is expected to already
-- provision pg_net and pgsodium/Vault itself (same way Supabase's
-- own hosted platform and the local dev CLI's `supabase start` do —
-- neither is created by any migration under supabase/migrations/,
-- confirmed by grepping that whole directory: only pgcrypto, pg_cron
-- and vector have an explicit `create extension` there, because the
-- platform/CLI bootstrap normally handles the other two before a
-- single user migration ever runs). This file exists so a self-host
-- operator isn't silently relying on that undocumented-to-them
-- assumption holding for whatever supabase/postgres tag they pull —
-- every statement here is a no-op if the extension already exists.
create extension if not exists pgcrypto;
create extension if not exists pg_net;
create extension if not exists pgsodium;
-- The `vault` schema (vault.create_secret/vault.decrypted_secrets,
-- used by several migrations' own one-time cron-secret setup) is a
-- separate extension from pgsodium, not a side effect of it — a real
-- `docker compose up` failed with "schema \"vault\" does not exist"
-- until this was added explicitly.
create extension if not exists supabase_vault;
create extension if not exists pg_cron schema extensions;
create extension if not exists vector;

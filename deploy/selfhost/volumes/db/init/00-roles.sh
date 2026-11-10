#!/bin/sh
set -e

# CO-506: creates the roles this stack's own services actually need
# to log in as — found to be genuinely missing by a real
# `docker compose up`: supabase/postgres's own superuser is
# `supabase_admin` (its own entrypoint's POSTGRES_USER default,
# confirmed directly — not "postgres"), but PostgREST/Auth/Storage
# in this compose file connect as authenticator/supabase_auth_admin/
# supabase_storage_admin, none of which exist until something creates
# them. Every password here is $POSTGRES_PASSWORD, the one secret
# this stack already asks an operator for — simpler than minting a
# distinct password per role for no real security gain on a single-
# tenant self-hosted instance. auth/storage's own embedded migrators
# create their own schema's tables on first boot once they can log in
# and own their schema; this script only gets them that far.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
	-- GoTrue's own embedded migrations (not anything in this repo)
	-- hardcode "grant ... to postgres with grant option" — found by a
	-- real `docker compose up` failing auth's startup migration with
	-- "role \"postgres\" does not exist" otherwise. Made a real login
	-- superuser (not just a grant target) so this repo's own existing
	-- local-dev tooling that hardcodes a "postgres"/$POSTGRES_PASSWORD
	-- connection (supabase/seed.sql's own CI invocation, web/package.json's
	-- seed:e2e script) keeps working unmodified against this stack too —
	-- found genuinely necessary by a real CI run failing exactly that
	-- connection with "password authentication failed for user
	-- \"postgres\"" otherwise.
	create role postgres superuser login password '$POSTGRES_PASSWORD';

	create role anon nologin noinherit;
	create role authenticated nologin noinherit;
	create role service_role nologin noinherit bypassrls;

	create role authenticator noinherit login password '$POSTGRES_PASSWORD';
	grant anon to authenticator;
	grant authenticated to authenticator;
	grant service_role to authenticator;

	create role supabase_auth_admin noinherit createrole createdb login password '$POSTGRES_PASSWORD';
	create role supabase_storage_admin noinherit createrole createdb login password '$POSTGRES_PASSWORD';

	grant all privileges on database "$POSTGRES_DB" to supabase_auth_admin;
	grant all privileges on database "$POSTGRES_DB" to supabase_storage_admin;

	create schema if not exists auth authorization supabase_auth_admin;
	create schema if not exists storage authorization supabase_storage_admin;

	-- Realtime's own DB_AFTER_CONNECT_QUERY (docker-compose.yml) sets
	-- search_path to _realtime before it ever runs its own Ecto
	-- migrations — a real `docker compose up` failed with "no schema
	-- has been selected to create in" otherwise, since that schema
	-- never existed for search_path to resolve to.
	create schema if not exists _realtime;
	create schema if not exists realtime;

	-- This repo's own migrations ALTER this publication to add
	-- tables (CO-402's live session list) but never create it — on
	-- Supabase Cloud/the local CLI it already exists by the time any
	-- user migration runs; a real `docker compose up` failed with
	-- "publication \"supabase_realtime\" does not exist" otherwise.
	do \$\$
	begin
		if not exists (select 1 from pg_publication where pubname = 'supabase_realtime') then
			create publication supabase_realtime;
		end if;
	end \$\$;

	grant usage on schema public to anon, authenticated, service_role, authenticator, supabase_auth_admin, supabase_storage_admin;
	grant create on schema public to supabase_auth_admin, supabase_storage_admin;
EOSQL

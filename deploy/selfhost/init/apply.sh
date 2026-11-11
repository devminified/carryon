#!/usr/bin/env bash
set -euo pipefail

# CO-506: applies every migration under supabase/migrations/ against
# $DB_URL, in filename order, tracked in supabase_migrations.schema_
# migrations — the SAME schema/table name the real Supabase CLI uses
# (`supabase migration up` against a linked project), so a self-host
# operator who later runs CLI commands against this same database
# sees one consistent migration history, not two competing trackers.
#
# Safe to re-run on every `docker compose up` (not just the first):
# every migration file is itself idempotent (`create table if not
# exists`, `create or replace function`, etc. — database-rls.md's own
# append-only migration convention), and this script only ever
# applies a version it hasn't recorded yet, in order — nothing here
# ever drops data. That's what makes an upgrade from this stack's
# version N to N+1 (pull new images, more migration files land on
# disk, `docker compose up` again) preserve every existing row: the
# already-applied versions are skipped, the new ones apply on top of
# real existing data, exactly like a normal CLI-driven deploy.

MIGRATIONS_DIR="/workspace/supabase/migrations"

until pg_isready -d "$DB_URL" >/dev/null 2>&1; do
  echo "waiting for postgres..."
  sleep 1
done

psql "$DB_URL" -v ON_ERROR_STOP=1 -q <<'SQL'
create schema if not exists supabase_migrations;
create table if not exists supabase_migrations.schema_migrations (
  version text primary key,
  statements text[],
  name text
);
SQL

applied_count=0
skipped_count=0

for file in "$MIGRATIONS_DIR"/*.sql; do
  filename="$(basename "$file")"
  version="${filename%%_*}"

  already_applied=$(psql "$DB_URL" -tAc \
    "select exists(select 1 from supabase_migrations.schema_migrations where version = '$version')")

  if [ "$already_applied" = "t" ]; then
    skipped_count=$((skipped_count + 1))
    continue
  fi

  echo "applying $filename"
  psql "$DB_URL" -v ON_ERROR_STOP=1 -q -1 -f "$file"
  psql "$DB_URL" -v ON_ERROR_STOP=1 -q -c \
    "insert into supabase_migrations.schema_migrations (version, name) values ('$version', '$filename')"
  applied_count=$((applied_count + 1))
done

echo "migrations: $applied_count applied, $skipped_count already up to date"

# PostgREST (the `rest` service) starts as soon as Postgres is
# healthy — before this container ever runs, let alone finishes —
# and builds its own schema cache once at startup; it never notices
# tables this script adds afterward on its own. A real test (an
# actual POST, not just a health-check GET) failed with a bare 404
# against every real table until this was added: PostgREST's own
# "pgrst" LISTEN channel (built in since v10, no extra config needed)
# picks this up immediately, no restart required — and this is safe
# to send on every run, migrations applied or not, so a later
# upgrade's new migrations are picked up the same way without anyone
# needing to remember to restart `rest` by hand.
psql "$DB_URL" -v ON_ERROR_STOP=1 -q -c "select pg_notify('pgrst', 'reload schema');"

# Realtime, in this stack's own multi-tenant mode, refuses every
# subscription with "TenantNotFound" until a real row names it —
# found by a real Postgres Changes subscription hanging forever with
# that exact error in realtime's own logs. external_id 'realtime' is
# the value realtime itself requests by default (confirmed directly
# from that same error log, not guessed) — not something any client
# or Kong config here sets explicitly. Its own tables
# (_realtime.tenants/_realtime.extensions) don't exist until
# realtime's own embedded migrations create them on first boot,
# sometime after this container starts — hence the wait loop, same
# reasoning as the pg_isready wait above. Upserted, not
# insert-if-missing, so a rotated JWT_SECRET on an upgrade takes
# effect here too.
echo "waiting for realtime's own migrations..."
until psql "$DB_URL" -tAc "select to_regclass('_realtime.tenants') is not null" 2>/dev/null | grep -q t; do
  sleep 1
done
psql "$DB_URL" -v ON_ERROR_STOP=1 -q -c "
insert into _realtime.tenants (id, name, external_id, jwt_secret, max_concurrent_users, inserted_at, updated_at, max_events_per_second, postgres_cdc_default, max_bytes_per_second, max_channels_per_client, max_joins_per_second, suspend, private_only)
values (gen_random_uuid(), 'carryon', 'realtime', '$JWT_SECRET', 200, now(), now(), 100, 'postgres_cdc_rls', 100000, 100, 500, false, false)
on conflict (external_id) do update set jwt_secret = excluded.jwt_secret, updated_at = now();
"
psql "$DB_URL" -v ON_ERROR_STOP=1 -q -c "
insert into _realtime.extensions (id, type, settings, tenant_external_id, inserted_at, updated_at)
values (
  gen_random_uuid(), 'postgres_cdc_rls',
  jsonb_build_object(
    'region', 'us-east-1', 'db_host', 'db', 'db_port', '5432', 'db_name', 'postgres',
    'db_user', 'supabase_admin', 'db_password', '$POSTGRES_PASSWORD', 'db_ssl', 'false',
    'slot_name', 'supabase_realtime_replication_slot', 'publication', 'supabase_realtime',
    'poll_interval_ms', 100, 'poll_max_record_bytes', 1048576
  ),
  'realtime', now(), now()
)
on conflict (tenant_external_id, type) do update set settings = excluded.settings, updated_at = now();
"

# Deliberately no "deploy functions" step — the self-hosted
# edge-runtime (docker-compose.yml's `edge-functions` service) serves
# every function directly from the read-only mounted
# supabase/functions/ directory via its own main/index.ts router
# (see that file's own comment); there is nothing separate to deploy
# or register, unlike Supabase Cloud's `supabase functions deploy`.
echo "init complete"

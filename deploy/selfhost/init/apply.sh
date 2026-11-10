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

# Deliberately no "deploy functions" step — the self-hosted
# edge-runtime (docker-compose.yml's `edge-functions` service) serves
# every function directly from the read-only mounted
# supabase/functions/ directory via its own main/index.ts router
# (see that file's own comment); there is nothing separate to deploy
# or register, unlike Supabase Cloud's `supabase functions deploy`.
echo "init complete"

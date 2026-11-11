#!/usr/bin/env bash
set -euo pipefail

# CO-506: brings up the self-hosted stack from a fresh checkout of
# this directory. Run from deploy/selfhost/ itself:
#   ./install.sh
#
# What it does, in order: checks docker/docker compose are available,
# requires a filled-in .env (never generates one from thin air — a
# JWT_SECRET or cron secret this script invented and silently wrote
# would be exactly the kind of secret an operator doesn't know to
# rotate or back up), brings the stack up, waits for the web app's own
# /api/health to report healthy, and prints the two `carryon config
# set` commands (commands.md's own CLI, CO-506's new command) a
# developer runs once on their own laptop to point at this stack.

cd "$(dirname "$0")"

if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker is required — https://docs.docker.com/get-docker/" >&2
  exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo "error: docker compose (v2) is required" >&2
  exit 1
fi

if [ ! -f .env ]; then
  echo "error: .env not found. Copy .env.example to .env and fill in every value first:" >&2
  echo "  cp .env.example .env" >&2
  exit 1
fi

# shellcheck disable=SC1091
set -a
source .env
set +a

required_vars=(POSTGRES_PASSWORD JWT_SECRET ANON_KEY SERVICE_ROLE_KEY SUPABASE_PUBLIC_URL)
missing=()
for v in "${required_vars[@]}"; do
  if [ -z "${!v:-}" ]; then
    missing+=("$v")
  fi
done
if [ ${#missing[@]} -gt 0 ]; then
  echo "error: .env is missing required values: ${missing[*]}" >&2
  echo "see .env.example and README.md's \"Generating your keys\" step." >&2
  exit 1
fi

echo "Starting the self-hosted Carryon stack..."
docker compose up -d --build

echo "Waiting for migrations to finish applying..."
attempts=0
until docker compose logs init 2>&1 | grep -q "init complete"; do
  attempts=$((attempts + 1))
  if [ "$attempts" -gt 60 ]; then
    echo "error: migrations never completed. Check logs with:" >&2
    echo "  docker compose logs init" >&2
    exit 1
  fi
  sleep 5
done

# Realtime caches its tenant list once at its own boot, before the
# init container above has had a chance to seed the one tenant row
# this stack needs — found as a real, reproducible bug: every
# Postgres Changes subscription fails with "TenantNotFound" until
# Realtime is restarted after that row exists. A real self-host
# operator would hit this on a genuinely fresh `./install.sh` run
# every time, so this script does the restart for them rather than
# documenting it as a manual step to remember.
echo "Restarting realtime to pick up its seeded tenant..."
docker compose restart realtime

echo "Waiting for the web app to report healthy (this can take a few minutes on first boot)..."
attempts=0
until curl -fsS "${WEB_APP_URL:-http://127.0.0.1:3000}/api/health" >/dev/null 2>&1; do
  attempts=$((attempts + 1))
  if [ "$attempts" -gt 60 ]; then
    echo "error: the web app never became healthy. Check logs with:" >&2
    echo "  docker compose logs" >&2
    exit 1
  fi
  sleep 5
done

echo ""
echo "Carryon is up at ${SUPABASE_PUBLIC_URL} (API) and ${WEB_APP_URL:-http://127.0.0.1:3000} (web)."
echo ""
echo "On each developer's laptop, point the carryon binary at this stack:"
echo "  carryon config set api_url ${SUPABASE_PUBLIC_URL}"
echo "  carryon config set anon_key ${ANON_KEY}"

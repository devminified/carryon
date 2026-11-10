# Self-hosting Carryon

A `docker compose` stack that runs Carryon's own backend (Postgres, Auth,
REST, Realtime, Storage, Edge Functions, all real Supabase images — the
same self-hosting reference architecture Supabase itself publishes, not a
reimplementation) plus the Next.js web app, behind one Kong gateway. Point
the `carryon` laptop binary at it (`carryon config set api_url ...`) and
every laptop/CLI/web feature works exactly as it does against Supabase
Cloud — there is no separate self-host build.

**Who this is for:** a team that can't or won't put session transcripts
and code snapshots on Supabase Cloud, running Carryon on their own
infrastructure instead.

## Quick start

Requires Docker and Docker Compose v2, and roughly 4GB of free RAM for the
whole stack on a single host.

```bash
cd deploy/selfhost
cp .env.example .env
```

### Generating your keys

1. **`JWT_SECRET`** — any long random string, e.g.:
   ```bash
   openssl rand -base64 32
   ```
2. **`ANON_KEY`/`SERVICE_ROLE_KEY`** — JWTs signed with that same secret,
   roles `anon` and `service_role`. The simplest way to mint both without
   installing anything extra is Supabase's own public JWT generator at
   their self-hosting docs, pasting in your `JWT_SECRET`; or, with Node
   available locally:
   ```bash
   node -e "
   const crypto = require('crypto');
   const b64url = (buf) => buf.toString('base64').replace(/\+/g,'-').replace(/\//g,'_').replace(/=+$/,'');
   const sign = (role) => {
     const header = b64url(Buffer.from(JSON.stringify({alg:'HS256',typ:'JWT'})));
     const now = Math.floor(Date.now()/1000);
     const payload = b64url(Buffer.from(JSON.stringify({role, iss:'supabase', iat:now, exp:now+315360000})));
     const sig = b64url(crypto.createHmac('sha256', process.argv[1]).update(\`\${header}.\${payload}\`).digest());
     return \`\${header}.\${payload}.\${sig}\`;
   };
   console.log('ANON_KEY=' + sign('anon'));
   console.log('SERVICE_ROLE_KEY=' + sign('service_role'));
   " "$(grep ^JWT_SECRET= .env | cut -d= -f2)"
   ```
   **Must be a classic JWT, not a Supabase Cloud "publishable key"**
   (`sb_publishable_...`) — found the hard way: this stack's plain
   PostgREST/GoTrue images verify `ANON_KEY` as an HS256 JWT signed
   with `JWT_SECRET` above; Supabase Cloud's newer publishable/secret
   key format isn't a JWT at all and fails with a `JWSError` the
   instant anything calls `/rest/v1/*` through Kong. If you've ever
   copied a key from a real Supabase Cloud project's API settings
   page, it is almost certainly the wrong format here.
3. **Every other `*_SECRET`/`*_PASSWORD`/`*_KEY` in `.env.example`** —
   `openssl rand -base64 32` (or `-hex 24`/`-hex 20` for the cron secrets
   and `STORAGE_S3_ACCESS_KEY`/`STORAGE_S3_SECRET_KEY`) works for all of
   them — they're credentials for this stack's own internal services, not
   a real AWS account, so any random value is fine.

Fill in `SUPABASE_PUBLIC_URL` and `WEB_APP_URL` for your real deployment
(a public hostname behind your own TLS-terminating proxy, or
`http://127.0.0.1:...` for a local trial), then:

```bash
./install.sh
```

This brings the stack up, waits for the web app's own `/api/health` to
report healthy, applies every database migration automatically (the
`init` service — see below), and prints the two commands to run on each
developer's laptop:

```bash
carryon config set api_url <your SUPABASE_PUBLIC_URL>
carryon config set anon_key <your ANON_KEY>
carryon login
```

**Phase-gate target (qa-testing.md):** an engineer who didn't build
Carryon should be able to go from a fresh VM and this file alone to a
working stack in under two hours. If any step above takes longer than
expected, that's the thing to fix in this document, not a one-off you
work around.

## What's running

| Service | Image | Role |
| --- | --- | --- |
| `db` | `supabase/postgres` | Postgres + `vector`/`pg_cron`/`pg_net`/`pgsodium` (Vault) |
| `auth` | `supabase/gotrue` | Supabase Auth (GitHub OAuth) |
| `rest` | `postgrest/postgrest` | the REST API RLS enforces |
| `realtime` | `supabase/realtime` | Postgres Changes (the live session list) |
| `storage` | `supabase/storage-api` | signed-URL object storage, backed by `seaweedfs` |
| `seaweedfs`/`storage-setup` | `chrislusf/seaweedfs`, `amazon/aws-cli` | the default S3-compatible storage backend (see below for why not MinIO) |
| `edge-functions` | `supabase/edge-runtime` | every function under `supabase/functions/`, routed by `functions/main/` |
| `kong` | `kong` | the one API gateway (`kong.yml`) |
| `init` | built from `init/` | applies `supabase/migrations/*.sql`, once per `docker compose up` |
| `web` | built from `web.Dockerfile` | the Next.js app |

## Upgrading

Pull new images/code, then run `./install.sh` (or `docker compose up -d
--build`) again. `init` re-runs on every start but only applies
migrations it hasn't already recorded — nothing it does ever drops or
resets data, so an upgrade from this stack's version N to N+1 preserves
every existing project, session, and transcript. This is also what CI's
own self-host upgrade test (`.github/workflows/selfhost-e2e.yml`) checks
automatically: seed data, upgrade, assert nothing was lost.

## Why SeaweedFS, not MinIO

The default bundled S3-compatible backend is
[SeaweedFS](https://github.com/seaweedfs/seaweedfs) (Apache-2.0), run in
its single-process `server -s3` mode. Not MinIO: a real `docker compose
up` against `minio/minio`/`minio/mc` failed outright with "pull access
denied" — MinIO gated its own Docker Hub images behind a login wall as
part of its 2024/2025 relicensing (confirmed live; `quay.io/minio/*` 401s
the same way for every tag tried, so it isn't a one-registry fluke). If
MinIO's images become freely pullable again, switching back is a
docker-compose-only change — nothing in the rest of this stack assumes
SeaweedFS specifically, only an S3-compatible endpoint.

## Using a real S3 bucket instead

Create an override file (`docker-compose.override.yml`) that replaces the
`storage` service's `GLOBAL_S3_*`/`AWS_*` environment with your real
bucket's region/endpoint/credentials, and drop the `seaweedfs`/
`storage-setup` services
(`docker compose -f docker-compose.yml -f docker-compose.override.yml up -d`).
`storage-api` itself speaks the same S3 API either way.

## GitHub Enterprise Server

Set `GITHUB_ENTERPRISE_URL` (your GHES host, e.g.
`https://ghes.internal.example.com`) and `GITHUB_ENTERPRISE_API_URL`
(normally `<GITHUB_ENTERPRISE_URL>/api/v3`) in `.env`. This redirects:

- GoTrue's own OAuth authorize/token exchange for sign-in
  (`GOTRUE_EXTERNAL_GITHUB_URL`).
- `sync-github-teams`' team-membership reads
  (`GITHUB_API_BASE_URL`, `supabase/functions/_shared/github.ts`).

**One real limitation, found while wiring this up:** GoTrue's own
GitHub provider calls `api.github.com` directly for its post-OAuth
user/email lookup — that's hardcoded inside GoTrue itself, not something
this stack's env vars can redirect. This only matters if your GHES
instance's OAuth app doesn't use the standard flow (it does, by default);
if sign-in fails specifically after the GitHub authorize redirect, this
is the first thing to check, and the current workaround is a GoTrue
version bump if/when GoTrue itself adds a configurable API host for this
call — not something this stack can fix on its own today.

## AI summaries

Off by default (`AI_ENABLED=false` in `.env.example`). Set
`AI_ENABLED=true` and a real `ANTHROPIC_API_KEY` to turn them on — every
AI-related web UI element (the Re-summarize button, the AI-generated
summary badge, the admin dashboard's cost/latency tiles) appears only
once this is on; the backend already skips summarization end-to-end with
no key configured either way (`.claude/rules/edge-functions.md`).

## Health check

`GET /api/health` on the web app — checks the web app can reach its own
database, not just that the process is listening. `docker compose ps`
shows the same signal via the `web` service's own Docker healthcheck.

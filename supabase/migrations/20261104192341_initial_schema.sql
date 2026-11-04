-- CO-102: base schema. Six tables exactly as specified (Doc "Database
-- recommendation"), RLS enabled everywhere with deny-all defaults — no
-- policies yet, those are CO-302. `embedding` is deliberately deferred to
-- CO-501.
--
-- One deviation from the literal PRD SQL, documented here and in
-- .claude/rules/database-rls.md: `project_members.role` is a Postgres enum
-- (`project_role`) instead of `text check (...)`, so `role >= min_role` in
-- CO-302's `has_role()` sorts correctly (viewer < resumer < admin) — plain
-- text would sort 'admin' < 'resumer' < 'viewer', the opposite of intended.

create extension if not exists pgcrypto;

create type project_role as enum ('viewer', 'resumer', 'admin'); -- order is significant

create table projects (
  id uuid primary key default gen_random_uuid(),
  name text not null,
  git_remote text not null unique -- e.g. git@github.com:devminified/acme-api.git
);

alter table projects enable row level security;

create table project_members (
  project_id uuid not null references projects on delete cascade,
  user_id uuid not null references auth.users on delete cascade,
  role project_role not null,
  primary key (project_id, user_id)
);

alter table project_members enable row level security;

create table sessions (
  id uuid primary key, -- Claude Code's own session_id
  project_id uuid not null references projects,
  user_id uuid not null references auth.users,
  machine text,
  original_cwd text not null, -- needed to rewrite paths on pull
  branch text,
  head_sha text,
  claude_code_version text,
  title text, -- /rename name, else AI-written title
  status text not null default 'live' check (status in ('live', 'ended')),
  started_at timestamptz not null default now(),
  last_activity_at timestamptz not null default now(),
  total_bytes bigint not null default 0,
  parent_session_id uuid references sessions on delete set null, -- set when this session is a pulled fork; CO-307 keeps forks when the parent is deleted
  summary text, -- AI-written, see Doc "Session lifecycle and AI summaries"
  outcome text check (outcome in ('done', 'in_progress', 'blocked')),
  files_touched text[]
  -- embedding vector(…) — pgvector, CO-501. Not added until that ticket.
);

alter table sessions enable row level security;

create index sessions_project_last_activity_idx
  on sessions (project_id, last_activity_at desc);

create index sessions_user_id_idx
  on sessions (user_id);

create table session_chunks (
  session_id uuid not null references sessions on delete cascade,
  seq int not null,
  byte_start bigint not null,
  byte_end bigint not null,
  object_key text not null,
  sha256 text not null,
  uploaded_at timestamptz not null default now(),
  primary key (session_id, seq) -- also covers the "index on (session_id, seq)" requirement
);

alter table session_chunks enable row level security;

create table code_snapshots (
  session_id uuid not null references sessions on delete cascade,
  seq int not null,
  head_sha text not null,
  wip_ref text, -- refs/carryon/<user>/<session-id> on the git remote
  object_key text, -- git bundle fallback (CO-202)
  taken_at timestamptz not null default now(),
  primary key (session_id, seq)
);

alter table code_snapshots enable row level security;

create table audit_log (
  id bigserial primary key,
  actor uuid references auth.users,
  action text not null, -- vocabulary grows in CO-304; not constrained here
  session_id uuid,
  at timestamptz not null default now()
);

alter table audit_log enable row level security;

-- PostgREST connects as `anon`/`authenticated`, and this Supabase version no
-- longer auto-grants new public-schema tables to those roles (config.toml's
-- api.auto_expose_new_tables default). Grant baseline privileges explicitly
-- so RLS — not a missing GRANT — is what blocks access: with zero policies,
-- every query below still returns zero rows rather than a permission error.
-- CO-302 adds the real per-role policies on top of these grants.
grant select, insert, update, delete on
  projects, project_members, sessions, session_chunks, code_snapshots, audit_log
  to anon, authenticated;

-- Private Storage bucket for session chunks and code snapshots. No storage.objects
-- policies yet (CO-302/CO-305 add those) — public = false plus RLS already
-- enabled on storage.objects by default means both anon and authenticated
-- callers get zero access until then.
insert into storage.buckets (id, name, public)
values ('carryon-sessions', 'carryon-sessions', false);

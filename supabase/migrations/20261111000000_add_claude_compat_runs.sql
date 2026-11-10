-- CO-503: the nightly Claude Code compatibility CI's own results
-- table — append-only (I2), one row per (carryon_version,
-- claude_code_version, os) combination tested. `version_label`
-- ('pinned' | 'latest') is what the web app's own "can't move the pin
-- to a version without a green run" gate actually matches against —
-- the real resolved claude_code_version (e.g. "2.1.321") is kept
-- alongside it for the ticket's own "flagged with a specific breaking
-- change" validation point, not for the gate itself, since the gate
-- only cares "did the version we currently call pinned/latest pass,"
-- not which exact semver that happened to be on a given night.
create table claude_compat_runs (
  id bigserial primary key,
  carryon_version text not null,
  claude_code_version text not null,
  version_label text not null check (version_label in ('pinned', 'latest')),
  os text not null check (os in ('macos-latest', 'ubuntu-latest', 'windows-latest')),
  passed boolean not null,
  failure_reason text,
  -- A new session-file line type the nightly run doesn't recognize is
  -- reported here, never a failure on its own (the ticket's own
  -- explicit wording) — Carryon's own reader/viewer code may need an
  -- update, but a genuinely new Claude Code feature showing up as a
  -- new line type isn't itself a compatibility break.
  unknown_line_types text[] not null default '{}',
  ran_at timestamptz not null default now()
);

alter table claude_compat_runs enable row level security;

-- api.auto_expose_new_tables is off (config.toml) — same explicit
-- grant every new table in this codebase needs regardless of RLS.
grant select, insert, update, delete on claude_compat_runs to anon, authenticated;
grant select, insert, update, delete on claude_compat_runs to service_role;

create index claude_compat_runs_carryon_version_idx on claude_compat_runs (carryon_version);

-- Any global admin can read the compatibility history (a future admin
-- page's own natural data source) — is_any_project_admin(), same gate
-- the /admin console itself uses, not has_role() (this table has
-- nothing to do with any one project). No insert/update/delete policy
-- for anyone but the service role: the nightly workflow's own
-- record-compat-run Edge Function (authenticated by a shared CI
-- secret, not a user JWT) is the only writer, same append-only shape
-- as ai_usage.
create policy "any_project_admin_can_read_compat_runs" on claude_compat_runs
  for select to authenticated
  using (is_any_project_admin());

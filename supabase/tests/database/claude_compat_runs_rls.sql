-- CO-503: claude_compat_runs — any global admin (is_any_project_admin,
-- same gate the /admin console itself uses) can read the compatibility
-- history; a non-admin, and even a project-level admin of some OTHER
-- project, sees nothing; no insert policy for anyone but the service
-- role, since record-compat-run (authenticated by a shared CI secret,
-- not a user JWT) is the only writer.
begin;
select plan(5);

insert into auth.users (id, email) values
  ('d0000000-0000-0000-0000-000000000001', 'global-admin@test.local'),
  ('d0000000-0000-0000-0000-000000000002', 'plain-viewer@test.local');

insert into projects (id, name, git_remote) values
  ('d0000000-0000-0000-0000-00000000000a', 'proj-a', 'example.com/d/proj-a');

insert into project_members (project_id, user_id, role) values
  ('d0000000-0000-0000-0000-00000000000a', 'd0000000-0000-0000-0000-000000000001', 'admin'),
  ('d0000000-0000-0000-0000-00000000000a', 'd0000000-0000-0000-0000-000000000002', 'viewer');

insert into claude_compat_runs (carryon_version, claude_code_version, version_label, os, passed) values
  ('v0.4.0', '2.1.321', 'latest', 'ubuntu-latest', true),
  ('v0.4.0', '2.1.263', 'pinned', 'ubuntu-latest', false);

-- An admin of (any) project can read the fleet-wide compat run list.
set local role authenticated;
set local request.jwt.claim.sub = 'd0000000-0000-0000-0000-000000000001';
select results_eq(
  $$select count(*) from claude_compat_runs$$,
  $$values (2::bigint)$$, 'an admin of any project can read the fleet-wide compat run list'
);

-- A plain viewer (not an admin of anything) sees none of it.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'd0000000-0000-0000-0000-000000000002';
select results_eq(
  $$select count(*) from claude_compat_runs$$,
  $$values (0::bigint)$$, 'a non-admin sees zero rows, not a leaked count'
);

-- No insert policy exists for an authenticated caller, even an admin —
-- only the service role (via record-compat-run) ever writes this
-- table.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'd0000000-0000-0000-0000-000000000001';
select throws_ok(
  $$insert into claude_compat_runs (carryon_version, claude_code_version, version_label, os, passed) values ('v0.4.0', '2.1.321', 'latest', 'macos-latest', true)$$,
  '42501', null, 'an authenticated admin cannot insert a compat run row directly — no policy grants it'
);

-- Append-only: no update/delete policy for anyone either.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'd0000000-0000-0000-0000-000000000001';
select lives_ok(
  $$update claude_compat_runs set passed = true where carryon_version = 'v0.4.0' and version_label = 'pinned'$$,
  'an admin''s update runs without erroring'
);
select results_eq(
  $$select passed from claude_compat_runs where carryon_version = 'v0.4.0' and version_label = 'pinned'$$,
  $$values (false)$$, 'but it affected zero rows — compat runs are append-only, even for an admin'
);

select * from finish();
rollback;

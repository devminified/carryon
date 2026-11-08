-- CO-304: audit_log's real policies — admins read every row for their
-- project; a developer reads only rows about a session they own; never
-- update/delete for anyone, including admins (append-only, I2). No
-- insert policy (see the migration's own comment for why that's fine).
begin;
select plan(10);

insert into auth.users (id, email) values
  ('a8888888-8888-8888-8888-888888888801', 'owner@test.local'),
  ('a8888888-8888-8888-8888-888888888802', 'admin@test.local'),
  ('a8888888-8888-8888-8888-888888888803', 'outsider@test.local'),
  ('a8888888-8888-8888-8888-888888888804', 'puller@test.local');

insert into projects (id, name, git_remote) values
  ('a8888888-8888-8888-8888-88888888880a', 'proj', 'example.com/a/proj');

insert into project_members (project_id, user_id, role) values
  ('a8888888-8888-8888-8888-88888888880a', 'a8888888-8888-8888-8888-888888888801', 'resumer'),
  ('a8888888-8888-8888-8888-88888888880a', 'a8888888-8888-8888-8888-888888888802', 'admin'),
  ('a8888888-8888-8888-8888-88888888880a', 'a8888888-8888-8888-8888-888888888804', 'resumer');

insert into sessions (id, project_id, user_id, original_cwd) values
  ('a8888888-8888-8888-8888-88888888880b', 'a8888888-8888-8888-8888-88888888880a', 'a8888888-8888-8888-8888-888888888801', '/home/x');

-- A session-scoped row (the owner's own session got pulled) and a
-- project-scoped, session-less row (a role change) — only the admin
-- should ever see the second one.
insert into audit_log (action, actor, session_id, project_id) values
  ('pull_manifest', 'a8888888-8888-8888-8888-888888888804', 'a8888888-8888-8888-8888-88888888880b', 'a8888888-8888-8888-8888-88888888880a');
insert into audit_log (action, project_id) values
  ('role_change', 'a8888888-8888-8888-8888-88888888880a');

-- The project's admin sees both rows.
set local role authenticated;
set local request.jwt.claim.sub = 'a8888888-8888-8888-8888-888888888802';
select results_eq(
  $$select count(*) from audit_log where project_id = 'a8888888-8888-8888-8888-88888888880a'$$,
  $$values (2::bigint)$$, 'an admin sees every audit row for their project'
);

-- The session's owner sees only the row about their own session — not
-- the project-level role_change row.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a8888888-8888-8888-8888-888888888801';
select results_eq(
  $$select count(*) from audit_log where project_id = 'a8888888-8888-8888-8888-88888888880a'$$,
  $$values (1::bigint)$$, 'the session owner sees exactly one row — the one about their own session'
);
select results_eq(
  $$select action from audit_log where project_id = 'a8888888-8888-8888-8888-88888888880a'$$,
  $$values ('pull_manifest'::text)$$, 'and it is the pull_manifest row, not the role_change row'
);

-- A resumer on the project who does NOT own the session sees nothing
-- — "only admins, or the session's own owner," not "any project
-- member."
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a8888888-8888-8888-8888-888888888804';
select results_eq(
  $$select count(*) from audit_log where project_id = 'a8888888-8888-8888-8888-88888888880a'$$,
  $$values (0::bigint)$$, 'a resumer who pulled the session but does not own it sees nothing'
);

-- A non-member sees nothing.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a8888888-8888-8888-8888-888888888803';
select results_eq(
  $$select count(*) from audit_log where project_id = 'a8888888-8888-8888-8888-88888888880a'$$,
  $$values (0::bigint)$$, 'a non-member sees nothing'
);

-- Nobody can insert, update, or delete directly — not even the admin.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a8888888-8888-8888-8888-888888888802';
select throws_ok(
  $$insert into audit_log (action, project_id) values ('config_change', 'a8888888-8888-8888-8888-88888888880a')$$,
  '42501', null, 'an admin cannot insert an audit_log row directly (no insert policy)'
);
select lives_ok(
  $$update audit_log set action = 'tampered' where project_id = 'a8888888-8888-8888-8888-88888888880a'$$,
  'an admin''s update runs without erroring'
);
select results_eq(
  $$select count(*) from audit_log where action = 'tampered'$$,
  $$values (0::bigint)$$, 'but it affected zero rows — audit_log is append-only even for admins'
);
select lives_ok(
  $$delete from audit_log where project_id = 'a8888888-8888-8888-8888-88888888880a'$$,
  'an admin''s delete runs without erroring'
);
select results_eq(
  $$select count(*) from audit_log where project_id = 'a8888888-8888-8888-8888-88888888880a'$$,
  $$values (2::bigint)$$, 'but it affected zero rows — both rows still exist'
);

select * from finish();
rollback;

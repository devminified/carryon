-- CO-410: agent_heartbeats RLS — the agent can only ever write its
-- own row; only a user who is admin on at least one project (not
-- necessarily this one — is_any_project_admin() is deliberately not
-- has_role(), see the migration's own comment) can read the fleet.
begin;
select plan(8);

insert into auth.users (id, email) values
  ('a7777777-7777-4777-8777-777777777701', 'admin-of-a@test.local'),
  ('a7777777-7777-4777-8777-777777777702', 'viewer-everywhere@test.local'),
  ('a7777777-7777-4777-8777-777777777703', 'agent-user@test.local');

insert into projects (id, name, git_remote) values
  ('a7777777-7777-4777-8777-77777777770a', 'proj-a', 'example.com/a/proj-a');

insert into project_members (project_id, user_id, role) values
  ('a7777777-7777-4777-8777-77777777770a', 'a7777777-7777-4777-8777-777777777701', 'admin'),
  ('a7777777-7777-4777-8777-77777777770a', 'a7777777-7777-4777-8777-777777777702', 'viewer');

insert into agent_heartbeats (user_id, machine_id, hostname, version, blocked_sessions) values
  ('a7777777-7777-4777-8777-777777777703', 'machine-1', 'alis-laptop', '1.2.3', 0);

-- An admin of project-a (not agent-user's own project at all — this
-- is the whole point of is_any_project_admin() over has_role()) can
-- read the fleet-wide heartbeat list.
set local role authenticated;
set local request.jwt.claim.sub = 'a7777777-7777-4777-8777-777777777701';
select results_eq(
  $$select count(*) from agent_heartbeats$$,
  $$values (1::bigint)$$, 'an admin of any project can read the fleet-wide heartbeat list'
);

-- A mere viewer (not an admin anywhere) cannot.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a7777777-7777-4777-8777-777777777702';
select results_eq(
  $$select count(*) from agent_heartbeats$$,
  $$values (0::bigint)$$, 'a non-admin cannot read the heartbeat list'
);

-- The agent itself (not an admin anywhere, and not even a project
-- member at all) cannot read the list either — insert/update only.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a7777777-7777-4777-8777-777777777703';
select results_eq(
  $$select count(*) from agent_heartbeats$$,
  $$values (0::bigint)$$, 'the reporting agent itself cannot read the heartbeat list back'
);

-- It can insert a second machine's row for itself.
select lives_ok(
  $$insert into agent_heartbeats (user_id, machine_id, hostname, version, blocked_sessions) values ('a7777777-7777-4777-8777-777777777703', 'machine-2', 'alis-desktop', '1.2.3', 1)$$,
  'a user can insert a heartbeat row for their own machine'
);

-- It can update its own existing row.
select lives_ok(
  $$update agent_heartbeats set blocked_sessions = 2 where user_id = 'a7777777-7777-4777-8777-777777777703' and machine_id = 'machine-1'$$,
  'a user can update their own heartbeat row'
);

-- It cannot insert a row claiming to be someone else.
select throws_ok(
  $$insert into agent_heartbeats (user_id, machine_id, hostname, version) values ('a7777777-7777-4777-8777-777777777701', 'machine-x', 'not-mine', '1.0.0')$$,
  '42501', null, 'a user cannot insert a heartbeat row for a different user_id'
);

-- No delete policy for anyone — a stale machine's row just ages out
-- (last_seen_at), it isn't removed. A missing DELETE policy (unlike a
-- missing INSERT one) doesn't error — it silently matches zero rows,
-- same shape as ai_usage_rls.sql's own append-only check.
select lives_ok(
  $$delete from agent_heartbeats where user_id = 'a7777777-7777-4777-8777-777777777703' and machine_id = 'machine-1'$$,
  'the delete runs without erroring'
);

-- Verified as the database owner (there's no self-read policy for
-- agent_heartbeats at all — only an admin can read it back, and
-- agent-user isn't one) that the row actually survived.
reset role;
select results_eq(
  $$select count(*) from agent_heartbeats where user_id = 'a7777777-7777-4777-8777-777777777703' and machine_id = 'machine-1'$$,
  $$values (1::bigint)$$, 'but the delete affected zero rows — the heartbeat row survived, no delete policy'
);

select * from finish();
rollback;

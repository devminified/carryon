-- CO-504: observability_alert_state and storage_snapshots RLS (admin-
-- read, service-role-only write — same shape as claude_compat_runs),
-- plus the real alert-condition SQL functions: blocked for a plain
-- authenticated caller (they're an internal implementation detail of
-- check-observability-alerts, not an API surface — see the
-- migration's own comment), and computing the right real answer when
-- called as service_role.
begin;
select plan(9);

insert into auth.users (id, email) values
  ('f0000000-0000-0000-0000-000000000001', 'global-admin@test.local'),
  ('f0000000-0000-0000-0000-000000000002', 'plain-viewer@test.local');

insert into projects (id, name, git_remote) values
  ('f0000000-0000-0000-0000-00000000000a', 'proj-a', 'example.com/f/proj-a');

insert into project_members (project_id, user_id, role) values
  ('f0000000-0000-0000-0000-00000000000a', 'f0000000-0000-0000-0000-000000000001', 'admin'),
  ('f0000000-0000-0000-0000-00000000000a', 'f0000000-0000-0000-0000-000000000002', 'viewer');

insert into observability_alert_state (alert_key, is_firing, last_fired_at) values
  ('stale_agent', true, now());
insert into storage_snapshots (project_id, snapshotted_at, total_bytes) values
  ('f0000000-0000-0000-0000-00000000000a', current_date, 123456);

-- An admin of any project can read both new tables.
set local role authenticated;
set local request.jwt.claim.sub = 'f0000000-0000-0000-0000-000000000001';
select results_eq(
  $$select count(*) from observability_alert_state$$,
  $$values (1::bigint)$$, 'an admin of any project can read the alert-state table'
);
select results_eq(
  $$select count(*) from storage_snapshots$$,
  $$values (1::bigint)$$, 'an admin of any project can read storage_snapshots'
);

-- A plain viewer (not an admin of anything) sees neither.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'f0000000-0000-0000-0000-000000000002';
select results_eq(
  $$select count(*) from observability_alert_state$$,
  $$values (0::bigint)$$, 'a non-admin sees zero alert-state rows'
);
select results_eq(
  $$select count(*) from storage_snapshots$$,
  $$values (0::bigint)$$, 'a non-admin sees zero storage_snapshots rows'
);

-- No insert policy for anyone but the service role, on either table.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'f0000000-0000-0000-0000-000000000001';
select throws_ok(
  $$insert into observability_alert_state (alert_key, is_firing) values ('fake', true)$$,
  '42501', null, 'an authenticated admin cannot insert an alert-state row directly'
);

-- The five alert-condition functions are revoked from public — an
-- ordinary authenticated caller is blocked, same as any internal
-- implementation detail with no RLS to lean on at the row level.
select throws_ok(
  $$select has_stale_agent_on_a_workday()$$,
  '42501', null, 'has_stale_agent_on_a_workday is blocked for a plain authenticated caller'
);

-- As the service role (the only real caller, via check-observability-
-- alerts), the functions actually compute the right answer.
reset role;
set local role service_role;
select results_eq(
  $$select fleet_upload_error_rate()$$,
  $$values (null::double precision)$$, 'fleet_upload_error_rate is null with no heartbeats at all, not a false 0%'
);

insert into agent_heartbeats (user_id, machine_id, hostname, version, upload_attempts, upload_errors) values
  ('f0000000-0000-0000-0000-000000000001', 'm1', 'host1', '1.0.0', 20, 2);
select results_eq(
  $$select fleet_upload_error_rate()$$,
  $$values (0.1::double precision)$$, 'fleet_upload_error_rate reflects real upload_attempts/upload_errors'
);

insert into storage_snapshots (project_id, snapshotted_at, total_bytes) values
  ('f0000000-0000-0000-0000-00000000000a', current_date - 1, 1000000000)
on conflict (project_id, snapshotted_at) do update set total_bytes = excluded.total_bytes;
update storage_snapshots set total_bytes = 2000000000
  where project_id = 'f0000000-0000-0000-0000-00000000000a' and snapshotted_at = current_date;
select results_eq(
  $$select * from abnormal_storage_growth_project_ids()$$,
  $$values ('f0000000-0000-0000-0000-00000000000a'::uuid)$$,
  'abnormal_storage_growth_project_ids fires for a real >50%/500MB day-over-day jump'
);

select * from finish();
rollback;

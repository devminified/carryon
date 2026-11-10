-- CO-504: observability_alert_state, storage_snapshots, and
-- summarize_job_runs RLS (admin-read, service-role-only write — same
-- shape as claude_compat_runs), plus the real alert-condition and
-- dashboard SQL functions: blocked for a plain authenticated caller
-- (they're an internal implementation detail of
-- check-observability-alerts/the dashboard query, not an API surface
-- — see the migration's own comment), and computing the right real
-- answer when called as service_role.
begin;
select plan(15);

insert into auth.users (id, email) values
  ('f0000000-0000-0000-0000-000000000001', 'global-admin@test.local'),
  ('f0000000-0000-0000-0000-000000000002', 'plain-viewer@test.local');

insert into projects (id, name, git_remote) values
  ('f0000000-0000-0000-0000-00000000000a', 'proj-a', 'example.com/f/proj-a');

insert into project_members (project_id, user_id, role) values
  ('f0000000-0000-0000-0000-00000000000a', 'f0000000-0000-0000-0000-000000000001', 'admin'),
  ('f0000000-0000-0000-0000-00000000000a', 'f0000000-0000-0000-0000-000000000002', 'viewer');

insert into observability_alert_state (alert_key, is_firing, last_fired_at) values
  ('stale_agent:f0000000-0000-0000-0000-000000000001:m1', true, now());
insert into storage_snapshots (project_id, snapshotted_at, total_bytes) values
  ('f0000000-0000-0000-0000-00000000000a', current_date, 123456);
insert into summarize_job_runs (session_id, status, duration_ms) values
  (null, 'summarized', 500);

-- An admin of any project can read all three new tables.
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
select results_eq(
  $$select count(*) from summarize_job_runs$$,
  $$values (1::bigint)$$, 'an admin of any project can read summarize_job_runs'
);

-- A plain viewer (not an admin of anything) sees none of them.
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
select results_eq(
  $$select count(*) from summarize_job_runs$$,
  $$values (0::bigint)$$, 'a non-admin sees zero summarize_job_runs rows'
);

-- No insert policy for anyone but the service role, on any of the
-- three tables.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'f0000000-0000-0000-0000-000000000001';
select throws_ok(
  $$insert into observability_alert_state (alert_key, is_firing) values ('fake', true)$$,
  '42501', null, 'an authenticated admin cannot insert an alert-state row directly'
);

-- The observability/dashboard functions are revoked from public — an
-- ordinary authenticated caller is blocked, same as any internal
-- implementation detail with no RLS to lean on at the row level.
select throws_ok(
  $$select * from stale_agents_on_a_workday()$$,
  '42501', null, 'stale_agents_on_a_workday is blocked for a plain authenticated caller'
);
select throws_ok(
  $$select summarize_job_p95_latency_ms()$$,
  '42501', null, 'summarize_job_p95_latency_ms is blocked for a plain authenticated caller'
);

-- As the service role (the only real caller, via
-- check-observability-alerts / the admin dashboard query), the
-- functions actually compute the right answer.
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

-- stale_agents_on_a_workday returns a SET (one row per stale
-- machine), not a fleet-wide boolean — this is the real fix CO-504's
-- own review caught: a non-identifying alert can't say who to go
-- check on.
update agent_heartbeats set last_seen_at = now() - interval '25 hours'
  where user_id = 'f0000000-0000-0000-0000-000000000001' and machine_id = 'm1';
select results_eq(
  $$select machine_id from stale_agents_on_a_workday() where extract(isodow from now()) between 1 and 5$$,
  $$select case when extract(isodow from now()) between 1 and 5 then 'm1' else null end
    where extract(isodow from now()) between 1 and 5$$,
  'stale_agents_on_a_workday names the real stale machine, on a workday'
);

-- Day-over-day growth is checked against the most recent PRIOR
-- snapshot, not literally "exactly yesterday" — a real gap (a missed
-- run) still has a baseline to compare against.
insert into storage_snapshots (project_id, snapshotted_at, total_bytes) values
  ('f0000000-0000-0000-0000-00000000000a', current_date - 2, 1000000000)
on conflict (project_id, snapshotted_at) do update set total_bytes = excluded.total_bytes;
update storage_snapshots set total_bytes = 2000000000
  where project_id = 'f0000000-0000-0000-0000-00000000000a' and snapshotted_at = current_date;
select results_eq(
  $$select * from abnormal_storage_growth_project_ids()$$,
  $$values ('f0000000-0000-0000-0000-00000000000a'::uuid)$$,
  'abnormal_storage_growth_project_ids fires against the most recent prior snapshot, even 2 days back'
);

-- summarize_job_runs' own latency/failure functions compute the real
-- numbers, and only count 'summarized' runs for latency (a skipped
-- or gave-up run's own duration isn't real summarization latency).
insert into summarize_job_runs (session_id, status, duration_ms) values
  (null, 'gave_up', 50),
  (null, 'summarized', 1000),
  (null, 'summarized', 2000);
select results_eq(
  $$select summarize_job_p50_latency_ms()$$,
  $$values (1000::double precision)$$,
  'summarize_job_p50_latency_ms only counts real summarized runs, not gave_up ones'
);
select results_eq(
  $$select round(summarize_job_failure_rate()::numeric, 2)$$,
  $$values (0.25::numeric)$$,
  'summarize_job_failure_rate counts gave_up over every run (summarized + skipped + gave_up)'
);

select * from finish();
rollback;

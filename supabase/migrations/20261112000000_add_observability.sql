-- CO-504: Observability.
--
-- 1. agent_heartbeats grows the six new fields the heartbeat now
-- reports (cmd/carryon/heartbeat.go) — OS, the aggregate bytes-behind
-- and last-upload-at across every session this machine is tracking,
-- and the upload-attempts/errors/redactions-applied counts *since the
-- last heartbeat* (internal/upload.Uploader.TakeCounts, which resets
-- them on every read) — a fleet-wide operational snapshot, not an
-- all-time total.
alter table agent_heartbeats add column os text;
alter table agent_heartbeats add column bytes_behind bigint not null default 0;
alter table agent_heartbeats add column last_upload_at timestamptz;
alter table agent_heartbeats add column upload_attempts bigint not null default 0;
alter table agent_heartbeats add column upload_errors bigint not null default 0;
alter table agent_heartbeats add column redactions_applied bigint not null default 0;

-- 2. 30-day retention — pure SQL scheduled job, same pattern as
-- end_idle_sessions (database-rls.md's "Scheduled jobs that are pure
-- SQL" section): a single DELETE, no Storage/API call, so a whole
-- Edge Function would be more than this needs. A stale machine's own
-- row used to just age out by last_seen_at forever (CO-410's own
-- comment); this is what actually removes it.
create or replace function purge_stale_heartbeats()
returns void
language sql
security definer
set search_path = public
as $$
  delete from agent_heartbeats where last_seen_at < now() - interval '30 days';
$$;

-- Same default-PUBLIC-EXECUTE footgun end_idle_sessions already
-- guards against — this is a scheduled job's own SQL, not an API
-- surface.
revoke execute on function purge_stale_heartbeats() from public;

select cron.schedule(
  'purge-stale-heartbeats',
  '0 5 * * *', -- once daily; no time-of-day is specified by the ticket
  $$select purge_stale_heartbeats();$$
);

-- 3. storage_snapshots — one row per (project, day), written by
-- check-observability-alerts on every run (upserting today's own row
-- as storage grows through the day) so "storage grows abnormally"
-- has a real yesterday-vs-today baseline to compare against, not an
-- invented number. Kept separate from `projects` itself (unlike
-- bucket/daily_token_budget) since this is a time series, not a
-- current-value column.
create table storage_snapshots (
  project_id uuid not null references projects on delete cascade,
  snapshotted_at date not null default current_date,
  total_bytes bigint not null,
  primary key (project_id, snapshotted_at)
);

alter table storage_snapshots enable row level security;

grant select, insert, update, delete on storage_snapshots to anon, authenticated;
grant select, insert, update, delete on storage_snapshots to service_role;

create policy "any_project_admin_can_read_storage_snapshots" on storage_snapshots
  for select to authenticated
  using (is_any_project_admin());

-- 4. observability_alert_state — one row per alert, tracking whether
-- it's CURRENTLY firing, so check-observability-alerts only posts to
-- Slack on the false -> true transition (never re-notifies every run
-- while still bad) and silently clears on true -> false (same
-- "notify on enter, stay quiet on exit" shape _shared/summarize.ts's
-- own blocked-session alert already established — see that file's
-- `session.outcome !== "blocked"` check). storage_growth's own
-- alert_key is per-project ('storage_growth:<project_id>'); the other
-- three are fleet-wide singletons.
create table observability_alert_state (
  alert_key text primary key,
  is_firing boolean not null default false,
  last_fired_at timestamptz,
  last_cleared_at timestamptz,
  updated_at timestamptz not null default now()
);

alter table observability_alert_state enable row level security;

grant select, insert, update, delete on observability_alert_state to anon, authenticated;
grant select, insert, update, delete on observability_alert_state to service_role;

create policy "any_project_admin_can_read_alert_state" on observability_alert_state
  for select to authenticated
  using (is_any_project_admin());

-- 5. Four SQL functions, each one alert condition — kept as real,
-- pgTAP-testable SQL functions (same reasoning as search_sessions/
-- find_sessions_by_file) rather than inlined into the Edge Function,
-- so the actual threshold logic is directly testable without an HTTP
-- round trip. All SECURITY INVOKER-equivalent in spirit (stable, no
-- security definer) — these read agent_heartbeats/storage_snapshots
-- directly, called only by check-observability-alerts via the
-- service role (there's no end-user caller for these, unlike
-- search_sessions, so RLS isn't the scoping question here; they're
-- still revoked from public below since they're an internal
-- implementation detail, not an API surface).

-- Fires when any agent hasn't checked in for 24h, but only counts on
-- a workday (Mon-Fri) — a stale weekend check-in is expected, not an
-- incident; checking isodow of "now" (when the check itself runs)
-- rather than of last_seen_at, since the alert is about "is this
-- worth paging someone about right now."
create or replace function has_stale_agent_on_a_workday()
returns boolean
language sql
stable
as $$
  select extract(isodow from now()) between 1 and 5
    and exists (
      select 1 from agent_heartbeats where last_seen_at < now() - interval '24 hours'
    );
$$;

-- p95 lag across the fleet's current heartbeat snapshot, in seconds —
-- NFR-02's own definition ("compare the local file size with
-- sessions.total_bytes") read as a time lag: a machine with nothing
-- behind has zero lag regardless of when it last uploaded; one with
-- something behind has been lagging since its own last_upload_at.
-- Cross-sectional (every current machine's own current lag), not a
-- time series — there's no per-machine lag history table, matching
-- this ticket's own "the dashboard numbers match database queries"
-- testing point with a query simple enough to literally be that.
create or replace function lag_p95_seconds()
returns double precision
language sql
stable
as $$
  select percentile_cont(0.95) within group (order by lag_seconds)
  from (
    select case
      when bytes_behind > 0 and last_upload_at is not null
        then extract(epoch from (now() - last_upload_at))
      else 0
    end as lag_seconds
    from agent_heartbeats
  ) t;
$$;

create or replace function lag_p50_seconds()
returns double precision
language sql
stable
as $$
  select percentile_cont(0.5) within group (order by lag_seconds)
  from (
    select case
      when bytes_behind > 0 and last_upload_at is not null
        then extract(epoch from (now() - last_upload_at))
      else 0
    end as lag_seconds
    from agent_heartbeats
  ) t;
$$;

-- Fleet-wide error rate over the most recent heartbeat interval per
-- machine (upload_attempts/upload_errors both reset on every real
-- heartbeat — see TakeCounts' own doc comment) — a snapshot of "right
-- now," not a rolling window; null (not 0) when nobody has attempted
-- an upload this interval, so the caller can tell "no data" apart
-- from "a real 0% error rate."
create or replace function fleet_upload_error_rate()
returns double precision
language sql
stable
as $$
  select case when sum(upload_attempts) > 0
    then sum(upload_errors)::double precision / sum(upload_attempts)
    else null
  end
  from agent_heartbeats;
$$;

-- Project IDs whose storage grew abnormally between yesterday's and
-- today's snapshot — both an absolute floor (500MB) and a relative
-- one (50% day-over-day) have to be crossed, so a tiny project's
-- ordinary noise never fires this; both thresholds are a documented,
-- concrete choice, not a derived constant — tune them in a new
-- migration if the real fleet's own data says otherwise.
create or replace function abnormal_storage_growth_project_ids()
returns setof uuid
language sql
stable
as $$
  select t.project_id
  from storage_snapshots t
  join storage_snapshots y
    on y.project_id = t.project_id and y.snapshotted_at = t.snapshotted_at - 1
  where t.snapshotted_at = current_date
    and t.total_bytes - y.total_bytes > 500000000
    and y.total_bytes > 0
    and (t.total_bytes - y.total_bytes)::numeric / y.total_bytes > 0.5;
$$;

-- Today's real total bytes per project, summed from sessions.total_bytes
-- (the same column CO-402's own list already reads) — what
-- check-observability-alerts upserts into storage_snapshots each run.
-- A plain grouped sum, not a join to storage_snapshots itself, so
-- this function answers only "what is true right now," independent
-- of whatever's already been snapshotted today.
create or replace function project_storage_totals()
returns table (project_id uuid, total_bytes bigint)
language sql
stable
as $$
  select s.project_id, sum(s.total_bytes)::bigint as total_bytes
  from sessions s
  group by s.project_id;
$$;

-- Revoked from PUBLIC (same default-grant footgun end_idle_sessions
-- already guards against) but explicitly re-granted to service_role:
-- unlike end_idle_sessions (called directly by pg_cron as postgres,
-- never through PostgREST), check-observability-alerts calls these
-- via admin.rpc(...) — the service-role PostgREST client — so
-- service_role needs its own real grant, not just an assumption that
-- "service role" means "bypasses this." Verified directly: without
-- this grant, `set role service_role; select has_stale_agent_on_a_
-- workday();` 42501s.
revoke execute on function project_storage_totals() from public;
grant execute on function project_storage_totals() to service_role;
revoke execute on function has_stale_agent_on_a_workday() from public;
grant execute on function has_stale_agent_on_a_workday() to service_role;
revoke execute on function lag_p95_seconds() from public;
grant execute on function lag_p95_seconds() to service_role;
revoke execute on function lag_p50_seconds() from public;
grant execute on function lag_p50_seconds() to service_role;
revoke execute on function fleet_upload_error_rate() from public;
grant execute on function fleet_upload_error_rate() to service_role;
revoke execute on function abnormal_storage_growth_project_ids() from public;
grant execute on function abnormal_storage_growth_project_ids() to service_role;

-- 6. check-observability-alerts — same pg_cron + net.http_post +
-- Vault shape as send-daily-digest/rescan/sync-github-teams/
-- purge-expired-sessions (edge-functions.md's "scheduled Edge
-- Functions" section) — a human has to set this secret to match the
-- function's own CHECK_ALERTS_CRON_SECRET via:
--   select vault.update_secret(
--     (select id from vault.secrets where name = 'check_observability_alerts_cron_secret'),
--     'the-same-value-as-CHECK_ALERTS_CRON_SECRET'
--   );
select vault.create_secret(
  'REPLACE-ME-see-this-migrations-own-comment',
  'check_observability_alerts_cron_secret'
);

select cron.schedule(
  'check-observability-alerts',
  '*/30 * * * *', -- every 30 minutes; no cadence is specified by the ticket itself
  $$
  select net.http_post(
    url := 'http://kong:8000/functions/v1/check-observability-alerts',
    headers := jsonb_build_object(
      'Content-Type', 'application/json',
      'x-cron-secret', (
        select decrypted_secret from vault.decrypted_secrets
        where name = 'check_observability_alerts_cron_secret'
      )
    ),
    body := '{}'::jsonb
  );
  $$
);

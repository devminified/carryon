-- CO-111: session record lifecycle.
--
-- sessions.machine/branch/head_sha/claude_code_version already exist
-- (CO-102's initial schema) but nothing populates or transitions them
-- yet — register-chunk (this migration's companion Edge Function change)
-- now sets them on first insert, confirm-chunk updates last_activity_at/
-- total_bytes/status on every confirmed chunk, and end-session sets
-- status = 'ended' on a SessionEnd event.
--
-- ended_reason distinguishes an explicit SessionEnd from the idle sweep
-- below, so the two are never confused later (e.g. by a future admin
-- UI deciding whether a "blocked" outcome deserves a nudge).
alter table sessions add column ended_reason text
  check (ended_reason in ('explicit', 'idle'));

-- The idle sweep is a scheduled Postgres job, not an Edge Function — the
-- state transition is pure SQL with no Storage/API call involved, so
-- pg_cron calling a plain SQL function directly is the minimal
-- implementation (see .claude/rules/edge-functions.md's "no queue/
-- outbox" pattern: straightforward periodic work goes through a
-- scheduled job, and this one needs even less than an Edge Function).
create extension if not exists pg_cron schema extensions;

create or replace function end_idle_sessions()
returns void
language sql
security definer
set search_path = public
as $$
  update sessions
  set status = 'ended', ended_reason = 'idle'
  where status = 'live'
    and last_activity_at < now() - interval '12 hours';
$$;

-- Postgres grants EXECUTE on new functions to PUBLIC by default — which
-- would otherwise let anon/authenticated trigger this over PostgREST at
-- /rest/v1/rpc/end_idle_sessions. It's a scheduled job's implementation
-- detail, not an API surface, so revoke the default grant explicitly
-- (same defence-in-depth posture as every RLS policy in this project).
revoke execute on function end_idle_sessions() from public;

select cron.schedule(
  'end-idle-sessions',
  '*/15 * * * *', -- same cadence as sync-github-teams (auth.md)
  'select end_idle_sessions();'
);

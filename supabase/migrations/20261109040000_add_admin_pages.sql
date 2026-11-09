-- CO-410: admin pages. Two new things the five admin pages need that
-- no earlier ticket already built:
--
-- 1. is_any_project_admin() — "is this user an admin of at least one
--    project," the access check the whole /admin section in the web
--    app is gated on (CLAUDE.md: "project/CTO admins doing access
--    review"). Deliberately NOT has_role(project_id, 'admin') reused
--    — that function answers "admin of *this* project," a different
--    question from "trusted enough to see the admin console at all."
--    Same STABLE SECURITY DEFINER shape as has_role() for the same
--    reason (one lookup per statement, not per row).
--
-- 2. agent_heartbeats — the laptop's own periodic "I'm alive" check-in
--    (CO-410's own "from agent heartbeats added here" — this is
--    genuinely new, no earlier ticket reports this). Keyed by
--    (user_id, machine_id), not just user_id, since one developer can
--    run the agent on more than one machine and the Agents page lists
--    per-machine rows. RLS: the agent can only ever upsert its own
--    row (caller client, user_id = auth.uid()); only an admin-of-
--    any-project can read the fleet-wide list.
create or replace function is_any_project_admin()
returns boolean
language sql
security definer
stable
set search_path = public
as $$
  select exists (
    select 1 from project_members
    where user_id = auth.uid() and role = 'admin'
  );
$$;

create table agent_heartbeats (
  user_id uuid not null,
  machine_id text not null,
  hostname text not null,
  version text not null,
  blocked_sessions int not null default 0,
  last_seen_at timestamptz not null default now(),
  primary key (user_id, machine_id)
);

alter table agent_heartbeats enable row level security;

-- api.auto_expose_new_tables is off (config.toml) — same explicit
-- grant every new table in this codebase needs regardless of RLS.
grant select, insert, update, delete on agent_heartbeats to anon, authenticated;
grant select, insert, update, delete on agent_heartbeats to service_role;

create policy "self_can_upsert_own_heartbeat" on agent_heartbeats
  for insert to authenticated
  with check (user_id = auth.uid());

create policy "self_can_update_own_heartbeat" on agent_heartbeats
  for update to authenticated
  using (user_id = auth.uid())
  with check (user_id = auth.uid());

create policy "any_project_admin_can_read_heartbeats" on agent_heartbeats
  for select to authenticated
  using (is_any_project_admin());

-- redaction_config (CO-301) was created with NO grant to service_role
-- beyond Supabase's own default SELECT-only privilege — fine until
-- now, since get-config/rescan only ever read it. This Config page is
-- the first thing that ever WRITES to it, and found the gap for real:
-- the admin's own update 500'd with Postgres's own "permission denied
-- for table redaction_config" (a raw GRANT failure, not an RLS one —
-- RLS says nothing here at all, by CO-301's own original design).
grant insert, update, delete on redaction_config to service_role;

-- The web admin "Projects"/"Members"/"Audit"/"Config" pages read and
-- write every project's data (not just ones the signed-in admin is a
-- direct member of) via the service-role client, authorized by this
-- same is_any_project_admin() check run server-side first — see
-- web/src/shared/lib/auth.ts's requireGlobalAdmin(). That's a
-- deliberate, narrow widening of architecture.md's case (b) ("admin
-- operations that can't be expressed via RLS"): per-project RLS
-- (has_role()) has no way to express "an admin of project A may also
-- manage project B," and Carryon's own real usage (one Devminified
-- ops team running the whole client fleet, CLAUDE.md's "project/CTO
-- admins doing access review") makes that the correct model here —
-- not a multi-tenant product where one client's admin must never see
-- another's data. No new RLS policy is added to `projects`/
-- `project_members`/`audit_log` for this; it stays entirely a web-
-- app-side, service-role-client concern.

-- CO-104: service_role needs explicit table grants too. BYPASSRLS exempts
-- it from RLS policies, but GRANT is a separate, still-required privilege
-- layer — Postgres checks both independently. CO-102's migration granted
-- anon/authenticated but missed service_role; discovered when
-- register-project's service-role insert into `projects` failed with
-- 42501 (insufficient_privilege) against a real local instance.
grant select, insert, update, delete on
  projects, project_members, sessions, session_chunks, code_snapshots, audit_log
  to service_role;

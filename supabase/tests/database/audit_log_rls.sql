-- CO-302: audit_log stays deny-all for every role, same as CO-102 left
-- it — CO-304 ("Audit log," depends on this ticket) is what adds the
-- real columns and the real select policy ("admins read everything,
-- a developer reads rows about their own sessions"). This test exists
-- so that if CO-304 lands a policy that's broader than intended, the
-- diff in this file (not a silent behavior change) is what catches it.
begin;
select plan(4);

insert into auth.users (id, email) values
  ('a5555555-5555-5555-5555-555555555501', 'admin@test.local');

insert into projects (id, name, git_remote) values
  ('a5555555-5555-5555-5555-55555555550a', 'proj', 'example.com/a/proj');

insert into project_members (project_id, user_id, role) values
  ('a5555555-5555-5555-5555-55555555550a', 'a5555555-5555-5555-5555-555555555501', 'admin');

set local role authenticated;
set local request.jwt.claim.sub = 'a5555555-5555-5555-5555-555555555501';

select results_eq(
  'select count(*) from audit_log',
  $$values (0::bigint)$$, 'even a project admin sees zero audit_log rows (no select policy yet)'
);
select throws_ok(
  $$insert into audit_log (actor, action) values ('a5555555-5555-5555-5555-555555555501', 'pull_manifest')$$,
  '42501', null, 'a project admin cannot insert an audit_log row directly (service role does; no caller policy yet)'
);
select lives_ok(
  $$update audit_log set action = 'tampered'$$,
  'an update on audit_log runs without erroring'
);
select results_eq(
  'select count(*) from audit_log where action = ''tampered''',
  $$values (0::bigint)$$, 'but it affected zero rows'
);

select * from finish();
rollback;

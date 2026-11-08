-- CO-302 testing point: "a user removed from a project loses access on
-- their next request" — has_role() is STABLE (cached only within one
-- statement, not across statements/requests), so revoking a row is
-- immediately visible to the very next query, with nothing to
-- invalidate or expire.
begin;
select plan(2);

insert into auth.users (id, email) values
  ('a7777777-7777-7777-7777-777777777701', 'leaver@test.local');

insert into projects (id, name, git_remote) values
  ('a7777777-7777-7777-7777-77777777770a', 'proj', 'example.com/a/proj');

insert into project_members (project_id, user_id, role) values
  ('a7777777-7777-7777-7777-77777777770a', 'a7777777-7777-7777-7777-777777777701', 'admin');

set local role authenticated;
set local request.jwt.claim.sub = 'a7777777-7777-7777-7777-777777777701';

select results_eq(
  $$select count(*) from projects where id = 'a7777777-7777-7777-7777-77777777770a'$$,
  $$values (1::bigint)$$, 'while still a member, the project is visible'
);

-- Revoke membership — as the superuser, bypassing RLS, standing in for
-- an admin (or CO-303's sync job) removing this row.
reset role;
delete from project_members
  where project_id = 'a7777777-7777-7777-7777-77777777770a'
    and user_id = 'a7777777-7777-7777-7777-777777777701';

-- The very next request, same session, same identity, is already
-- locked out — no cache, no stale grant, no re-login needed.
set local role authenticated;
set local request.jwt.claim.sub = 'a7777777-7777-7777-7777-777777777701';
select results_eq(
  $$select count(*) from projects where id = 'a7777777-7777-7777-7777-77777777770a'$$,
  $$values (0::bigint)$$, 'immediately after revocation, the very next request sees zero rows'
);

select * from finish();
rollback;

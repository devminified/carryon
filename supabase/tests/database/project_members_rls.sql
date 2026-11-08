-- CO-302: project_members RLS — any member reads the list, only admins
-- write it directly (CO-303's sync job still uses the service role
-- regardless; this is for an admin's own direct action).
begin;
select plan(8);

insert into auth.users (id, email) values
  ('a1111111-1111-1111-1111-111111111101', 'viewer@test.local'),
  ('a1111111-1111-1111-1111-111111111102', 'resumer@test.local'),
  ('a1111111-1111-1111-1111-111111111103', 'admin@test.local'),
  ('a1111111-1111-1111-1111-111111111104', 'outsider@test.local'),
  ('a1111111-1111-1111-1111-111111111105', 'newcomer@test.local');

insert into projects (id, name, git_remote) values
  ('a1111111-1111-1111-1111-11111111110a', 'proj', 'example.com/a/proj');

insert into project_members (project_id, user_id, role) values
  ('a1111111-1111-1111-1111-11111111110a', 'a1111111-1111-1111-1111-111111111101', 'viewer'),
  ('a1111111-1111-1111-1111-11111111110a', 'a1111111-1111-1111-1111-111111111102', 'resumer'),
  ('a1111111-1111-1111-1111-11111111110a', 'a1111111-1111-1111-1111-111111111103', 'admin');

-- Any member (even a viewer) can see the full member list.
set local role authenticated;
set local request.jwt.claim.sub = 'a1111111-1111-1111-1111-111111111101';
select results_eq(
  $$select count(*) from project_members where project_id = 'a1111111-1111-1111-1111-11111111110a'$$,
  $$values (3::bigint)$$, 'a viewer sees the full member list'
);

-- A non-member sees none of it.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a1111111-1111-1111-1111-111111111104';
select results_eq(
  $$select count(*) from project_members where project_id = 'a1111111-1111-1111-1111-11111111110a'$$,
  $$values (0::bigint)$$, 'a non-member sees zero member rows'
);

-- A viewer cannot add a member.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a1111111-1111-1111-1111-111111111101';
select throws_ok(
  $$insert into project_members (project_id, user_id, role) values ('a1111111-1111-1111-1111-11111111110a', 'a1111111-1111-1111-1111-111111111105', 'viewer')$$,
  '42501', null, 'a viewer cannot add a member'
);

-- A resumer cannot add a member either.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a1111111-1111-1111-1111-111111111102';
select throws_ok(
  $$insert into project_members (project_id, user_id, role) values ('a1111111-1111-1111-1111-11111111110a', 'a1111111-1111-1111-1111-111111111105', 'viewer')$$,
  '42501', null, 'a resumer cannot add a member'
);

-- An admin can add a member.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a1111111-1111-1111-1111-111111111103';
select lives_ok(
  $$insert into project_members (project_id, user_id, role) values ('a1111111-1111-1111-1111-11111111110a', 'a1111111-1111-1111-1111-111111111105', 'viewer')$$,
  'an admin can add a member'
);

-- An admin can change a member's role.
select lives_ok(
  $$update project_members set role = 'resumer' where project_id = 'a1111111-1111-1111-1111-11111111110a' and user_id = 'a1111111-1111-1111-1111-111111111105'$$,
  'an admin can change a member role'
);
select results_eq(
  $$select role::text from project_members where project_id = 'a1111111-1111-1111-1111-11111111110a' and user_id = 'a1111111-1111-1111-1111-111111111105'$$,
  $$values ('resumer'::text)$$, 'the role change actually took effect'
);

-- An admin can remove a member.
select lives_ok(
  $$delete from project_members where project_id = 'a1111111-1111-1111-1111-11111111110a' and user_id = 'a1111111-1111-1111-1111-111111111105'$$,
  'an admin can remove a member'
);

select * from finish();
rollback;

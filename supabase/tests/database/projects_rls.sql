-- CO-302: projects RLS. Deliberately no insert/update/delete policy
-- (see the migration's own comment) — those three are tested here too,
-- as "every role gets refused," so a future accidental grant doesn't
-- slip in unnoticed.
begin;
select plan(9);

insert into auth.users (id, email) values
  ('a0000000-0000-0000-0000-000000000001', 'viewer@test.local'),
  ('a0000000-0000-0000-0000-000000000002', 'resumer@test.local'),
  ('a0000000-0000-0000-0000-000000000003', 'admin@test.local'),
  ('a0000000-0000-0000-0000-000000000004', 'outsider@test.local');

insert into projects (id, name, git_remote) values
  ('a0000000-0000-0000-0000-00000000000a', 'proj-a', 'example.com/a/proj-a');

insert into project_members (project_id, user_id, role) values
  ('a0000000-0000-0000-0000-00000000000a', 'a0000000-0000-0000-0000-000000000001', 'viewer'),
  ('a0000000-0000-0000-0000-00000000000a', 'a0000000-0000-0000-0000-000000000002', 'resumer'),
  ('a0000000-0000-0000-0000-00000000000a', 'a0000000-0000-0000-0000-000000000003', 'admin');

-- viewer, resumer, admin can all read the project.
set local role authenticated;
set local request.jwt.claim.sub = 'a0000000-0000-0000-0000-000000000001';
select results_eq(
  'select count(*) from projects where id = ''a0000000-0000-0000-0000-00000000000a''',
  $$values (1::bigint)$$, 'viewer can read their project'
);

reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a0000000-0000-0000-0000-000000000002';
select results_eq(
  'select count(*) from projects where id = ''a0000000-0000-0000-0000-00000000000a''',
  $$values (1::bigint)$$, 'resumer can read their project'
);

reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a0000000-0000-0000-0000-000000000003';
select results_eq(
  'select count(*) from projects where id = ''a0000000-0000-0000-0000-00000000000a''',
  $$values (1::bigint)$$, 'admin can read their project'
);

-- A non-member gets zero rows, not an error that leaks existence.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a0000000-0000-0000-0000-000000000004';
select results_eq(
  'select count(*) from projects where id = ''a0000000-0000-0000-0000-00000000000a''',
  $$values (0::bigint)$$, 'non-member sees zero rows'
);

-- No role can write directly — insert/update/delete all stay denied.
-- Back to the admin identity for these (the previous block left the
-- session as the non-member outsider).
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a0000000-0000-0000-0000-000000000003';

select throws_ok(
  $$insert into projects (id, name, git_remote) values ('a0000000-0000-0000-0000-00000000000b', 'x', 'example.com/x/x')$$,
  '42501', null, 'admin cannot insert a project directly (no policy)'
);
-- update/delete with no matching policy don't error (unlike insert's
-- WITH CHECK) — they just filter every row out via USING, silently
-- affecting zero rows. Prove that directly: run each, then confirm
-- (still as admin, who can read) nothing changed.
select lives_ok(
  $$update projects set name = 'renamed' where id = 'a0000000-0000-0000-0000-00000000000a'$$,
  'update runs without erroring (no policy just filters rows, not an exception)'
);
select results_eq(
  $$select name from projects where id = 'a0000000-0000-0000-0000-00000000000a'$$,
  $$values ('proj-a'::text)$$, 'the update affected zero rows — name is unchanged'
);
select lives_ok(
  $$delete from projects where id = 'a0000000-0000-0000-0000-00000000000a'$$,
  'delete runs without erroring'
);
select results_eq(
  $$select count(*) from projects where id = 'a0000000-0000-0000-0000-00000000000a'$$,
  $$values (1::bigint)$$, 'the delete affected zero rows — the project still exists'
);

select * from finish();
rollback;

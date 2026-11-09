-- CO-302: sessions RLS — owner always reads their own session, any
-- project member (viewer+) reads every session in that project, only
-- the owner writes it, only an admin deletes it.
-- CO-307: deleting a parent session keeps its forks, reparented to
-- null (the plain ON DELETE SET NULL FK on parent_session_id, not an
-- RLS policy — still worth locking in here since it's this ticket's
-- own named testing point, right next to the admin-delete test it
-- depends on).
begin;
select plan(15);

insert into auth.users (id, email) values
  ('a2222222-2222-2222-2222-222222222201', 'owner@test.local'),
  ('a2222222-2222-2222-2222-222222222202', 'viewer@test.local'),
  ('a2222222-2222-2222-2222-222222222203', 'admin@test.local'),
  ('a2222222-2222-2222-2222-222222222204', 'outsider@test.local');

insert into projects (id, name, git_remote) values
  ('a2222222-2222-2222-2222-22222222220a', 'proj', 'example.com/a/proj');

insert into project_members (project_id, user_id, role) values
  ('a2222222-2222-2222-2222-22222222220a', 'a2222222-2222-2222-2222-222222222201', 'resumer'),
  ('a2222222-2222-2222-2222-22222222220a', 'a2222222-2222-2222-2222-222222222202', 'viewer'),
  ('a2222222-2222-2222-2222-22222222220a', 'a2222222-2222-2222-2222-222222222203', 'admin');

-- The owner creates their own session.
set local role authenticated;
set local request.jwt.claim.sub = 'a2222222-2222-2222-2222-222222222201';
select lives_ok(
  $$insert into sessions (id, project_id, user_id, original_cwd) values ('a2222222-2222-2222-2222-22222222220b', 'a2222222-2222-2222-2222-22222222220a', 'a2222222-2222-2222-2222-222222222201', '/home/x')$$,
  'the owner can create their own session'
);

-- Nobody can create a session on someone else's behalf.
select throws_ok(
  $$insert into sessions (id, project_id, user_id, original_cwd) values ('a2222222-2222-2222-2222-22222222220c', 'a2222222-2222-2222-2222-22222222220a', 'a2222222-2222-2222-2222-222222222202', '/home/x')$$,
  '42501', null, 'cannot create a session for a different user_id'
);

-- A viewer (not the owner) can still see the session.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a2222222-2222-2222-2222-222222222202';
select results_eq(
  $$select count(*) from sessions where id = 'a2222222-2222-2222-2222-22222222220b'$$,
  $$values (1::bigint)$$, 'a viewer (not the owner) can still see the session'
);

-- A non-member cannot.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a2222222-2222-2222-2222-222222222204';
select results_eq(
  $$select count(*) from sessions where id = 'a2222222-2222-2222-2222-22222222220b'$$,
  $$values (0::bigint)$$, 'a non-member sees zero rows'
);

-- A viewer cannot update the session.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a2222222-2222-2222-2222-222222222202';
select lives_ok(
  $$update sessions set title = 'hijacked' where id = 'a2222222-2222-2222-2222-22222222220b'$$,
  'a viewer''s update runs without erroring'
);
select results_eq(
  $$select title from sessions where id = 'a2222222-2222-2222-2222-22222222220b'$$,
  $$values (null::text)$$, 'but the viewer''s update affected zero rows — title is still unset'
);

-- CO-405: an admin (not the owner) CAN update it — rename/pin are an
-- admin's rights too (admins_can_update_sessions), on top of the
-- owner-only policy above. Row-level, not column-level (RLS has no
-- such primitive — see that migration's own comment): this proves the
-- same thing the real rename/pin paths rely on.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a2222222-2222-2222-2222-222222222203';
select lives_ok(
  $$update sessions set title = 'renamed-by-admin' where id = 'a2222222-2222-2222-2222-22222222220b'$$,
  'an admin''s update (not the owner) runs without erroring'
);
select results_eq(
  $$select title from sessions where id = 'a2222222-2222-2222-2222-22222222220b'$$,
  $$values ('renamed-by-admin'::text)$$, 'and the admin''s update actually took effect'
);

-- The owner can update their own session.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a2222222-2222-2222-2222-222222222201';
select lives_ok(
  $$update sessions set title = 'my-session' where id = 'a2222222-2222-2222-2222-22222222220b'$$,
  'the owner can update their own session'
);
select results_eq(
  $$select title from sessions where id = 'a2222222-2222-2222-2222-22222222220b'$$,
  $$values ('my-session'::text)$$, 'the owner''s update actually took effect'
);

-- A viewer cannot delete the session (only admin can).
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a2222222-2222-2222-2222-222222222202';
select lives_ok(
  $$delete from sessions where id = 'a2222222-2222-2222-2222-22222222220b'$$,
  'a viewer''s delete runs without erroring'
);

-- Still as the viewer, who DOES have select rights — confirm the
-- session survived the viewer's delete attempt.
select results_eq(
  $$select count(*) from sessions where id = 'a2222222-2222-2222-2222-22222222220b'$$,
  $$values (1::bigint)$$, 'the viewer''s delete affected zero rows — the session survived'
);

-- A fork of this session, owned by the viewer (a pulled resume) —
-- created as the database owner so this insert isn't itself under
-- test, same as every other fixture row above.
reset role;
insert into sessions (id, project_id, user_id, original_cwd, parent_session_id) values
  ('a2222222-2222-2222-2222-22222222220c', 'a2222222-2222-2222-2222-22222222220a', 'a2222222-2222-2222-2222-222222222202', '/home/y', 'a2222222-2222-2222-2222-22222222220b');

-- An admin can delete the parent.
set local role authenticated;
set local request.jwt.claim.sub = 'a2222222-2222-2222-2222-222222222203';
select lives_ok(
  $$delete from sessions where id = 'a2222222-2222-2222-2222-22222222220b'$$,
  'an admin can delete the session'
);

-- CO-307: the fork survives, reparented to null.
reset role;
select results_eq(
  $$select count(*) from sessions where id = 'a2222222-2222-2222-2222-22222222220c'$$,
  $$values (1::bigint)$$, 'the fork survives its parent being deleted'
);
select results_eq(
  $$select parent_session_id from sessions where id = 'a2222222-2222-2222-2222-22222222220c'$$,
  $$values (null::uuid)$$, 'the fork''s parent_session_id is set to null, not left dangling'
);

select * from finish();
rollback;

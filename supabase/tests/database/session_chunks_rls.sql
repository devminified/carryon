-- CO-302: session_chunks RLS — resumer+ reads (pulling requires
-- resumer+, matching get-manifest's own check), only the session's own
-- owner inserts (not just any resumer of the project), and it's
-- append-only — no update/delete policy for anyone, ever (I2).
begin;
select plan(7);

insert into auth.users (id, email) values
  ('a3333333-3333-3333-3333-333333333301', 'owner@test.local'),
  ('a3333333-3333-3333-3333-333333333302', 'viewer@test.local'),
  ('a3333333-3333-3333-3333-333333333303', 'resumer-teammate@test.local'),
  ('a3333333-3333-3333-3333-333333333304', 'outsider@test.local');

insert into projects (id, name, git_remote) values
  ('a3333333-3333-3333-3333-33333333330a', 'proj', 'example.com/a/proj');

insert into project_members (project_id, user_id, role) values
  ('a3333333-3333-3333-3333-33333333330a', 'a3333333-3333-3333-3333-333333333301', 'resumer'),
  ('a3333333-3333-3333-3333-33333333330a', 'a3333333-3333-3333-3333-333333333302', 'viewer'),
  ('a3333333-3333-3333-3333-33333333330a', 'a3333333-3333-3333-3333-333333333303', 'resumer');

insert into sessions (id, project_id, user_id, original_cwd) values
  ('a3333333-3333-3333-3333-33333333330b', 'a3333333-3333-3333-3333-33333333330a', 'a3333333-3333-3333-3333-333333333301', '/home/x');

-- The owner uploads their own session's chunk.
set local role authenticated;
set local request.jwt.claim.sub = 'a3333333-3333-3333-3333-333333333301';
select lives_ok(
  $$insert into session_chunks (session_id, stream_id, seq, byte_start, byte_end, object_key, sha256) values ('a3333333-3333-3333-3333-33333333330b', 'main', 0, 0, 100, 'k0', 'deadbeef')$$,
  'the owner can upload a chunk for their own session'
);

-- A teammate with resumer rights on the project — but not the owner of
-- THIS session — cannot upload a chunk for it.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a3333333-3333-3333-3333-333333333303';
select throws_ok(
  $$insert into session_chunks (session_id, stream_id, seq, byte_start, byte_end, object_key, sha256) values ('a3333333-3333-3333-3333-33333333330b', 'main', 1, 100, 200, 'k1', 'c0ffee')$$,
  '42501', null, 'a project resumer who is not this session''s owner cannot upload a chunk for it'
);

-- A resumer (even just a project member, not the owner) can read it.
select results_eq(
  $$select count(*) from session_chunks where session_id = 'a3333333-3333-3333-3333-33333333330b'$$,
  $$values (1::bigint)$$, 'a resumer teammate can read the chunk'
);

-- A viewer (below resumer) cannot read it.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a3333333-3333-3333-3333-333333333302';
select results_eq(
  $$select count(*) from session_chunks where session_id = 'a3333333-3333-3333-3333-33333333330b'$$,
  $$values (0::bigint)$$, 'a viewer (below resumer) cannot read the chunk'
);

-- A non-member cannot read it.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a3333333-3333-3333-3333-333333333304';
select results_eq(
  $$select count(*) from session_chunks where session_id = 'a3333333-3333-3333-3333-33333333330b'$$,
  $$values (0::bigint)$$, 'a non-member cannot read the chunk'
);

-- Append-only: not even the owner can update or delete a confirmed
-- chunk (no policy exists for either command, for any role).
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a3333333-3333-3333-3333-333333333301';
select lives_ok(
  $$update session_chunks set sha256 = 'tampered' where session_id = 'a3333333-3333-3333-3333-33333333330b' and stream_id = 'main' and seq = 0$$,
  'the owner''s update on a chunk runs without erroring'
);
select results_eq(
  $$select sha256 from session_chunks where session_id = 'a3333333-3333-3333-3333-33333333330b' and stream_id = 'main' and seq = 0$$,
  $$values ('deadbeef'::text)$$, 'but it affected zero rows — chunks are append-only, even for the owner'
);

select * from finish();
rollback;

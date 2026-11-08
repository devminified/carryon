-- CO-302: code_snapshots RLS — same owning-session shape as
-- session_chunks, except this one genuinely is upserted (retrying the
-- same (session_id, seq) replaces the row — NFR-05), so it has a real
-- update policy too, unlike session_chunks' append-only one.
begin;
select plan(6);

insert into auth.users (id, email) values
  ('a4444444-4444-4444-4444-444444444401', 'owner@test.local'),
  ('a4444444-4444-4444-4444-444444444402', 'resumer-teammate@test.local'),
  ('a4444444-4444-4444-4444-444444444403', 'viewer@test.local');

insert into projects (id, name, git_remote) values
  ('a4444444-4444-4444-4444-44444444440a', 'proj', 'example.com/a/proj');

insert into project_members (project_id, user_id, role) values
  ('a4444444-4444-4444-4444-44444444440a', 'a4444444-4444-4444-4444-444444444401', 'resumer'),
  ('a4444444-4444-4444-4444-44444444440a', 'a4444444-4444-4444-4444-444444444402', 'resumer'),
  ('a4444444-4444-4444-4444-44444444440a', 'a4444444-4444-4444-4444-444444444403', 'viewer');

insert into sessions (id, project_id, user_id, original_cwd) values
  ('a4444444-4444-4444-4444-44444444440b', 'a4444444-4444-4444-4444-44444444440a', 'a4444444-4444-4444-4444-444444444401', '/home/x');

-- The owner creates a snapshot.
set local role authenticated;
set local request.jwt.claim.sub = 'a4444444-4444-4444-4444-444444444401';
select lives_ok(
  $$insert into code_snapshots (session_id, seq, head_sha, wip_ref) values ('a4444444-4444-4444-4444-44444444440b', 0, 'abc123', 'refs/carryon/owner/x')$$,
  'the owner can create a snapshot for their own session'
);

-- A resumer teammate (not this session's owner) cannot.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a4444444-4444-4444-4444-444444444402';
select throws_ok(
  $$insert into code_snapshots (session_id, seq, head_sha) values ('a4444444-4444-4444-4444-44444444440b', 1, 'def456')$$,
  '42501', null, 'a resumer teammate who is not the owner cannot create a snapshot for it'
);

-- That same teammate CAN read it (resumer+).
select results_eq(
  $$select count(*) from code_snapshots where session_id = 'a4444444-4444-4444-4444-44444444440b'$$,
  $$values (1::bigint)$$, 'a resumer teammate can read the snapshot'
);

-- A viewer (below resumer) cannot.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a4444444-4444-4444-4444-444444444403';
select results_eq(
  $$select count(*) from code_snapshots where session_id = 'a4444444-4444-4444-4444-44444444440b'$$,
  $$values (0::bigint)$$, 'a viewer (below resumer) cannot read the snapshot'
);

-- Retrying the same (session_id, seq) as an upsert — the owner's
-- update right must actually work, unlike session_chunks' append-only
-- policy.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a4444444-4444-4444-4444-444444444401';
select lives_ok(
  $$insert into code_snapshots (session_id, seq, head_sha, wip_ref) values ('a4444444-4444-4444-4444-44444444440b', 0, 'newer-sha', 'refs/carryon/owner/x')
    on conflict (session_id, seq) do update set head_sha = excluded.head_sha$$,
  'the owner can upsert (retry) the same (session_id, seq) snapshot'
);
select results_eq(
  $$select head_sha from code_snapshots where session_id = 'a4444444-4444-4444-4444-44444444440b' and seq = 0$$,
  $$values ('newer-sha'::text)$$, 'the upsert actually replaced the row'
);

select * from finish();
rollback;

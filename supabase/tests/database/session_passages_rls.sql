-- CO-501: session_passages RLS mirrors sessions' own
-- owner_or_member_can_read_sessions exactly (a passage is exactly as
-- visible as the session it's a turn of) — and, this ticket's own
-- named testing point, search_sessions (SECURITY INVOKER, not
-- DEFINER) never returns a session the caller can't otherwise see,
-- even though it reads sessions/session_passages directly rather than
-- through has_role(). Two projects, two sessions, both with real
-- embeddings, so the search_sessions checks below are a genuine
-- cross-project isolation test, not just a single-row presence check.
begin;
select plan(11);

insert into auth.users (id, email) values
  ('b0000000-0000-0000-0000-000000000001', 'owner-a@test.local'),
  ('b0000000-0000-0000-0000-000000000002', 'viewer-a@test.local'),
  ('b0000000-0000-0000-0000-000000000003', 'outsider@test.local'),
  ('b0000000-0000-0000-0000-000000000004', 'owner-b@test.local');

insert into projects (id, name, git_remote) values
  ('b0000000-0000-0000-0000-00000000000a', 'proj-a', 'example.com/b/proj-a'),
  ('b0000000-0000-0000-0000-00000000000b', 'proj-b', 'example.com/b/proj-b');

insert into project_members (project_id, user_id, role) values
  ('b0000000-0000-0000-0000-00000000000a', 'b0000000-0000-0000-0000-000000000001', 'resumer'),
  ('b0000000-0000-0000-0000-00000000000a', 'b0000000-0000-0000-0000-000000000002', 'viewer'),
  ('b0000000-0000-0000-0000-00000000000b', 'b0000000-0000-0000-0000-000000000004', 'resumer');

-- A real 384-dim embedding per session (and a near-duplicate for its
-- one passage) — the exact values don't matter for an RLS test, only
-- that the column is populated so search_sessions' own
-- "s.embedding is not null" filter doesn't exclude the row for an
-- unrelated reason.
insert into sessions (id, project_id, user_id, original_cwd, title, embedding) values
  (
    'b0000000-0000-0000-0000-00000000001a',
    'b0000000-0000-0000-0000-00000000000a',
    'b0000000-0000-0000-0000-000000000001',
    '/home/a',
    'fix payment webhook retry',
    (select array_fill(0.1::real, array[384])::vector)
  ),
  (
    'b0000000-0000-0000-0000-00000000001b',
    'b0000000-0000-0000-0000-00000000000b',
    'b0000000-0000-0000-0000-000000000004',
    '/home/b',
    'unrelated session in another project',
    (select array_fill(0.1::real, array[384])::vector)
  );

insert into session_passages (session_id, seq, text, embedding) values
  (
    'b0000000-0000-0000-0000-00000000001a',
    0,
    'fixed the webhook retry backoff',
    (select array_fill(0.1::real, array[384])::vector)
  );

-- The session's own owner can read its passages.
set local role authenticated;
set local request.jwt.claim.sub = 'b0000000-0000-0000-0000-000000000001';
select results_eq(
  $$select count(*) from session_passages where session_id = 'b0000000-0000-0000-0000-00000000001a'$$,
  $$values (1::bigint)$$, 'the session''s owner can read its passages'
);

-- A project member (viewer role, not the owner) can also read them —
-- same visibility as sessions itself.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'b0000000-0000-0000-0000-000000000002';
select results_eq(
  $$select count(*) from session_passages where session_id = 'b0000000-0000-0000-0000-00000000001a'$$,
  $$values (1::bigint)$$, 'a project member (viewer role) can read the passages too'
);

-- A non-member of project A cannot.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'b0000000-0000-0000-0000-000000000003';
select results_eq(
  $$select count(*) from session_passages where session_id = 'b0000000-0000-0000-0000-00000000001a'$$,
  $$values (0::bigint)$$, 'a non-member sees zero passage rows'
);

-- No insert policy exists for anyone but the service role (only
-- summarizeSession, running as the service role, ever writes this
-- table) — an authenticated caller's insert is rejected outright.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'b0000000-0000-0000-0000-000000000001';
select throws_ok(
  $$insert into session_passages (session_id, seq, text, embedding) values ('b0000000-0000-0000-0000-00000000001a', 1, 'forged', (select array_fill(0.1::real, array[384])::vector))$$,
  '42501', null, 'an authenticated caller cannot insert a passage directly — no policy grants it'
);

-- search_sessions, called as the session's own owner, finds it and
-- surfaces the matching passage — proving the function's own join
-- actually works end to end before the RLS-isolation checks below.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'b0000000-0000-0000-0000-000000000001';
select results_eq(
  $$select session_id, matching_passage from search_sessions((select array_fill(0.1::real, array[384])::vector), 'webhook retry') where session_id = 'b0000000-0000-0000-0000-00000000001a'$$,
  $$values ('b0000000-0000-0000-0000-00000000001a'::uuid, 'fixed the webhook retry backoff'::text)$$,
  'search_sessions, called as the owner, returns the session with its matching passage'
);

-- A project A member (viewer, not owner) also finds it via
-- search_sessions — same has_role-equivalent visibility as a plain
-- select on sessions, even though this function is SECURITY INVOKER
-- reading the table directly rather than going through has_role().
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'b0000000-0000-0000-0000-000000000002';
select results_eq(
  $$select count(*) from search_sessions((select array_fill(0.1::real, array[384])::vector), 'webhook retry') where session_id = 'b0000000-0000-0000-0000-00000000001a'$$,
  $$values (1::bigint)$$, 'a project member (viewer) also finds the session via search_sessions'
);

-- This ticket's own named testing point: a non-member of project A
-- gets zero rows for project A's session from search_sessions — not
-- an error that leaks its existence, just like every other RLS policy
-- in this codebase.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'b0000000-0000-0000-0000-000000000003';
select results_eq(
  $$select count(*) from search_sessions((select array_fill(0.1::real, array[384])::vector), 'webhook retry') where session_id = 'b0000000-0000-0000-0000-00000000001a'$$,
  $$values (0::bigint)$$, 'a non-member gets zero rows for project A''s session from search_sessions'
);

-- Cross-project isolation: project B's own admin (session owner-b) can
-- see project B's session via search_sessions...
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'b0000000-0000-0000-0000-000000000004';
select results_eq(
  $$select count(*) from search_sessions((select array_fill(0.1::real, array[384])::vector), 'webhook retry') where session_id = 'b0000000-0000-0000-0000-00000000001b'$$,
  $$values (1::bigint)$$, 'project B''s own member can see project B''s session via search_sessions'
);

-- ...but never project A's session, even with no p_project_id filter
-- applied (the default, matching the Edge Function's own "search
-- everything I can see" call shape) — RLS on `sessions` inside the
-- function is the only thing doing this narrowing.
select results_eq(
  $$select count(*) from search_sessions((select array_fill(0.1::real, array[384])::vector), 'webhook retry') where session_id = 'b0000000-0000-0000-0000-00000000001a'$$,
  $$values (0::bigint)$$, 'project B''s member gets zero rows for project A''s session, with no explicit project filter'
);

-- The p_project_id filter itself still works as a narrowing, on top of
-- RLS, for a caller who's a member of both.
reset role;
insert into project_members (project_id, user_id, role) values
  ('b0000000-0000-0000-0000-00000000000b', 'b0000000-0000-0000-0000-000000000001', 'viewer');
set local role authenticated;
set local request.jwt.claim.sub = 'b0000000-0000-0000-0000-000000000001';
select results_eq(
  $$select count(*) from search_sessions((select array_fill(0.1::real, array[384])::vector), 'webhook retry', 'b0000000-0000-0000-0000-00000000000b') where session_id = 'b0000000-0000-0000-0000-00000000001a'$$,
  $$values (0::bigint)$$, 'a caller who can see both projects, filtered to project B, excludes project A''s session'
);
select results_eq(
  $$select count(*) from search_sessions((select array_fill(0.1::real, array[384])::vector), 'webhook retry', 'b0000000-0000-0000-0000-00000000000b') where session_id = 'b0000000-0000-0000-0000-00000000001b'$$,
  $$values (1::bigint)$$, 'and still includes project B''s session once a member of it too'
);

select * from finish();
rollback;

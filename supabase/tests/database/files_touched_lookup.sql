-- CO-502: find_sessions_by_file — exact and prefix matches both work
-- with one query (a plain starts-with comparison, not LIKE, so a
-- literal `_`/`%` in a real filename can't leak wildcard semantics),
-- and RLS (not a function-level check) is what actually scopes a
-- caller's results — this function is SECURITY INVOKER, same
-- reasoning as CO-501's search_sessions.
begin;
select plan(8);

insert into auth.users (id, email) values
  ('c0000000-0000-0000-0000-000000000001', 'owner-a@test.local'),
  ('c0000000-0000-0000-0000-000000000002', 'viewer-a@test.local'),
  ('c0000000-0000-0000-0000-000000000003', 'outsider@test.local'),
  ('c0000000-0000-0000-0000-000000000004', 'owner-b@test.local');

insert into projects (id, name, git_remote) values
  ('c0000000-0000-0000-0000-00000000000a', 'proj-a', 'example.com/c/proj-a'),
  ('c0000000-0000-0000-0000-00000000000b', 'proj-b', 'example.com/c/proj-b');

insert into project_members (project_id, user_id, role) values
  ('c0000000-0000-0000-0000-00000000000a', 'c0000000-0000-0000-0000-000000000001', 'resumer'),
  ('c0000000-0000-0000-0000-00000000000a', 'c0000000-0000-0000-0000-000000000002', 'viewer'),
  ('c0000000-0000-0000-0000-00000000000b', 'c0000000-0000-0000-0000-000000000004', 'resumer');

insert into sessions (id, project_id, user_id, original_cwd, files_touched, last_activity_at) values
  (
    'c0000000-0000-0000-0000-00000000001a',
    'c0000000-0000-0000-0000-00000000000a',
    'c0000000-0000-0000-0000-000000000001',
    '/home/a',
    array['src/payments/webhook.ts', 'src/payments/webhook.test.ts'],
    now() - interval '1 hour'
  ),
  (
    'c0000000-0000-0000-0000-00000000001b',
    'c0000000-0000-0000-0000-00000000000a',
    'c0000000-0000-0000-0000-000000000001',
    '/home/a',
    array['src/auth/login.ts'],
    now() - interval '2 hours'
  ),
  (
    'c0000000-0000-0000-0000-00000000001c',
    'c0000000-0000-0000-0000-00000000000b',
    'c0000000-0000-0000-0000-000000000004',
    '/home/b',
    array['src/payments/webhook.ts'],
    now() - interval '30 minutes'
  );

-- Exact match finds the one session with that exact path, as the
-- session's own owner.
set local role authenticated;
set local request.jwt.claim.sub = 'c0000000-0000-0000-0000-000000000001';
-- Project B's session 1c shares this exact path too, but owner-a has
-- no membership there at all — RLS (not this query) is why only the
-- one session this caller can actually see comes back.
select results_eq(
  $$select id from find_sessions_by_file('src/payments/webhook.ts') order by id$$,
  $$values ('c0000000-0000-0000-0000-00000000001a'::uuid)$$,
  'an exact path matches, scoped to exactly what this caller can see'
);

-- Prefix match finds every file under that directory, in one session.
select results_eq(
  $$select count(*) from find_sessions_by_file('src/payments/') where id = 'c0000000-0000-0000-0000-00000000001a'$$,
  $$values (1::bigint)$$, 'a directory prefix matches a session with more than one file under it'
);

-- A prefix that is NOT a real path segment boundary still works as a
-- plain string prefix (the ticket's own example, src/payments/, has
-- the trailing slash baked in by the caller — this function does
-- exactly what it's given, no segment-boundary logic of its own).
select results_eq(
  $$select count(*) from find_sessions_by_file('src/auth') where id = 'c0000000-0000-0000-0000-00000000001b'$$,
  $$values (1::bigint)$$, 'a bare prefix with no trailing slash still matches'
);

-- A literal underscore in the query is NOT treated as a LIKE wildcard
-- (this project's own files don't have one, so this must find
-- nothing, not accidentally match src/payments/webhook.ts via `_` as
-- "any one character").
select results_eq(
  $$select count(*) from find_sessions_by_file('src/pay_ents/webhook.ts')$$,
  $$values (0::bigint)$$, 'an underscore in the query is a literal character, not a LIKE wildcard'
);

-- A project member (viewer, not owner) also sees it.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'c0000000-0000-0000-0000-000000000002';
select results_eq(
  $$select count(*) from find_sessions_by_file('src/payments/webhook.ts') where id = 'c0000000-0000-0000-0000-00000000001a'$$,
  $$values (1::bigint)$$, 'a project member (viewer) also finds the session'
);

-- This ticket's own named testing point: results respect RLS — a
-- non-member of project A gets zero rows for project A's file, even
-- though project B's own, unrelated session happens to share the same
-- path string.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'c0000000-0000-0000-0000-000000000003';
select results_eq(
  $$select count(*) from find_sessions_by_file('src/payments/webhook.ts')$$,
  $$values (0::bigint)$$, 'a non-member of either project gets zero rows, not a leaked count'
);

-- p_project_id narrows to one project on top of RLS.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'c0000000-0000-0000-0000-000000000004';
select results_eq(
  $$select count(*) from find_sessions_by_file('src/payments/webhook.ts', 'c0000000-0000-0000-0000-00000000000b')$$,
  $$values (1::bigint)$$, 'project B''s own member finds project B''s session when scoped to that project'
);

-- Newest activity first — session 1a (1 hour ago) really does come
-- before session 1b (2 hours ago) in the function's own result order,
-- not just "both rows present" (array_agg preserves the query's own
-- row order, unlike comparing as an unordered set).
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'c0000000-0000-0000-0000-000000000001';
select results_eq(
  $$select array_agg(id) from (select id from find_sessions_by_file('src/') where project_id = 'c0000000-0000-0000-0000-00000000000a') t$$,
  $$values (array['c0000000-0000-0000-0000-00000000001a'::uuid, 'c0000000-0000-0000-0000-00000000001b'::uuid])$$,
  'results come back newest activity first, not just as an unordered set'
);

select * from finish();
rollback;

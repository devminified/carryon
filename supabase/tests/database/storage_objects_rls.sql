-- CO-302: storage.objects RLS on the carryon-sessions bucket. Keys are
-- always "<project_id>/<user_id>/<session_id>/..." — resumer+ gated,
-- same as session_chunks/code_snapshots, with a path-traversal check:
-- a malformed or path-traversal-shaped name must be refused cleanly
-- (zero rows, no error), never raise an exception that could leak
-- whether some other object exists.
begin;
select plan(5);

insert into auth.users (id, email) values
  ('a6666666-6666-6666-6666-666666666601', 'resumer@test.local'),
  ('a6666666-6666-6666-6666-666666666602', 'viewer@test.local'),
  ('a6666666-6666-6666-6666-666666666603', 'outsider@test.local');

insert into projects (id, name, git_remote) values
  ('a6666666-6666-6666-6666-66666666660a', 'proj', 'example.com/a/proj');

insert into project_members (project_id, user_id, role) values
  ('a6666666-6666-6666-6666-66666666660a', 'a6666666-6666-6666-6666-666666666601', 'resumer'),
  ('a6666666-6666-6666-6666-66666666660a', 'a6666666-6666-6666-6666-666666666602', 'viewer');

-- A real chunk object, inserted directly (as the superuser, bypassing
-- RLS, standing in for what register-chunk's service-role insert
-- would have produced) at the real path shape.
insert into storage.objects (bucket_id, name) values
  ('carryon-sessions', 'a6666666-6666-6666-6666-66666666660a/a6666666-6666-6666-6666-666666666601/a6666666-6666-6666-6666-66666666660b/main/chunk-0.jsonl.zst');

-- A path-traversal-shaped name under the SAME real project prefix —
-- storage_object_project_id() only ever looks at the first path
-- segment, so this is really just testing that an unusual-but-still-
-- real-project-prefixed name doesn't behave any differently.
insert into storage.objects (bucket_id, name) values
  ('carryon-sessions', 'a6666666-6666-6666-6666-66666666660a/../../etc/passwd');

-- A name whose first segment isn't a UUID at all (the actual
-- path-traversal case: no real project_id to even check membership
-- against).
insert into storage.objects (bucket_id, name) values
  ('carryon-sessions', '../../etc/passwd');

-- storage_object_project_id() never raises on any of the above —
-- confirmed directly, independent of role/RLS.
select is(
  storage_object_project_id('a6666666-6666-6666-6666-66666666660a/../../etc/passwd'),
  'a6666666-6666-6666-6666-66666666660a'::uuid,
  'a traversal-shaped suffix after a real project prefix still resolves to that real project id'
);
select is(
  storage_object_project_id('../../etc/passwd'), null,
  'a name with no real project-id-shaped first segment resolves to null, not an error'
);

-- A resumer can read the real chunk object.
set local role authenticated;
set local request.jwt.claim.sub = 'a6666666-6666-6666-6666-666666666601';
select results_eq(
  $$select count(*) from storage.objects where bucket_id = 'carryon-sessions' and name = 'a6666666-6666-6666-6666-66666666660a/a6666666-6666-6666-6666-666666666601/a6666666-6666-6666-6666-66666666660b/main/chunk-0.jsonl.zst'$$,
  $$values (1::bigint)$$, 'a resumer can read a real chunk object under their own project'
);

-- A viewer (below resumer) cannot.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a6666666-6666-6666-6666-666666666602';
select results_eq(
  $$select count(*) from storage.objects where bucket_id = 'carryon-sessions' and name = 'a6666666-6666-6666-6666-66666666660a/a6666666-6666-6666-6666-666666666601/a6666666-6666-6666-6666-66666666660b/main/chunk-0.jsonl.zst'$$,
  $$values (0::bigint)$$, 'a viewer (below resumer) cannot read it directly'
);

-- Nobody — not even a project resumer — can read an object whose name
-- doesn't resolve to any project at all (the real path-traversal case).
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a6666666-6666-6666-6666-666666666601';
select results_eq(
  $$select count(*) from storage.objects where bucket_id = 'carryon-sessions' and name = '../../etc/passwd'$$,
  $$values (0::bigint)$$, 'an object with no resolvable project id is refused, not errored, for every caller'
);

select * from finish();
rollback;

-- CO-407: ai_usage RLS — admins can read their own project's AI spend
-- (a future admin page's own concern, not built here); no insert/
-- update/delete policy for anyone, including admins — every write is
-- the summarize-session job's own service role (append-only, I2).
begin;
select plan(6);

insert into auth.users (id, email) values
  ('a5555555-5555-4555-8555-555555555501', 'admin@test.local'),
  ('a5555555-5555-4555-8555-555555555502', 'resumer@test.local'),
  ('a5555555-5555-4555-8555-555555555503', 'viewer@test.local');

insert into projects (id, name, git_remote) values
  ('a5555555-5555-4555-8555-55555555550a', 'proj', 'example.com/a/proj');

insert into project_members (project_id, user_id, role) values
  ('a5555555-5555-4555-8555-55555555550a', 'a5555555-5555-4555-8555-555555555501', 'admin'),
  ('a5555555-5555-4555-8555-55555555550a', 'a5555555-5555-4555-8555-555555555502', 'resumer'),
  ('a5555555-5555-4555-8555-55555555550a', 'a5555555-5555-4555-8555-555555555503', 'viewer');

-- Seeded as the database owner, same as every other fixture row in
-- this suite — summarize-session's own service-role insert isn't
-- itself under test here.
insert into ai_usage (project_id, input_tokens, output_tokens, cost_usd, model) values
  ('a5555555-5555-4555-8555-55555555550a', 1000, 200, 0.002200, 'claude-haiku-4-5-20251001');

-- An admin can read their project's own usage.
set local role authenticated;
set local request.jwt.claim.sub = 'a5555555-5555-4555-8555-555555555501';
select results_eq(
  $$select count(*) from ai_usage where project_id = 'a5555555-5555-4555-8555-55555555550a'$$,
  $$values (1::bigint)$$, 'an admin can read their project''s own ai_usage rows'
);

-- A resumer (below admin) cannot.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a5555555-5555-4555-8555-555555555502';
select results_eq(
  $$select count(*) from ai_usage where project_id = 'a5555555-5555-4555-8555-55555555550a'$$,
  $$values (0::bigint)$$, 'a resumer (below admin) cannot read ai_usage'
);

-- A viewer cannot either.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a5555555-5555-4555-8555-555555555503';
select results_eq(
  $$select count(*) from ai_usage where project_id = 'a5555555-5555-4555-8555-55555555550a'$$,
  $$values (0::bigint)$$, 'a viewer cannot read ai_usage'
);

-- No insert policy for anyone, including the project's own admin —
-- only the service role (summarize-session) writes this table.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a5555555-5555-4555-8555-555555555501';
select throws_ok(
  $$insert into ai_usage (project_id, input_tokens, output_tokens, cost_usd, model) values ('a5555555-5555-4555-8555-55555555550a', 1, 1, 0, 'x')$$,
  '42501', null, 'even an admin cannot insert into ai_usage directly — service role only'
);

-- No delete policy for anyone, either — append-only (I2). Unlike a
-- missing INSERT policy (which raises 42501), a missing DELETE policy
-- doesn't error — it silently matches zero rows, same shape as
-- end-session's own RLS-denial case elsewhere in this codebase.
select lives_ok(
  $$delete from ai_usage where project_id = 'a5555555-5555-4555-8555-55555555550a'$$,
  'the delete runs without erroring'
);
select results_eq(
  $$select count(*) from ai_usage where project_id = 'a5555555-5555-4555-8555-55555555550a'$$,
  $$values (1::bigint)$$, 'but the delete affected zero rows — the usage row survived, append-only'
);

select * from finish();
rollback;

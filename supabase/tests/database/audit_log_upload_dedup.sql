-- CO-304: "upload" is logged once per session per day (to limit
-- volume) — enforced as a real unique index, not just application
-- logic, so a retried confirm-chunk call can never double-log even
-- under a race (I3's "safe to retry" standard).
begin;
select plan(4);

insert into auth.users (id, email) values
  ('a9999999-9999-9999-9999-999999999901', 'owner@test.local');
insert into projects (id, name, git_remote) values
  ('a9999999-9999-9999-9999-99999999990a', 'proj', 'example.com/a/proj');
insert into sessions (id, project_id, user_id, original_cwd) values
  ('a9999999-9999-9999-9999-99999999990b', 'a9999999-9999-9999-9999-99999999990a', 'a9999999-9999-9999-9999-999999999901', '/home/x');

select lives_ok(
  $$insert into audit_log (action, session_id) values ('upload', 'a9999999-9999-9999-9999-99999999990b')$$,
  'the first upload row of the day inserts cleanly'
);
select throws_ok(
  $$insert into audit_log (action, session_id) values ('upload', 'a9999999-9999-9999-9999-99999999990b')$$,
  '23505', null, 'a second upload row for the same session on the same day is refused'
);
select results_eq(
  $$select count(*) from audit_log where session_id = 'a9999999-9999-9999-9999-99999999990b' and action = 'upload'$$,
  $$values (1::bigint)$$, 'exactly one upload row exists for the day'
);
-- A different action type for the same session on the same day is
-- unaffected — the uniqueness is scoped to action = 'upload' only.
select lives_ok(
  $$insert into audit_log (action, session_id) values ('pull_manifest', 'a9999999-9999-9999-9999-99999999990b')$$,
  'a different action type for the same session/day is not blocked'
);

select * from finish();
rollback;

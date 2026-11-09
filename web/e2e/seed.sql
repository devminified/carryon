-- CO-402's own real-verification fixture: NOT part of supabase/seed.sql
-- (which runs on every `supabase start`/`db reset` for every developer
-- and every CI job) — this is only for web-e2e's Playwright suite,
-- applied as its own step after local Supabase is up. 10,050 real rows
-- is the ticket's own "10,000 sessions" perf testing point; project
-- 44444444-… has no members, backing the "non-member gets a 404" test.
insert into projects (id, name, git_remote)
values ('33333333-3333-3333-3333-333333333333', 'co402-perf-test', 'github.com/devminified/co402-perf-test')
on conflict (id) do nothing;

insert into project_members (project_id, user_id, role, source)
values
  ('33333333-3333-3333-3333-333333333333', '11111111-1111-1111-1111-111111111111', 'admin', 'manual'),
  ('33333333-3333-3333-3333-333333333333', '22222222-2222-2222-2222-222222222222', 'viewer', 'manual')
on conflict (project_id, user_id) do nothing;

-- A deterministic id (not gen_random_uuid()) is what makes "on conflict
-- (id) do nothing" below actually a no-op on re-run instead of silently
-- inserting another 10,050 rows every time — found for real: wiring
-- this script into pnpm's pretest:e2e re-seed hook duplicated the whole
-- project to 20,100 rows on the very next run.
insert into sessions (id, project_id, user_id, original_cwd, branch, status, outcome, title, summary, last_activity_at, started_at, total_bytes)
select
  ('33333333-aaaa-4aaa-8aaa-' || lpad(i::text, 12, '0'))::uuid,
  '33333333-3333-3333-3333-333333333333'::uuid,
  case when i % 5 = 0 then '22222222-2222-2222-2222-222222222222'::uuid else '11111111-1111-1111-1111-111111111111'::uuid end,
  '/tmp/co402',
  (array['main','feature/payments','feature/auth','bugfix/retry'])[1 + (i % 4)],
  case when i % 10 = 0 then 'live' else 'ended' end,
  (array['done','in_progress','blocked'])[1 + (i % 3)],
  'session ' || i || ' - fix payment webhook retry logic',
  'investigated the stripe webhook retry backoff and rewrote it',
  now() - (i || ' minutes')::interval,
  now() - (i || ' minutes')::interval - interval '10 minutes',
  (1000 + i)
from generate_series(1, 10050) as i
on conflict (id) do nothing;

insert into projects (id, name, git_remote)
values ('44444444-4444-4444-4444-444444444444', 'co402-no-access', 'github.com/devminified/co402-no-access')
on conflict (id) do nothing;

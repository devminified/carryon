-- CO-409: project_notification_settings RLS — admin-only in every
-- direction (select/insert/update/delete). Unlike bucket/
-- daily_token_budget on `projects` itself (readable by any viewer),
-- a Slack webhook URL lets its holder post into the team's channel,
-- so it stays out of the viewer-readable table entirely — see the
-- migration's own comment.
begin;
select plan(8);

insert into auth.users (id, email) values
  ('a6666666-6666-4666-8666-666666666601', 'admin@test.local'),
  ('a6666666-6666-4666-8666-666666666602', 'resumer@test.local'),
  ('a6666666-6666-4666-8666-666666666603', 'viewer@test.local');

insert into projects (id, name, git_remote) values
  ('a6666666-6666-4666-8666-66666666660a', 'proj', 'example.com/a/proj');

insert into project_members (project_id, user_id, role) values
  ('a6666666-6666-4666-8666-66666666660a', 'a6666666-6666-4666-8666-666666666601', 'admin'),
  ('a6666666-6666-4666-8666-66666666660a', 'a6666666-6666-4666-8666-666666666602', 'resumer'),
  ('a6666666-6666-4666-8666-66666666660a', 'a6666666-6666-4666-8666-666666666603', 'viewer');

-- Seeded as the database owner, same as every other fixture row in
-- this suite.
insert into project_notification_settings (project_id, slack_webhook_url, notify_digest) values
  ('a6666666-6666-4666-8666-66666666660a', 'https://hooks.slack.com/services/T0/B0/x', true);

-- An admin can read their project's own settings.
set local role authenticated;
set local request.jwt.claim.sub = 'a6666666-6666-4666-8666-666666666601';
select results_eq(
  $$select count(*) from project_notification_settings where project_id = 'a6666666-6666-4666-8666-66666666660a'$$,
  $$values (1::bigint)$$, 'an admin can read their project''s own notification settings'
);

-- A resumer (below admin) cannot.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a6666666-6666-4666-8666-666666666602';
select results_eq(
  $$select count(*) from project_notification_settings where project_id = 'a6666666-6666-4666-8666-66666666660a'$$,
  $$values (0::bigint)$$, 'a resumer (below admin) cannot read notification settings'
);

-- A viewer cannot either — this is the whole point of keeping the
-- webhook URL off the viewer-readable `projects` table.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a6666666-6666-4666-8666-666666666603';
select results_eq(
  $$select count(*) from project_notification_settings where project_id = 'a6666666-6666-4666-8666-66666666660a'$$,
  $$values (0::bigint)$$, 'a viewer cannot read notification settings'
);

-- A viewer cannot insert for a project either.
select throws_ok(
  $$insert into project_notification_settings (project_id, slack_webhook_url) values ('a6666666-6666-4666-8666-66666666660a', 'https://evil.example/hook')$$,
  '42501', null, 'a viewer cannot insert notification settings'
);

-- An admin can insert settings for a different project they admin.
reset role;
insert into projects (id, name, git_remote) values
  ('a6666666-6666-4666-8666-66666666660b', 'proj-b', 'example.com/a/proj-b');
insert into project_members (project_id, user_id, role) values
  ('a6666666-6666-4666-8666-66666666660b', 'a6666666-6666-4666-8666-666666666601', 'admin');

set local role authenticated;
set local request.jwt.claim.sub = 'a6666666-6666-4666-8666-666666666601';
select lives_ok(
  $$insert into project_notification_settings (project_id, slack_webhook_url, notify_digest) values ('a6666666-6666-4666-8666-66666666660b', 'https://hooks.slack.com/services/T1/B1/y', false)$$,
  'an admin can insert notification settings for their own project'
);

-- An admin can update their project's own settings.
select lives_ok(
  $$update project_notification_settings set notify_digest = true where project_id = 'a6666666-6666-4666-8666-66666666660b'$$,
  'an admin can update their project''s own notification settings'
);

-- A resumer cannot update even though the row exists and is readable
-- to nobody but admins — a missing UPDATE policy match is zero rows,
-- not an error.
reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a6666666-6666-4666-8666-666666666602';
select lives_ok(
  $$update project_notification_settings set notify_digest = false where project_id = 'a6666666-6666-4666-8666-66666666660a'$$,
  'the update runs without erroring for a resumer'
);

reset role;
set local role authenticated;
set local request.jwt.claim.sub = 'a6666666-6666-4666-8666-666666666601';
select results_eq(
  $$select notify_digest from project_notification_settings where project_id = 'a6666666-6666-4666-8666-66666666660a'$$,
  $$values (true)$$, 'but the resumer''s update affected zero rows — notify_digest is still true'
);

select * from finish();
rollback;

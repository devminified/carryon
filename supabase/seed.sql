-- CO-102 local dev seed: one project, two users, one membership.
-- Local only — never run against staging/production.

insert into auth.users (
  instance_id, id, aud, role, email, encrypted_password,
  email_confirmed_at, last_sign_in_at,
  raw_app_meta_data, raw_user_meta_data, created_at, updated_at,
  confirmation_token, recovery_token, email_change, email_change_token_new
) values
  (
    '00000000-0000-0000-0000-000000000000',
    '11111111-1111-1111-1111-111111111111',
    'authenticated', 'authenticated',
    'ali@devminified.com',
    crypt('password123', gen_salt('bf')),
    now(), now(),
    '{"provider":"github","providers":["github"]}',
    '{"user_name":"ali"}',
    now(), now(), '', '', '', ''
  ),
  (
    '00000000-0000-0000-0000-000000000000',
    '22222222-2222-2222-2222-222222222222',
    'authenticated', 'authenticated',
    'sara@devminified.com',
    crypt('password123', gen_salt('bf')),
    now(), now(),
    '{"provider":"github","providers":["github"]}',
    '{"user_name":"sara"}',
    now(), now(), '', '', '', ''
  );

insert into auth.identities (provider_id, user_id, identity_data, provider, last_sign_in_at, created_at, updated_at)
values
  (
    '11111111-1111-1111-1111-111111111111',
    '11111111-1111-1111-1111-111111111111',
    '{"sub":"11111111-1111-1111-1111-111111111111","user_name":"ali"}',
    'github', now(), now(), now()
  ),
  (
    '22222222-2222-2222-2222-222222222222',
    '22222222-2222-2222-2222-222222222222',
    '{"sub":"22222222-2222-2222-2222-222222222222","user_name":"sara"}',
    'github', now(), now(), now()
  );

insert into projects (id, name, git_remote)
values ('33333333-3333-3333-3333-333333333333', 'acme-api', 'github.com/devminified/acme-api');

insert into project_members (project_id, user_id, role)
values ('33333333-3333-3333-3333-333333333333', '11111111-1111-1111-1111-111111111111', 'admin');

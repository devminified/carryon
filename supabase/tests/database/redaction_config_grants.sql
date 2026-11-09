-- CO-306: redaction_config's own migration (CO-301) never granted
-- service_role access to it — the same "BYPASSRLS exempts RLS checks
-- but GRANT is a separate, still-required privilege" gap
-- 20261106002756_grant_service_role.sql's own comment describes, just
-- missed for this one table. Found for real: get-config (which has
-- been live since CO-301) 500ed with 42501 insufficient_privilege the
-- first time it was actually exercised against a real local instance
-- as part of this ticket's own testing. Locking the fix in here so it
-- can't silently regress.
--
-- CO-410 found the other half of the same gap: that original fix only
-- covered SELECT (all get-config/rescan ever needed) — the admin
-- Config page is the first thing to ever WRITE to this table, and its
-- own first real save 500'd with "permission denied for table
-- redaction_config" until CO-410's migration granted insert/update/
-- delete too.
begin;
select plan(5);

select ok(
  has_table_privilege('service_role', 'redaction_config', 'SELECT'),
  'service_role can select from redaction_config'
);
select ok(
  has_table_privilege('service_role', 'redaction_config', 'UPDATE'),
  'service_role can update redaction_config'
);
select ok(
  has_table_privilege('service_role', 'redaction_config', 'INSERT'),
  'service_role can insert into redaction_config'
);
set local role service_role;
select lives_ok(
  $$select disabled_rule_ids, updated_at from redaction_config where id = 'default'$$,
  'a service-role read of redaction_config succeeds'
);
select lives_ok(
  $$update redaction_config set pinned_version = '1.0.0-test' where id = 'default'$$,
  'a service-role write to redaction_config succeeds'
);
reset role;

select * from finish();
rollback;

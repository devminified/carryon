-- CO-301: get-config lets rules be disabled and the path denylist
-- extended from the server without a new binary release. One row is
-- enough — there's no per-project redaction config in the current
-- build plan (CLAUDE.md: no speculative scale) — RLS stays enabled
-- with no policies (CO-102's own deny-all pattern): get-config reads
-- this with the service role, same as every other function in this
-- phase; nothing reads or writes it directly over PostgREST.
--
-- disabled_rule_ids/denylist_additions are plain text arrays, never raw
-- regex: get-config can narrow coverage (disable a rule) or extend the
-- denylist, but can never supply a pattern this binary would execute —
-- see internal/redact's Rule doc comment for why that boundary matters.

create table redaction_config (
  id text primary key default 'default',
  disabled_rule_ids text[] not null default '{}',
  denylist_additions text[] not null default '{}',
  updated_at timestamp with time zone not null default now()
);

alter table redaction_config enable row level security;

insert into redaction_config (id) values ('default');

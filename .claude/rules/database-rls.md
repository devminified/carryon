# Database & RLS

## Migrations

- All schema changes are SQL migrations under `supabase/migrations/`. Never click around in the Supabase dashboard to change schema.
- Filename format: `YYYYMMDDHHMMSS_short_description.sql`.
- Each migration enables RLS on every new table **in the same file** that creates it.
- `supabase db lint` and a full apply-to-fresh-database run gate every PR (see `.claude/rules/testing.md`).

## Schema (current build plan — CO-102)

A project **is** the tenant boundary here: there's no separate organization/`tenant_id` column. Access is scoped by `project_members` membership, checked via `has_role()` below. Six tables, exactly as CO-102 specifies (Doc §"Database recommendation"):

| Table | Primary key | Notes |
| --- | --- | --- |
| `projects` | `id` | one row per registered repo; `git_remote` unique; `team_viewer`/`team_resumer`/`team_admin` (added by CO-303's migration) are GitHub's own stable numeric team IDs, nullable — a project with none set stays 100% manually managed; `bucket` (nullable text, added by CO-305's migration) is a dedicated Storage bucket for a client whose contract requires physical separation — null means the project uses the shared default bucket (`carryon-sessions`); creating the bucket itself and setting this column are both manual, one-time ops actions, not automated by any Edge Function; `closed_at` (nullable timestamptz, added by CO-307's migration) and `retention_days_after_close` (int, default 90) drive the daily retention sweep — null `closed_at` means active, never touched; setting `closed_at` is itself a manual, one-time admin action today, same shape as `bucket` — there's no admin UI yet (Phase 4) to drive it from; `daily_token_budget` (int, default 200000, added by CO-407's migration) caps `summarize-session`'s own daily Anthropic spend per project (real input+output tokens, summed from `ai_usage`, not dollars) — same manual-tuning shape as `retention_days_after_close`, no admin UI yet either |
| `project_members` | `(project_id, user_id)` | `role` is `viewer` / `resumer` / `admin`, mirrored from GitHub teams (CO-303); `source` (`'manual'` \| `'sync'`, added by the same migration) marks whether a row was an admin's own direct action or the sync job's — the sync job only ever writes/removes `'sync'` rows, so a manual override always survives the next sync |
| `sessions` | `id` | `id` is Claude Code's own `session_id`; `parent_session_id` set on a pulled fork; `ended_reason` (`'explicit'` \| `'idle'`, added by CO-111's migration) distinguishes a real `SessionEnd` from the idle sweep below; `search_vector` (generated `tsvector` over `title`+`summary`, GIN-indexed, added by CO-402's migration) backs the web session list's free-text search — a generated column rather than an expression index, so a cursor-paginated search query can reference it directly instead of repeating `to_tsvector(...)` in every clause; the table is also added to the `supabase_realtime` publication by that same migration, so the web list updates live — RLS still gates what a given subscriber actually receives, same as any other read; `title_source` (`'ai'` \| `'manual'`, default `'ai'`, added by CO-405's migration) is set to `'manual'` by every real rename path (`rename-session`, the web app's `renameSessionAction`) — `summarize-session` (CO-407) checks this is still not `'manual'` before ever overwriting `title`; `pinned` (boolean, default `false`, same migration) sorts a project's session list pinned-first (`sessions_pinned_last_activity_idx`); `blocked_reason` (nullable text, added by CO-407's migration) is only ever set alongside `outcome = 'blocked'`; `summarized_through_seq` (nullable int, same migration) tracks how far into the session's main stream `summarize-session` has already condensed and summarized — null means never summarized, and it's what makes that job's own incremental mode (CO-406) possible |
| `session_chunks` | `(session_id, stream_id, seq)` | append-only, grows forever within a session; `stream_id` is `'main'` for the session's own transcript or a subagent's `agent_id` for a child stream (CO-109/CO-110 — added by a later migration than CO-102's original `(session_id, seq)`, once a real subagent chunk would otherwise have collided with the main stream's `seq` numbering); `last_rescanned_at` (nullable timestamptz, added by CO-306's migration) is the one narrow exception to this table's append-only-bytes rule — `rescan` overwrites a chunk's object in place and updates its `sha256` when a hit is found, since the whole point is closing a security hole, not preserving a byte history of it |
| `code_snapshots` | `(session_id, seq)` | `wip_ref` (hidden git ref) or `object_key` (bundle fallback, CO-202); `tree_sha` (added by a later migration than CO-102's original columns, CO-201) is the pushed commit's tree object, letting a retry compare against what's already registered without re-deriving it server-side |
| `audit_log` | `id bigserial` | append-only; RLS blocks `DELETE` for everyone but the DB owner; `project_id`/`ip`/`user_agent` (added by CO-304's migration) — `project_id` is deliberately **not** a foreign key, same as the original `session_id` column, so a row outlives the project/session it's about once CO-307's retention job starts deleting rows (an enforced FK here blocked every project delete in the real local e2e pull test — fixed by removing it, not by cascading); `reason` (nullable text, added by CO-307's migration) is the mandatory reason an admin gives to `carryon admin delete <sid> --reason` — the only other current writer of it; every other action type leaves it null |
| `ai_usage` | `id bigserial` | added by CO-407's migration; append-only (I2) — one row per real Anthropic API call `summarize-session` makes, successful or not; `project_id`/`session_id` **are** real foreign keys (`session_id` nullable, `on delete set null` — unlike `audit_log`, a usage row isn't itself about the session's own data retention, so there's no CO-307-style delete conflict to avoid); `cost_usd` is informational only, from a documented-as-approximate static rate (`_shared/anthropic.ts`) — the daily budget cap itself sums real token counts, not this column |
| `project_notification_settings` | `project_id` | added by CO-409's migration; one optional row per project — `slack_webhook_url` (nullable text) and `notify_digest` (boolean, default false). **Not** columns on `projects` itself, unlike `bucket`/`daily_token_budget` (readable by any viewer): a webhook URL lets its holder *post* into the team's Slack channel, not just read something, so this table's own RLS is admin-only in every direction (select/insert/update/delete), never widening `members_can_read_their_projects`. A missing row means "notifications off" — every reader (`summarize.ts`, `rescan/index.ts`, `send-daily-digest/index.ts`) treats `maybeSingle()` coming back null that way, not as an error. No admin UI sets this yet (CO-410's own planned page) — same manual-SQL-for-now shape `bucket`/`daily_token_budget`/`closed_at` already have. |

Indexes (CO-102): `sessions(project_id, last_activity_at desc)`, `sessions(user_id)`, `session_chunks(session_id, stream_id, seq)` (via the primary key). CO-304 adds a partial unique index enforcing "upload" is logged at most once per session per UTC day (`audit_log_one_upload_per_session_per_day`, keyed on `(session_id, (at at time zone 'utc')::date)` — `date(at)` alone isn't IMMUTABLE enough for an index, since its result depends on the session's timezone setting).

**Not part of the current build plan:** `tenant_id`, partitioning, `session_manifests`/compaction, `leases`, and `outbox` are all Doc §"Scaling to millions of users" (Tier 2/3) concerns — real, but deliberately out of scope until a ticket actually calls for them (CLAUDE.md: "No speculative scale"). Don't add a `tenant_id` column or a leading-key convention pre-emptively; if a future ticket introduces multi-org tenancy, it'll say so and this file gets updated then, not before.

## Scheduled jobs that are pure SQL, not an Edge Function

CO-111's idle-session sweep (a session with no activity for 12 hours gets `status = 'ended', ended_reason = 'idle'`) is a `pg_cron` job calling a plain SQL function directly — `end_idle_sessions()` — not an Edge Function, because the whole state transition is a single `UPDATE` with no Storage/API call involved (see `.claude/rules/edge-functions.md`'s "no queue/outbox" pattern: this needs even less than an Edge Function). Two things any similar future job must get right, both already done here:

- **`revoke execute on function … from public`** right after creating it. Postgres grants `EXECUTE` on a new function to `PUBLIC` by default, which would otherwise let `anon`/`authenticated` trigger it over PostgREST at `/rest/v1/rpc/<function>` — a scheduled job's SQL is an implementation detail, not an API surface. Verified locally: after the revoke, `information_schema.routine_privileges` lists only `postgres`.
- **`security definer` + `set search_path = public`**, same as every other `SECURITY DEFINER` function in this file.

## RLS policies

Every table has RLS **enabled**. CO-102 shipped every table with RLS on and deny-all; CO-302 (`supabase/migrations/20261108010000_add_roles_and_rls.sql`) is what added the real per-role policies, built around one helper (exact signature per Doc §"Row-level security that stays fast"). The full table-by-role matrix (and how each cell is actually verified) lives in `docs/co-302-role-matrix.md`; note that doc predates CO-304, so its `audit_log` row (which still says "deliberately deferred") is superseded by this section.

`audit_log`'s own real policies (CO-304, `supabase/migrations/20261108030000_add_audit_log_columns_and_rls.sql`) are two separate `select` policies, not one: `has_role(project_id, 'admin')` for a project's admins (every row), and a second policy for a non-admin who owns the session a row is about (`session_id is not null and (select user_id from sessions where id = session_id) = auth.uid()`) — a `role_change`/`config_change` row has no `session_id` at all, so that second policy correctly never surfaces those to anyone but an admin. No insert policy exists (every current writer already holds the service role for an unrelated reason in the same request); no update/delete policy for anyone, including admins — append-only (I2), only CO-307's retention job (service role) ever removes a row.

`project_members.role` is a Postgres **enum**, not plain `text` — `viewer < resumer < admin` only holds under `>=` if declared in that order (`text` would sort `'admin' < 'resumer' < 'viewer'`, the opposite of what's intended):

```sql
create type project_role as enum ('viewer', 'resumer', 'admin'); -- order is significant
```

```sql
-- STABLE SECURITY DEFINER — evaluated once per statement, not once per row
create or replace function has_role(p_project_id uuid, p_min_role project_role)
returns boolean
language sql
security definer
stable
set search_path = public
as $$
  select exists (
    select 1 from project_members
    where project_id = p_project_id
      and user_id = auth.uid()
      and role >= p_min_role
  );
$$;
```

```sql
-- resumer+ can pull a session's chunks; viewers can list but not pull
create policy "resumer_can_read_chunks" on session_chunks
  for select to authenticated
  using (has_role((select project_id from sessions where id = session_id), 'resumer'));
```

- Every policy has a comment naming which row of the access table (Doc §"Who can do what" / `auth.md`) it implements.
- Policies are per role and per action (select/insert/update/delete); both `update` and `insert` policies carry `with check`.
- Never disable RLS to debug. Query as the role in question, or use the pgTAP suite.
- **Never** inline `EXISTS (select 1 from project_members …)` in a policy — always go through `has_role()`, so the stability guarantee (one lookup per statement) holds everywhere.

Every policy is tested with pgTAP for every role and every table (NFR-08, CO-302): a user without the role must get zero rows, never an error that leaks existence. Tests live under `supabase/tests/database/*.sql`, one file per table, run via `supabase test db supabase/tests/database --local` (wired into `ci.yml`'s `supabase` job). The standard harness for "run this query as role X": `set local role authenticated; set local request.jwt.claim.sub = '<uuid>';` inside the test's own transaction — `SET ROLE` (not just the session's original login role) is what RLS actually keys off, and `postgres` is permitted to assume `authenticated` the same way PostgREST's `authenticator` role does in production.

## Generated types

Run `supabase gen types typescript --linked > web/src/shared/types/database.types.ts` after every migration. Never edit this file by hand.

## Audit log

Append-only, populated on every view/pull/delete of session data (not just writes — reads of sensitive data are audited too, per Doc §"Access, security and culture"). RLS prevents `DELETE` for everyone except the database owner.

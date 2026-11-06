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
| `projects` | `id` | one row per registered repo; `git_remote` unique |
| `project_members` | `(project_id, user_id)` | `role` is `viewer` / `resumer` / `admin`, mirrored from GitHub teams (CO-303) |
| `sessions` | `id` | `id` is Claude Code's own `session_id`; `parent_session_id` set on a pulled fork; `ended_reason` (`'explicit'` \| `'idle'`, added by CO-111's migration) distinguishes a real `SessionEnd` from the idle sweep below |
| `session_chunks` | `(session_id, stream_id, seq)` | append-only, grows forever within a session; `stream_id` is `'main'` for the session's own transcript or a subagent's `agent_id` for a child stream (CO-109/CO-110 — added by a later migration than CO-102's original `(session_id, seq)`, once a real subagent chunk would otherwise have collided with the main stream's `seq` numbering) |
| `code_snapshots` | `(session_id, seq)` | `wip_ref` (hidden git ref) or `object_key` (bundle fallback, CO-202) |
| `audit_log` | `id bigserial` | append-only; RLS blocks `DELETE` for everyone but the DB owner |

Indexes (CO-102): `sessions(project_id, last_activity_at desc)`, `sessions(user_id)`, `session_chunks(session_id, stream_id, seq)` (via the primary key).

**Not part of the current build plan:** `tenant_id`, partitioning, `session_manifests`/compaction, `leases`, and `outbox` are all Doc §"Scaling to millions of users" (Tier 2/3) concerns — real, but deliberately out of scope until a ticket actually calls for them (CLAUDE.md: "No speculative scale"). Don't add a `tenant_id` column or a leading-key convention pre-emptively; if a future ticket introduces multi-org tenancy, it'll say so and this file gets updated then, not before.

## Scheduled jobs that are pure SQL, not an Edge Function

CO-111's idle-session sweep (a session with no activity for 12 hours gets `status = 'ended', ended_reason = 'idle'`) is a `pg_cron` job calling a plain SQL function directly — `end_idle_sessions()` — not an Edge Function, because the whole state transition is a single `UPDATE` with no Storage/API call involved (see `.claude/rules/edge-functions.md`'s "no queue/outbox" pattern: this needs even less than an Edge Function). Two things any similar future job must get right, both already done here:

- **`revoke execute on function … from public`** right after creating it. Postgres grants `EXECUTE` on a new function to `PUBLIC` by default, which would otherwise let `anon`/`authenticated` trigger it over PostgREST at `/rest/v1/rpc/<function>` — a scheduled job's SQL is an implementation detail, not an API surface. Verified locally: after the revoke, `information_schema.routine_privileges` lists only `postgres`.
- **`security definer` + `set search_path = public`**, same as every other `SECURITY DEFINER` function in this file.

## RLS policies

Every table has RLS **enabled**. CO-102 ships every table with RLS on and **deny-all** (no policies yet — the anon/authenticated key gets zero rows everywhere). CO-302 is what adds the real per-role policies, built around one helper (exact signature per Doc §"Row-level security that stays fast"):

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

Every policy is tested with pgTAP for every role and every table (NFR-08, CO-302): a user without the role must get zero rows, never an error that leaks existence.

## Generated types

Run `supabase gen types typescript --linked > web/src/shared/types/database.types.ts` after every migration. Never edit this file by hand.

## Audit log

Append-only, populated on every view/pull/delete of session data (not just writes — reads of sensitive data are audited too, per Doc §"Access, security and culture"). RLS prevents `DELETE` for everyone except the database owner.

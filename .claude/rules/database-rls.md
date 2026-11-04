# Database & RLS

## Migrations

- All schema changes are SQL migrations under `supabase/migrations/`. Never click around in the Supabase dashboard to change schema.
- Filename format: `YYYYMMDDHHMMSS_short_description.sql`.
- Each migration enables RLS on every new table **in the same file** that creates it.
- `supabase db lint` and a full apply-to-fresh-database run gate every PR (see `.claude/rules/testing.md`).

## Tenant model

Every table carries `tenant_id` (the organization) as the **leading column** of its primary key and every index — this is cheap at today's scale and is what lets a tenant move to another database cell later without a query rewrite (Doc §"Multi-tenancy from day one"). The tenant ID is also a custom claim in the user's JWT, so RLS filters on it without a join.

Core tables (see Doc §"Tables, keys and partitioning" for the full set and partitioning plan):

| Table | Primary key | Notes |
| --- | --- | --- |
| `projects` | `id` | one row per registered repo; `git_remote` unique |
| `project_members` | `(project_id, user_id)` | `role` is `viewer` / `resumer` / `admin`, mirrored from GitHub teams |
| `sessions` | `(tenant_id, id)` | `id` is Claude Code's own `session_id` |
| `session_chunks` | `(tenant_id, session_id, generation, seq)` | append-only; monthly range partition once volume justifies it |
| `session_manifests` | `(tenant_id, session_id, generation)` | compacted chunk list for ended sessions |
| `code_snapshots` | `(session_id, seq)` | `wip_ref` (hidden git ref) or `object_key` (bundle fallback) |
| `leases` | `(tenant_id, session_id)` | enforces I7, one writer per session |
| `audit_log` | `(tenant_id, id)` | append-only; RLS blocks `DELETE` for everyone but the DB owner |
| `outbox` | `(id)` | short-lived; see `edge-functions.md` |

## RLS policies

Every table has RLS **enabled**. No exceptions. Every policy calls a single helper:

```sql
-- STABLE SECURITY DEFINER — evaluated once per statement, not once per row
create or replace function has_role(p_tenant_id uuid, p_project_id uuid, p_min_role text)
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
      and role >= p_min_role  -- role ordering: viewer < resumer < admin
  );
$$;
```

```sql
-- resumer+ can pull a session's chunks; viewers can list but not pull
create policy "resumer_can_read_chunks" on session_chunks
  for select to authenticated
  using (has_role(tenant_id, (select project_id from sessions where id = session_id), 'resumer'));
```

- Every policy has a comment naming which row of the access table (Doc §"Who can do what" / `auth.md`) it implements.
- Policies are per role and per action (select/insert/update/delete); both `update` and `insert` policies carry `with check`.
- Never disable RLS to debug. Query as the role in question, or use the pgTAP suite.
- **Never** inline `EXISTS (select 1 from project_members …)` in a policy — always go through `has_role()`, so the stability guarantee (one lookup per statement) holds everywhere.

Every policy is tested with pgTAP for every role and every table (NFR-08, CO-302): a user without the role must get zero rows, never an error that leaks existence.

## Generated types

Run `supabase gen types typescript --linked > web/src/shared/types/database.types.ts` after every migration. Never edit this file by hand.

## Compaction

When a session ends, a background job (queue: `compact`) writes one manifest object listing every chunk (byte range, SHA-256, chain head), optionally merges small chunk objects into larger ones, and replaces the session's `session_chunks` rows with one `session_manifests` row. Don't write a query that assumes chunk rows exist for an ended session — always check `sessions.status` and fall back to the manifest.

## Audit log

Append-only, populated on every view/pull/delete of session data (not just writes — reads of sensitive data are audited too, per Doc §"Access, security and culture"). RLS prevents `DELETE` for everyone except the database owner.

# CO-302 — Roles and Row-Level Security: role/action matrix

This is CO-302's validation-point deliverable: "a matrix of roles against
actions is reviewed and signed off by the CTO." It's the actual RLS
policies added in `supabase/migrations/20261108010000_add_roles_and_rls.sql`,
written out as a table, plus how each row is verified. **Needs explicit
CTO sign-off before this is considered fully done — this file states what
was built; it isn't itself the sign-off.**

> **Superseded in part by CO-304.** This snapshot predates CO-304 ("Audit
> log"), which added `audit_log`'s real select policies. The `audit_log`
> row below (still showing CO-302's original "deliberately deferred"
> state) is now historical — see `.claude/rules/database-rls.md`'s RLS
> section for the real, current policy.
>
> **Also superseded in part by CO-405** ("Rename and pin"), which added
> `admins_can_update_sessions` — the `sessions` (update) row below is
> updated in place to reflect it, rather than left historical, since
> it's a one-cell change to an otherwise-still-accurate table.

Three roles per project, in ascending order: `viewer < resumer < admin`.
"Owner" means `sessions.user_id = auth.uid()` — a developer can always
read and manage their own sessions regardless of their project role
(auth.md: "a developer can always read their own sessions").

| Table | viewer | resumer | admin | non-member | owner (any role) |
| --- | --- | --- | --- | --- | --- |
| `projects` (select) | ✅ | ✅ | ✅ | ❌ (0 rows) | n/a |
| `projects` (insert/update/delete) | ❌ | ❌ | ❌ | ❌ | n/a — bootstrapped via `register-project`'s service role, see below |
| `project_members` (select) | ✅ | ✅ | ✅ | ❌ | n/a |
| `project_members` (insert/update/delete) | ❌ | ❌ | ✅ | ❌ | n/a |
| `sessions` (select) | ✅ | ✅ | ✅ | ❌ | ✅ (always, even below viewer on the project — there's no such state, but the policy is `OR`, not `AND`) |
| `sessions` (insert) | ❌ (unless also the owner) | ❌ (unless also the owner) | ❌ (unless also the owner) | ❌ | ✅ (for themselves only) |
| `sessions` (update) | ❌ (unless also the owner) | ❌ (unless also the owner) | ✅ (CO-405: `admins_can_update_sessions` — rename/pin any session in their project, row-level not column-level) | ❌ | ✅ (their own session only) |
| `sessions` (delete) | ❌ | ❌ | ✅ | ❌ | ❌ (not even the owner — delete is admin-only) |
| `session_chunks` (select) | ❌ | ✅ | ✅ | ❌ | n/a |
| `session_chunks` (insert) | ❌ | ❌ (unless also the owner) | ❌ (unless also the owner) | ❌ | ✅ (their own session only) |
| `session_chunks` (update/delete) | ❌ | ❌ | ❌ | ❌ | ❌ — append-only (I2), no exception for anyone, ever |
| `code_snapshots` (select) | ❌ | ✅ | ✅ | ❌ | n/a |
| `code_snapshots` (insert/update) | ❌ | ❌ (unless also the owner) | ❌ (unless also the owner) | ❌ | ✅ (their own session only; update exists because a retry upserts — NFR-05) |
| `code_snapshots` (delete) | ❌ | ❌ | ❌ | ❌ | ❌ — no delete policy for anyone |
| `audit_log` (select/insert/update/delete) | ❌ | ❌ | ❌ | ❌ | ❌ — deliberately deferred to CO-304 (depends on this ticket); every current writer uses the service role |
| `storage.objects` (carryon-sessions bucket, select) | ❌ | ✅ | ✅ | ❌ | n/a |
| `storage.objects` (insert/update/delete) | ❌ | ❌ | ❌ | ❌ | ❌ — every write goes through a service-role-signed URL, never direct RLS-checked access |

## Where the service role still legitimately bypasses RLS

Per `architecture.md`'s own rule ("the service role is for two things
only: public ingress that must bypass RLS, and admin operations RLS
can't express"), these stay on the service role even though RLS now
exists for the tables involved:

- **`register-project`** — bootstraps a project's first (admin) member;
  no policy can authorize "you're about to become this project's first
  admin" because there's no row yet for any policy to apply to.
- **`match-project`** — must tell "unregistered" apart from
  "registered, but you're not on it" across *every* project by git
  remote, which is inherently cross-project and can't be scoped to the
  caller's own membership.
- **`register-chunk` / `confirm-chunk` / `register-snapshot` /
  `sign-snapshot-bundle`** — all sign Storage upload URLs (not
  expressible via RLS at all), and `register-chunk` additionally
  creates a session's very first row before any session-scoped policy
  has anything to check against. Their own manual membership/ownership
  checks don't add a rule beyond what the matching RLS policy already
  states — see each function's own updated comment.
- **`list-sessions` / `get-manifest` / `register-fork`** — these moved
  their data reads (and, for `register-fork`, the write) to the
  caller's own client, relying on RLS directly; only the
  `auth.admin.getUserById` GitHub-login lookups (the `auth` schema
  isn't exposed to PostgREST — see `auth.md`) and `get-manifest`'s
  signed download URLs stay on the service role.
- **`end-session`** — the update itself now runs as the caller
  (`owner_can_update_their_sessions`); only the existence check (to
  report a real 404) stays on the service role.

## How each row above is actually verified

Every ✅/❌ cell is backed by a real pgTAP test under
`supabase/tests/database/` (run in CI via `supabase test db`, `ci.yml`'s
`supabase` job), not just asserted here:

- `projects_rls.sql`, `project_members_rls.sql`, `sessions_rls.sql`,
  `session_chunks_rls.sql`, `code_snapshots_rls.sql`, `audit_log_rls.sql`
  — one file per table, every role plus a non-member, both the allowed
  and the refused operations.
- `storage_objects_rls.sql` — the resumer+ gate on the bucket, plus the
  ticket's own "path-traversal attempts on Storage keys are refused"
  point: a name with no real project-id-shaped prefix resolves to zero
  rows (via `storage_object_project_id()`'s safe-cast guard), never an
  error that could leak whether some other object exists.
- `membership_revocation.sql` — the ticket's "a user removed from a
  project loses access on their next request" point: revoking a
  `project_members` row and immediately re-querying as that same user,
  in the same session, shows zero rows with nothing to invalidate or
  expire (`has_role()` is `STABLE`, not cached across statements).

**The ticket's other validation point** — "an external reviewer cannot
read another project's session with a valid token for a different
project" — is covered by the same mechanism every table's non-member
case already proves (a valid token for project A's membership getting
zero rows when querying project B's data), run for real against a
local instance via every `*_rls.sql` file's non-member assertions. That
is the practical equivalent of the stated check; a live, manual
pentest-style attempt with two real accounts is still worth doing
separately if the CTO wants to see it firsthand before signing off.

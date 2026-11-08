# CO-307 — Retention and deletion: dry-run report

This is CO-307's validation-point deliverable: "a dry-run report lists what
would be deleted before the first real run, and the CTO approves it."
**Needs explicit CTO sign-off before the real nightly job is ever scheduled
against production data** — this file states what was built and how to run
the report; it isn't itself the sign-off.

## How to run a dry-run report

`purge-expired-sessions` accepts `{"dryRun": true}` and walks the exact same
selection logic a real run would (closed projects whose
`closed_at + retention_days_after_close` has passed), but never calls
Storage's `remove()` or deletes a single row:

```bash
curl -X POST "$SUPABASE_URL/functions/v1/purge-expired-sessions" \
  -H "x-cron-secret: $PURGE_CRON_SECRET" \
  -H "Content-Type: application/json" \
  -d '{"dryRun": true}'
```

Response shape:

```json
{
  "dryRun": true,
  "projectsProcessed": 2,
  "sessionsDeleted": 14,
  "report": [
    { "projectId": "...", "sessionsDeleted": 9, "objectsRemoved": 23 },
    { "projectId": "...", "sessionsDeleted": 5, "objectsRemoved": 11 }
  ]
}
```

`sessionsDeleted`/`objectsRemoved` in a dry run mean "would be deleted" —
nothing is touched. Run this against the real production project (with the
real `PURGE_CRON_SECRET`/Vault secret already set up per the migration's own
comment) before the `purge-expired-sessions` cron schedule is ever allowed to
fire for real, and again after any change to a project's
`retention_days_after_close` or `closed_at`.

## What the real run does, for the CTO to review before approving

| Step | Detail |
| --- | --- |
| Scope | Only projects with `closed_at` set (an admin's own one-time action — see `database-rls.md`) AND past their own `retention_days_after_close` window. An active project (`closed_at` null) is never even considered. |
| Deletes | Every `sessions` row for that project (cascades to `session_chunks`/`code_snapshots` via the existing FK), their Storage objects (chunks + bundle snapshots), and every `audit_log` row for that project. |
| Does NOT delete | The `projects`/`project_members` rows themselves — only past-retention session data and its audit trail. A closed project's registration and membership survive. |
| Does NOT delete | `refs/carryon/<user>/<session>` git refs on the developer's own remote — flagged, not silently skipped. Carryon's real GitHub App only has org `Members: Read-only`; doing this for real would require expanding that live App's permissions to `Contents: Read & Write` across every installed repo, which the user explicitly asked to defer rather than have done silently as part of this ticket. A stale ref is inert and low-severity, not a correctness bug — see `rescan`/`purge-expired-sessions`'s own code comments. |
| Ordering/safety | Storage objects are removed first; the DB rows are only deleted if that succeeds (a storage failure skips the whole project for this run, leaving everything as-is for a retry). Sessions are deleted before `audit_log`, so a partial failure never leaves the audit trail gone while the data it describes still exists. |
| Isolation | One project's failure is logged and skipped — never aborts the rest of the run. |

## Single-session admin delete (separate from the nightly sweep)

`carryon admin delete <session-id> --reason "..."` (admin-only, mandatory
reason) is immediate and explicit — not part of the dry-run/retention sweep
above. Verified for real against local Supabase: the session row, its chunk
object, and (if any) its bundle snapshot object are removed; a fork of the
deleted session survives, reparented to no parent; the `audit_log` row
carrying the reason is the only remaining record of the deleted session.

## Real verification performed (local Supabase only)

- An expired project (closed 100 days ago, 90-day retention) with a real
  planted chunk object: a real run fully purged it — session row, chunk row,
  Storage object, and every audit_log row for the project, all confirmed
  gone by direct query/Storage check afterward.
- An active project (`closed_at` null): untouched by the same run.
- A closed project still within its retention window: untouched.
- Re-running the sweep against an already-purged project: idempotent,
  `sessionsDeleted: 0`, no error.
- `dryRun: true` against a real expired project: reported the same counts a
  real run would, verified nothing was actually deleted afterward.
- The real `carryon admin delete` path (`cmd/carryon.TestAdminDeleteSessionEndToEnd`):
  a mandatory-reason refusal, a non-admin refusal, and a successful admin
  delete that removes the Storage object, reparents a real fork to no
  parent, and leaves the audit_log row with the given reason.

Per standing instruction, no real secrets/credentials were configured
against Supabase Cloud/staging — every verification above ran against local
Supabase only (`127.0.0.1:54421`/`:54422`).

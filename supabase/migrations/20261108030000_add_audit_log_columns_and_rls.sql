-- CO-304: the real audit_log columns and RLS, replacing CO-102's
-- deny-all (CO-302's own migration deliberately left this table alone
-- for exactly this ticket to finish).
--
-- Honest status on the ticket's 7-action vocabulary: 4 are wired up by
-- this ticket (upload — confirm-chunk, once per session per day;
-- pull_manifest — get-manifest, pre-existing; pull_complete —
-- confirm-pull, new; role_change — sync-github-teams, pre-existing).
-- The other 3 have no caller yet because the feature that would fire
-- them doesn't exist yet: "view" needs Phase 4's web transcript viewer,
-- "delete" needs CO-307's actual deletion flow, "config_change" needs a
-- future admin UI for redaction_config. The CHECK-free `action text`
-- column already accepts any of the 7 — adding the real call site is
-- that future ticket's job, not a schema change.
-- No foreign key on project_id, deliberately matching CO-102's own
-- session_id column (also a bare uuid, no "references sessions") —
-- audit_log must outlive the project/session it's about once CO-307's
-- retention job starts deleting rows, and a future admin.md-promised
-- deletion flow must never be blocked by its own audit trail the way
-- it would be with an enforced FK here (found for real: an earlier
-- version of this migration added "references projects", which then
-- made every project delete in the real local e2e pull test fail with
-- "violates foreign key constraint audit_log_project_id_fkey").
alter table audit_log add column project_id uuid;
alter table audit_log add column ip text;
alter table audit_log add column user_agent text;

-- "Only admins can read the log. A developer can read the rows that
-- concern their own sessions, to see who pulled them" (ticket's own
-- wording) — two policies, not one, since the two groups see different
-- slices: an admin sees every row for their project regardless of
-- whose session it's about; a non-admin sees only rows tied to a
-- session they themselves own (role_change/config_change rows have no
-- session_id at all, so this policy correctly never surfaces those to
-- a non-admin).
create policy "admins_can_read_their_project_audit_log" on audit_log
  for select to authenticated
  using (has_role(project_id, 'admin'));

create policy "developers_can_read_audit_log_about_their_own_sessions" on audit_log
  for select to authenticated
  using (
    session_id is not null
    and (select user_id from sessions where id = session_id) = auth.uid()
  );

-- No insert policy: every current writer (get-manifest, confirm-chunk,
-- confirm-pull, sync-github-teams) already holds the service role for
-- an unrelated reason in the same request (signing a URL, verifying
-- Storage, calling the GitHub API), so there's no caller-client insert
-- path that would benefit from one yet — see CO-302's own precedent of
-- not building policies nothing currently needs (CLAUDE.md: no
-- speculative scale). Add one if a future ticket genuinely needs a
-- caller-client audit write.
--
-- No update or delete policy for anyone, including admins — append-
-- only (I2), same as CO-102's own "RLS blocks DELETE for everyone but
-- the DB owner." Only CO-307's retention job (service role, bypasses
-- RLS entirely) ever removes a row.

-- "upload" is logged once per session per day (ticket's own wording,
-- to limit volume) — enforced here, not just in application code, so
-- a retried confirm-chunk call can never double-log even under a race:
-- the writer (confirm-chunk) attempts the insert unconditionally and
-- treats a unique-violation on this index as "already logged today,"
-- the same pattern NFR-05/I3's other idempotent-insert paths use.
--
-- date(timestamptz) is only STABLE (its result depends on the session's
-- timezone setting), which Postgres refuses in an index expression —
-- "(at at time zone 'utc')::date" pins the zone to a fixed literal, so
-- the same timestamptz always produces the same date regardless of
-- who's connected or how, which is what makes it IMMUTABLE enough to
-- index. "Per day" here means per UTC day, not per caller's local day.
create unique index audit_log_one_upload_per_session_per_day
  on audit_log (session_id, (((at at time zone 'utc')::date)))
  where action = 'upload';

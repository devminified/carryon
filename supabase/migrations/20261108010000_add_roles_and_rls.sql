-- CO-302: real per-role RLS policies, replacing CO-102's deny-all
-- defaults. Three roles per project (viewer < resumer < admin, in that
-- declared enum order — see CO-102's own comment on project_role) via
-- one STABLE SECURITY DEFINER helper, has_role(), evaluated once per
-- statement rather than once per row.
--
-- has_role() runs with its owner's privileges (security definer), so
-- its own read of project_members bypasses that table's RLS — this is
-- what lets project_members' own policies call has_role() without
-- recursing into themselves (database-rls.md's "never inline EXISTS
-- (select 1 from project_members …) in a policy — always go through
-- has_role()" is exactly this property, not just a style preference).
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

-- ---------------------------------------------------------------------
-- projects
-- ---------------------------------------------------------------------
-- Any member (viewer+) can see their own projects' metadata. No
-- insert/update/delete policy: register-project (CO-104) bootstraps a
-- brand-new project's first (admin) member atomically via the service
-- role — there's no RLS-expressible rule for "let this request through
-- because it's about to make itself the first admin," the same kind of
-- admin-operation architecture.md carves out for the service role.
-- Direct writes stay denied for every role until a ticket genuinely
-- needs one (CLAUDE.md: no speculative scale).
create policy "members_can_read_their_projects" on projects
  for select to authenticated
  using (has_role(id, 'viewer'));

-- ---------------------------------------------------------------------
-- project_members
-- ---------------------------------------------------------------------
-- Any member can see the full member list of their own project (who's
-- viewer/resumer/admin) — same visibility tier as the project itself.
create policy "members_can_read_their_project_members" on project_members
  for select to authenticated
  using (has_role(project_id, 'viewer'));

-- Admins manage membership directly. CO-303's sync-github-teams job
-- still uses the service role regardless (it reconciles membership
-- across every project it manages in one pass, which exceeds what "I'm
-- an admin of this one project" can express) — this policy is for an
-- admin's own direct action (e.g. a future admin UI), not that job.
create policy "admins_can_add_members" on project_members
  for insert to authenticated
  with check (has_role(project_id, 'admin'));

create policy "admins_can_change_member_roles" on project_members
  for update to authenticated
  using (has_role(project_id, 'admin'))
  with check (has_role(project_id, 'admin'));

create policy "admins_can_remove_members" on project_members
  for delete to authenticated
  using (has_role(project_id, 'admin'));

-- ---------------------------------------------------------------------
-- sessions
-- ---------------------------------------------------------------------
-- A developer can always read their own sessions (ticket's own wording),
-- and any project member (viewer+) can see every session in a project
-- they belong to (auth.md: "Sees session list: Yes" for all three roles).
create policy "owner_or_member_can_read_sessions" on sessions
  for select to authenticated
  using (user_id = auth.uid() or has_role(project_id, 'viewer'));

-- A session is always created by (and for) its own capturing agent —
-- register-chunk's first-chunk upsert and register-fork's new-fork row
-- both set user_id from the caller's own JWT, never a client-supplied
-- field (edge-functions.md), so "insert for yourself" is the only rule
-- RLS needs to add on top of that.
create policy "owner_can_create_their_sessions" on sessions
  for insert to authenticated
  with check (user_id = auth.uid());

-- Only the owning developer updates their own session's live state
-- (confirm-chunk's activity/byte-count bump, end-session's status
-- flip, a future rename) — no other role has a write right here per
-- auth.md's access table (resumer/admin's extra rights are read/pull/
-- delete, never "edit someone else's session").
create policy "owner_can_update_their_sessions" on sessions
  for update to authenticated
  using (user_id = auth.uid())
  with check (user_id = auth.uid());

-- Admin-only delete (auth.md: "Deletes: Yes, with a reason in
-- audit_log"). CO-307 builds the actual delete flow and the
-- audit-logged "reason" on top of this gate; this is just the gate.
create policy "admins_can_delete_sessions" on sessions
  for delete to authenticated
  using (has_role(project_id, 'admin'));

-- ---------------------------------------------------------------------
-- session_chunks (no project_id column — go through the owning session)
-- ---------------------------------------------------------------------
-- Pulling/resuming needs resumer+ (auth.md), matching get-manifest's own
-- check. A viewer can see a session exists and read its transcript
-- through a future server-side viewer (Phase 4's web app, itself
-- service-role + signed URLs, same as get-manifest) but never raw chunk
-- bytes directly off Storage or this table — see the storage.objects
-- policy below for the matching bytes-level gate.
create policy "resumer_can_read_chunks" on session_chunks
  for select to authenticated
  using (has_role((select project_id from sessions where id = session_id), 'resumer'));

-- Only the session's own owner uploads its chunks — not just "any
-- resumer of the project," since a teammate with resumer rights on the
-- project isn't the one capturing this particular session.
create policy "owner_can_upload_chunks" on session_chunks
  for insert to authenticated
  with check ((select user_id from sessions where id = session_id) = auth.uid());

-- No update/delete policy: session_chunks is append-only (I2) — a
-- chunk is never corrected or removed once confirmed. Deny-all for
-- every role is the correct, permanent state here, not a placeholder.

-- ---------------------------------------------------------------------
-- code_snapshots (same owning-session shape as session_chunks)
-- ---------------------------------------------------------------------
create policy "resumer_can_read_snapshots" on code_snapshots
  for select to authenticated
  using (has_role((select project_id from sessions where id = session_id), 'resumer'));

create policy "owner_can_create_snapshots" on code_snapshots
  for insert to authenticated
  with check ((select user_id from sessions where id = session_id) = auth.uid());

-- Unlike chunks, a snapshot's (session_id, seq) row is genuinely
-- replaced on retry (CO-201/CO-202: "retrying the same (session_id,
-- seq) replaces the row instead of erroring" — NFR-05), so an upsert
-- needs both insert and update rights to succeed, not just insert.
create policy "owner_can_update_snapshots" on code_snapshots
  for update to authenticated
  using ((select user_id from sessions where id = session_id) = auth.uid())
  with check ((select user_id from sessions where id = session_id) = auth.uid());

-- ---------------------------------------------------------------------
-- audit_log
-- ---------------------------------------------------------------------
-- Deliberately no new policy here. CO-304 ("Audit log," depends on this
-- ticket) is what adds the real columns (project_id, ip, user_agent)
-- and the real select rule ("only admins can read the log; a developer
-- can read rows about their own sessions") — writing that policy now,
-- against today's narrower schema, would mean rewriting it again next
-- ticket. Every current writer already uses the service role (which
-- bypasses RLS) for its audit insert, so deny-all here changes nothing
-- functional yet; it's CO-304's job to open the right, final shape.

-- ---------------------------------------------------------------------
-- storage.objects — carryon-sessions bucket
-- ---------------------------------------------------------------------
-- Object keys are always server-derived as "<project_id>/<user_id>/
-- <session_id>/..." (_shared/chunks.ts, _shared/snapshots.ts), never
-- client-supplied, so there's no path-traversal vector through our own
-- registration endpoints. This policy is the matching bytes-level gate
-- for direct authenticated access to the bucket (as opposed to the
-- signed-URL path upload/download already uses, which Storage verifies
-- against the signed token itself, not this policy) — same resumer+
-- requirement as session_chunks/code_snapshots, so a direct call never
-- grants more than a signed URL already would.
--
-- storage_object_project_id() never raises on a malformed or
-- path-traversal-shaped name (a leading "..", a prefix that isn't a
-- UUID at all) — it returns null, and has_role(null, ...) is false
-- (project_members.project_id can't be null), so a crafted path is
-- refused cleanly rather than erroring the whole request.
create or replace function storage_object_project_id(object_name text)
returns uuid
language plpgsql
stable
set search_path = public
as $$
declare
  prefix text;
begin
  prefix := (storage.foldername(object_name))[1];
  if prefix is null or prefix !~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$' then
    return null;
  end if;
  return prefix::uuid;
exception
  when others then
    return null;
end;
$$;

create policy "resumer_can_read_session_objects" on storage.objects
  for select to authenticated
  using (
    bucket_id = 'carryon-sessions'
    and has_role(storage_object_project_id(name), 'resumer')
  );

-- No insert/update/delete policy on storage.objects: every write goes
-- through a service-role-signed URL (register-chunk/confirm-chunk,
-- sign-snapshot-bundle/register-snapshot), which Storage authorizes via
-- the signed token itself, not a caller's own RLS-checked session — see
-- redaction.md's signed-URL section. Direct authenticated writes stay
-- denied for every role.

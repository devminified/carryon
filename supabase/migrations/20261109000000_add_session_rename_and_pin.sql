-- CO-405: manual rename and admin pinning.
--
-- title_source distinguishes a developer's own /rename (or the web
-- app's rename) from an AI-written title — CO-407's not-yet-built
-- summarize-session must check `title_source = 'manual'` before ever
-- overwriting `title`, the same way a human edit always wins over a
-- machine one elsewhere in this schema (outcome/summary have no such
-- guard yet because nothing but the AI writes them). Defaults to 'ai'
-- so an existing untitled session (no manual rename yet) still reads
-- as "AI may title this," not "someone already named it."
alter table sessions
  add column title_source text not null default 'ai' check (title_source in ('ai', 'manual')),
  add column pinned boolean not null default false;

-- Admins can rename or pin any session in their project, on top of the
-- existing owner-only "owner_can_update_their_sessions" (CO-302), which
-- already lets an owner rename their own session (rename-session's own
-- role check allows either). Postgres RLS has no column-level policy
-- primitive, so — same accepted shape as admins_can_delete_sessions —
-- this row-level grant also technically permits an admin to update any
-- other column via a raw REST call; the real write paths (the
-- rename-session Edge Function, and the web app's renameSession/
-- pinSession Server Actions) only ever touch title/title_source/pinned.
create policy "admins_can_update_sessions" on sessions
  for update to authenticated
  using (has_role(project_id, 'admin'))
  with check (has_role(project_id, 'admin'));

-- Backs the session list's "pinned sessions sort first" ordering
-- (project_id, pinned desc, last_activity_at desc) — a direct index on
-- the new ORDER BY, same spirit as CO-102's original last-activity
-- index.
create index sessions_pinned_last_activity_idx
  on sessions (project_id, pinned, last_activity_at desc);

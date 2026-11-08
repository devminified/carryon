-- CO-305: storage hardening.

-- A project normally shares the default 'carryon-sessions' bucket
-- (bucket is null); a client whose contract requires physical
-- separation gets a dedicated bucket instead. Creating that bucket
-- itself is a manual, one-time ops action (same "external setup" shape
-- as the GitHub App in auth.md) — this migration only adds the column
-- every Edge Function needs to resolve which bucket a project's bytes
-- belong in; see supabase/functions/_shared/storage.ts's
-- resolveBucket().
alter table projects add column bucket text;

-- Object size limit, matching internal/upload's own 8MiB per-chunk
-- split threshold (maxUncompressedChunkBytes) with a safety margin —
-- not the bare 8MiB figure itself, since a single pathologically large
-- transcript line can't be split mid-line (the same known gap
-- redaction.md's "32MB lines aren't streamed" note already documents)
-- and redaction can slightly inflate a line's length. Compression
-- normally shrinks the actual uploaded bytes well below either number.
update storage.buckets set file_size_limit = 10 * 1024 * 1024 where id = 'carryon-sessions';

-- CO-302's original policy hardcoded bucket_id = 'carryon-sessions'.
-- Now that a project can have its own dedicated bucket, the policy
-- must work for any bucket whose objects follow the same
-- <project_id>/<user_id>/<session_id>/... path shape — the real
-- tenant-isolation boundary was always the path-derived project_id
-- plus has_role(), never the bucket name itself, so dropping the
-- bucket_id restriction doesn't weaken anything: an object in one
-- project's dedicated bucket is still only reachable by someone with
-- resumer+ on *that* project, regardless of which physical bucket it's
-- stored in.
drop policy "resumer_can_read_session_objects" on storage.objects;

create policy "resumer_can_read_session_objects" on storage.objects
  for select to authenticated
  using (has_role(storage_object_project_id(name), 'resumer'));

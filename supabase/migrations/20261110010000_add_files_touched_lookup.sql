-- CO-502: "who touched this file" lookup. The ticket's own literal
-- ask: index files_touched with a GIN index. A plain GIN index on an
-- array column (default array_ops) accelerates whole-element
-- containment queries (@>/&&/=) — a real, usable index for any direct
-- "this exact path is in the array" lookup.
create index sessions_files_touched_gin_idx on sessions using gin (files_touched);

-- find_sessions_by_file does both the exact AND the prefix case the
-- ticket's own testing point names ("Exact and prefix matches
-- (src/payments/) work") with one query: a plain starts-with check
-- (`left(f, length(p_path)) = p_path`, not LIKE) is true for an exact
-- match too (a string always starts with itself), so there's no
-- separate exact-mode flag to keep in sync with the index above.
-- Deliberately not `f like p_path || '%'` — a real file/directory name
-- can legitimately contain `%` or `_`, both LIKE wildcards, which
-- would silently turn an ordinary path like `src/my_file.ts` into a
-- pattern that also matches `src/myXfile.ts`; a plain prefix-length
-- comparison has no wildcard semantics to leak at all. Honest
-- tradeoff, not a speculative one: per-element prefix matching over an
-- unnested array does NOT benefit from the GIN index
-- above (array_ops accelerates whole-element containment, not pattern
-- matching within an element) — this is a sequential scan over each
-- matching row's own files_touched array. Acceptable at this
-- project's actual scale (CLAUDE.md: no speculative scale); if a
-- future ticket's real traffic makes this the bottleneck, the fix is
-- a normalized files table with a trigram index, built then.
--
-- Deliberately SECURITY INVOKER (the default, no `security definer`),
-- same reasoning as CO-501's search_sessions: this function returns
-- real `sessions` rows itself, so running it as the table owner would
-- silently bypass RLS. Called via the caller's own client from both
-- the web app (modules/sessions/data/queries.ts's findSessionsByFile)
-- and the CLI's `carryon ls --file <path>` (list-sessions Edge
-- Function) — RLS on `sessions` is what actually scopes a caller's
-- results in both cases. No `revoke execute ... from public` either,
-- for the same reason search_sessions has none: this should stay
-- callable by any authenticated caller over PostgREST.
create or replace function find_sessions_by_file(
  p_path text,
  p_project_id uuid default null,
  p_limit int default 50
)
returns setof sessions
language sql
stable
as $$
  select s.*
  from sessions s
  where (p_project_id is null or s.project_id = p_project_id)
    and exists (
      select 1 from unnest(s.files_touched) f
      where left(f, length(p_path)) = p_path
    )
  order by s.last_activity_at desc
  limit p_limit;
$$;

-- CO-501: semantic search. gte-small (_shared/embed.ts's own comment
-- has the full "why this model, not a paid API" reasoning) produces
-- 384-dimensional embeddings — the vector(384) columns below are
-- sized for that model specifically; changing models later means a
-- new migration that resizes both columns and re-embeds everything,
-- not a silent dimension mismatch.

create extension if not exists vector;

-- initial_schema.sql's own placeholder comment ("embedding vector(…)
-- — pgvector, CO-501. Not added until that ticket.") is this column.
alter table sessions add column embedding vector(384);

-- HNSW over IVFFlat: no training step needed (IVFFlat requires a
-- representative sample of rows to build its lists, awkward for a
-- table that starts empty and grows continuously), and better recall
-- at the write-heavy, read-occasional pattern search actually has
-- here. vector_cosine_ops matches embed.ts's own normalize:true (cosine
-- and dot product agree on normalized vectors; cosine is what the
-- search_sessions function below actually computes).
create index sessions_embedding_hnsw_idx on sessions
  using hnsw (embedding vector_cosine_ops);

-- Per-turn chunks of the condensed transcript (CO-406's own `turns`),
-- embedded incrementally alongside the session-level embedding above
-- — see _shared/summarize.ts's own comment on why passages accumulate
-- (keyed by a running per-session seq) rather than get wholesale
-- replaced the way sessions.embedding does: turns are append-only
-- (I2) once Claude Code writes them, so there's nothing to replace.
create table session_passages (
  id bigserial primary key,
  session_id uuid not null references sessions on delete cascade,
  seq int not null,
  text text not null,
  embedding vector(384) not null,
  created_at timestamptz not null default now(),
  unique (session_id, seq)
);

alter table session_passages enable row level security;

-- api.auto_expose_new_tables is off (config.toml) — same explicit
-- grant every new table in this codebase needs regardless of RLS.
grant select, insert, update, delete on session_passages to anon, authenticated;
grant select, insert, update, delete on session_passages to service_role;

create index session_passages_embedding_hnsw_idx on session_passages
  using hnsw (embedding vector_cosine_ops);
create index session_passages_session_id_idx on session_passages (session_id);

-- Same visibility rule as sessions' own "owner_or_member_can_read_
-- sessions" (CO-302) — a passage is exactly as visible as the session
-- it's a turn of, never more, never less. No insert/update/delete
-- policy for anyone but the service role (summarizeSession is the
-- only writer, same shape as ai_usage's own append-only table).
create policy "owner_or_member_can_read_session_passages" on session_passages
  for select to authenticated
  using (
    exists (
      select 1 from sessions s
      where s.id = session_passages.session_id
        and (s.user_id = auth.uid() or has_role(s.project_id, 'viewer'))
    )
  );

-- Hybrid search: vector similarity (session-level embedding, and the
-- single closest passage per session) blended with full-text rank
-- (search_vector, CO-402). Deliberately SECURITY INVOKER (the
-- default — no `security definer`) and NOT owned by a role with
-- BYPASSRLS: has_role()/is_any_project_admin() are boolean checks
-- *used inside* a policy, but this function itself SELECTs real rows
-- from sessions/session_passages, so running it as anyone other than
-- the caller would silently bypass RLS — the opposite of every other
-- SECURITY DEFINER helper in this codebase. The `search` Edge
-- Function calls this via the caller's own client specifically so
-- this inherited-RLS behavior is what actually runs.
--
-- Performance (the ticket's own "p95 under 500ms at 100k passages"
-- point): the nearest_passages CTE below does a plain `order by
-- embedding <=> query limit 200` — a real top-K ANN query the HNSW
-- index serves directly — *before* grouping to one best passage per
-- session, so the per-session dedup only ever sorts a bounded ~200
-- rows, not the full passage table. RLS still applies to every row
-- this function reads, at each table access, regardless of the CTE
-- nesting — verified for real in this migration's own pgTAP test.
create or replace function search_sessions(
  p_query_embedding vector(384),
  p_query_text text,
  p_project_id uuid default null,
  p_match_count int default 20
)
returns table (
  session_id uuid,
  score double precision,
  matching_passage text
)
language sql
stable
as $$
  with nearest_passages as (
    select sp.session_id, sp.text, sp.embedding <=> p_query_embedding as distance
    from session_passages sp
    order by sp.embedding <=> p_query_embedding
    limit 200
  ),
  best_passage as (
    select distinct on (session_id)
      session_id,
      text as passage_text,
      (1 - distance) as passage_score
    from nearest_passages
    order by session_id, distance asc
  )
  select
    s.id as session_id,
    (
      (1 - (s.embedding <=> p_query_embedding)) * 0.6
      + coalesce(bp.passage_score, 0) * 0.3
      + least(coalesce(ts_rank(s.search_vector, websearch_to_tsquery('english', p_query_text)), 0) * 2, 1) * 0.1
    ) as score,
    bp.passage_text as matching_passage
  from sessions s
  left join best_passage bp on bp.session_id = s.id
  where s.embedding is not null
    and (p_project_id is null or s.project_id = p_project_id)
  order by score desc
  limit p_match_count;
$$;

-- search_sessions is called via PostgREST's /rest/v1/rpc/search_sessions
-- (the `search` Edge Function's own caller-client call) — unlike
-- end_idle_sessions (database-rls.md's "pure SQL scheduled jobs"
-- section), this one *should* stay callable by any authenticated
-- user over PostgREST, so no `revoke execute ... from public` here;
-- RLS inside the function is what actually scopes a caller's results,
-- same reasoning as every RLS-scoped Edge Function in this codebase.

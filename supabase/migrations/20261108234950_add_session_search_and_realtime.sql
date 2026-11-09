-- CO-402: full-text search over a session's title+summary (both still
-- mostly null until CO-407 lands AI summaries — this is schema/index
-- work the search query already needs, not gated on that ticket), and
-- enabling Supabase Realtime so the web session list updates live
-- within NFR-02-style freshness without a client poll.

-- A generated column, not a plain index on an expression, so a cursor-
-- paginated search query can reference it directly in its WHERE/ORDER
-- BY without repeating the to_tsvector(...) expression everywhere.
alter table sessions add column search_vector tsvector
  generated always as (
    to_tsvector('english', coalesce(title, '') || ' ' || coalesce(summary, ''))
  ) stored;

create index sessions_search_vector_idx on sessions using gin (search_vector);

-- RLS already restricts who can SELECT a row (CO-302's
-- "owner_or_member_can_read_sessions"); Realtime's Postgres Changes
-- authorization re-checks that same policy per subscriber using their
-- own JWT, so adding this table to the publication doesn't widen access
-- — a non-member's subscription simply never receives a row.
alter publication supabase_realtime add table sessions;

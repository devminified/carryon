-- CO-408: summary display and re-run.
--
-- summarized_at is when summarize-session (CO-407) last wrote a real
-- summary — the web app's "AI-generated, <relative time>" badge reads
-- this; summarized_through_seq (CO-407) tracks byte/chunk progress,
-- not a human-readable moment, so this is a deliberately separate
-- column rather than overloading that one.
alter table sessions add column summarized_at timestamptz;

-- The manual "Re-summarize" trigger's own cooldown marker — set on
-- every real invocation attempt (success, failure, or rate-limited-
-- itself is never re-marked), not only on a successful write, so the
-- rate limit throttles repeated *clicks*, not repeated successful
-- runs. A session with no row here yet has never been manually
-- re-summarized.
alter table sessions add column last_resummarize_requested_at timestamptz;
